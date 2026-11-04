package chat

import "strings"

// Default buffer capacity limits:
//   - DefaultMaxLines = 500 messages
//   - DefaultMaxBytes = 256 KiB (262,144 bytes) of accumulated message footprint
const (
	DefaultMaxLines = 500
	DefaultMaxBytes = 256 * 1024
)

// MessageKind classifies an entry stored in the chat buffer.
type MessageKind string

const (
	MessageChat   MessageKind = "chat"
	MessageNotice MessageKind = "notice"
	MessageSystem MessageKind = "system"
)

// Message is a single displayable item in the chat buffer.
type Message struct {
	Generation  uint64
	Kind        MessageKind
	ID          string
	Nonce       string
	User        string
	DisplayName string
	Color       string
	Badges      []string
	MsgID       string
	Highlighted bool
	Action      bool
	Body        string
	// Months carries USERNOTICE msg-param-cumulative-months for resub system lines
	// before the UI localizes the body; 0 when unknown or not applicable.
	Months       int
	Deleted      bool
	Self         bool
	Delivery     DeliveryStatus
	RejectReason RejectReason
	RejectDetail string
}

// ByteSize estimates the byte footprint of m for buffer accounting.
func (m Message) ByteSize() int {
	n := len(m.ID) + len(m.Nonce) + len(m.User) + len(m.DisplayName) + len(m.Color) + len(m.MsgID) + len(m.Body) + len(m.RejectDetail) + 32
	for _, b := range m.Badges {
		n += len(b)
	}
	return n
}

// Buffer is a circular message buffer bounded simultaneously by line count
// (maxLines) and total byte footprint (maxBytes). It also rejects messages
// from stale channel generations.
type Buffer struct {
	maxLines   int
	maxBytes   int
	generation uint64
	items      []Message
	totalBytes int
}

// NewBuffer creates a Buffer bounded by maxLines and maxBytes.
// Non-positive limits fall back to DefaultMaxLines and DefaultMaxBytes.
func NewBuffer(maxLines, maxBytes int) *Buffer {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &Buffer{
		maxLines: maxLines,
		maxBytes: maxBytes,
		items:    make([]Message, 0, min(maxLines, 128)),
	}
}

// SetGeneration switches the active channel generation. If gen differs from
// the current generation, existing messages are cleared so messages from a
// previous channel never leak into the new channel.
func (b *Buffer) SetGeneration(gen uint64) {
	if b.generation != gen {
		b.generation = gen
		b.Clear()
	}
}

// Generation returns the active generation accepted by the buffer.
func (b *Buffer) Generation() uint64 {
	return b.generation
}

// MaxLines returns the configured line capacity.
func (b *Buffer) MaxLines() int {
	return b.maxLines
}

// MaxBytes returns the configured byte capacity.
func (b *Buffer) MaxBytes() int {
	return b.maxBytes
}

// Len returns the number of messages currently stored.
func (b *Buffer) Len() int {
	return len(b.items)
}

// Bytes returns the current accounted byte size of stored messages.
func (b *Buffer) Bytes() int {
	return b.totalBytes
}

// Clear removes all messages while keeping the current generation and limits.
func (b *Buffer) Clear() {
	clear(b.items)
	b.items = b.items[:0]
	b.totalBytes = 0
}

// Append adds msg to the buffer if its Generation matches the buffer's active
// generation (when the buffer has a non-zero generation set). If a single
// message's body exceeds maxBytes, it is truncated on a valid UTF-8 boundary.
// Oldest messages are evicted until both Len() <= maxLines and Bytes() <= maxBytes.
func (b *Buffer) Append(msg Message) bool {
	if b.generation != 0 && msg.Generation != b.generation {
		return false
	}
	if b.generation == 0 && msg.Generation != 0 {
		b.generation = msg.Generation
	}

	size := msg.ByteSize()
	if size > b.maxBytes {
		overhead := size - len(msg.Body)
		allowedBody := b.maxBytes - overhead
		if allowedBody < 0 {
			allowedBody = 0
		}
		msg.Body = truncateUTF8Bytes(msg.Body, allowedBody)
		size = msg.ByteSize()
		if size > b.maxBytes {
			// Metadata alone exceeds maxBytes; drop rather than violate limit.
			return false
		}
	}

	evictCount := 0
	remLines := len(b.items)
	remBytes := b.totalBytes
	for evictCount < len(b.items) && (remLines+1 > b.maxLines || remBytes+size > b.maxBytes) {
		remBytes -= b.items[evictCount].ByteSize()
		remLines--
		evictCount++
	}
	if evictCount > 0 {
		copy(b.items, b.items[evictCount:])
		clear(b.items[len(b.items)-evictCount:])
		b.items = b.items[:len(b.items)-evictCount]
		b.totalBytes = remBytes
	}

	b.items = append(b.items, msg)
	b.totalBytes += size
	return true
}

// UpdateDelivery updates the delivery state of a buffered message tracked by
// Nonce (or falls back to the earliest pending Self message when Nonce is empty)
// within the active generation.
func (b *Buffer) UpdateDelivery(upd SendStatusUpdate) bool {
	if b.generation != 0 && upd.Generation != 0 && upd.Generation != b.generation {
		return false
	}
	idx := -1
	if upd.Nonce != "" {
		for i := range b.items {
			if b.items[i].Nonce == upd.Nonce {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		for i := range b.items {
			if b.items[i].Self && b.items[i].Delivery == DeliverySending {
				if upd.Body == "" || b.items[i].Body == upd.Body {
					idx = i
					break
				}
			}
		}
	}
	if idx < 0 {
		return false
	}
	oldSize := b.items[idx].ByteSize()
	b.items[idx].Delivery = upd.Status
	b.items[idx].RejectReason = upd.Reason
	b.items[idx].RejectDetail = upd.Detail
	newSize := b.items[idx].ByteSize()
	b.totalBytes += newSize - oldSize
	return true
}

// ClearChat handles CLEARCHAT: if targetUser is empty, clears the entire buffer;
// otherwise removes messages sent by targetUser.
func (b *Buffer) ClearChat(gen uint64, targetUser string) {
	if b.generation != 0 && gen != b.generation {
		return
	}
	target := strings.ToLower(strings.TrimSpace(targetUser))
	if target == "" {
		b.Clear()
		return
	}
	kept := b.items[:0]
	total := 0
	for _, m := range b.items {
		if strings.EqualFold(m.User, target) || strings.EqualFold(m.DisplayName, target) {
			continue
		}
		kept = append(kept, m)
		total += m.ByteSize()
	}
	clear(b.items[len(kept):])
	b.items = kept
	b.totalBytes = total
}

// ClearMsg removes a single message matching targetMsgID for the active generation.
func (b *Buffer) ClearMsg(gen uint64, targetMsgID string) {
	if b.generation != 0 && gen != b.generation {
		return
	}
	if targetMsgID == "" {
		return
	}
	kept := b.items[:0]
	total := 0
	for _, m := range b.items {
		if m.ID == targetMsgID {
			continue
		}
		kept = append(kept, m)
		total += m.ByteSize()
	}
	clear(b.items[len(kept):])
	b.items = kept
	b.totalBytes = total
}

// Messages returns a copy of the buffered messages in chronological order.
func (b *Buffer) Messages() []Message {
	out := make([]Message, len(b.items))
	copy(out, b.items)
	return out
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && (s[maxBytes]&0xC0) == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
}
