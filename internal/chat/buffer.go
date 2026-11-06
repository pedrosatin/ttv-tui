package chat

import (
	"slices"
	"strings"
	"time"
)

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
	FirstMsg    bool
	ReceivedAt  time.Time // Local receipt time, independent of Twitch sent timestamps.
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
	// UserNotice marks a system line that came from a USERNOTICE and still needs
	// the UI to render it. Body then holds only the text the user typed, if any.
	UserNotice bool
	// SystemMsg is the Twitch-provided system-msg tag, shown verbatim when present.
	SystemMsg string
	// Months carries USERNOTICE msg-param-cumulative-months for the local resub
	// fallback; 0 when unknown or not applicable.
	Months int
	// Community gifts retain their announced total and received child count
	// separately: the total is their maximum, never their sum.
	CommunityGiftID string
	GiftCount       int
	GiftRecipient   string
	giftReceived    int
	giftIDs         []string
	// Deleted marks a message moderation erased on Twitch. Body keeps the text.
	Deleted      bool
	Self         bool
	Delivery     DeliveryStatus
	RejectReason RejectReason
	RejectDetail string
}

// ByteSize estimates the byte footprint of m for buffer accounting.
func (m Message) ByteSize() int {
	n := len(m.ID) + len(m.Nonce) + len(m.User) + len(m.DisplayName) + len(m.Color) + len(m.MsgID) + len(m.Body) + len(m.SystemMsg) + len(m.RejectDetail) + len(m.CommunityGiftID) + len(m.GiftRecipient) + 80
	for _, id := range m.giftIDs {
		n += len(id) + 16
	}
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

	// seen and seenRing hold the most recent message IDs, up to twice maxLines.
	// They outlive eviction on purpose: a reconnecting server may replay messages
	// the buffer already dropped, and each one accepted would evict another.
	seen     map[string]struct{}
	seenRing []string
	seenNext int
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
		seen:     make(map[string]struct{}),
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

// AdoptGeneration switches the active generation and keeps the stored messages.
// It is for a new session on the same channel; a channel change uses SetGeneration.
func (b *Buffer) AdoptGeneration(gen uint64) {
	b.generation = gen
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
	clear(b.seen)
	b.seenRing = b.seenRing[:0]
	b.seenNext = 0
}

// Append adds msg to the buffer if its Generation matches the buffer's active
// generation (when the buffer has a non-zero generation set). If a single
// message's body exceeds maxBytes, it is truncated on a valid UTF-8 boundary.
// Oldest messages are evicted until both Len() <= maxLines and Bytes() <= maxBytes.
// A message whose non-empty ID was stored recently is a replay and is dropped.
// Community gifts update their existing entry and return false (no new line).
func (b *Buffer) Append(msg Message) bool {
	if b.generation != 0 && msg.Generation != b.generation {
		return false
	}
	if b.generation == 0 && msg.Generation != 0 {
		b.generation = msg.Generation
	}
	if _, replay := b.seen[msg.ID]; replay {
		return false
	}

	if msg.CommunityGiftID != "" && ClassifyUserNotice(msg.MsgID) == UserNoticeGift {
		if msg.MsgID == "subgift" || msg.MsgID == "anonsubgift" {
			msg.giftReceived = 1
		}
		msg.GiftCount = max(msg.GiftCount, msg.giftReceived)
		for i := range b.items {
			old := &b.items[i]
			if old.CommunityGiftID != msg.CommunityGiftID || ClassifyUserNotice(old.MsgID) != UserNoticeGift {
				continue
			}
			if msg.ID != "" && (old.ID == msg.ID || slices.Contains(old.giftIDs, msg.ID)) {
				return false
			}
			b.totalBytes -= old.ByteSize()
			if old.giftReceived < int(^uint(0)>>1) {
				old.giftReceived += msg.giftReceived
			}
			old.GiftCount = max(old.GiftCount, msg.GiftCount, old.giftReceived)
			if old.GiftRecipient == "" {
				old.GiftRecipient = msg.GiftRecipient
			}
			if msg.ID != "" {
				old.giftIDs = append(old.giftIDs, msg.ID)
			}
			b.totalBytes += old.ByteSize()
			b.rememberID(msg.ID)
			// Growing metadata is bounded by the same byte budget as new lines.
			for len(b.items) > 0 && b.totalBytes > b.maxBytes {
				b.totalBytes -= b.items[0].ByteSize()
				copy(b.items, b.items[1:])
				b.items[len(b.items)-1] = Message{}
				b.items = b.items[:len(b.items)-1]
			}
			return false // Updated in place, so this is not a new scroll arrival.
		}
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
	b.rememberID(msg.ID)
	return true
}

func (b *Buffer) rememberID(id string) {
	if id == "" {
		return
	}
	if len(b.seenRing) < 2*b.maxLines {
		b.seenRing = append(b.seenRing, id)
	} else {
		delete(b.seen, b.seenRing[b.seenNext])
		b.seenRing[b.seenNext] = id
		b.seenNext = (b.seenNext + 1) % len(b.seenRing)
	}
	b.seen[id] = struct{}{}
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

// ClearChat handles CLEARCHAT. An empty targetUser clears the entire buffer.
// Otherwise every message from targetUser stays in place, text included, marked
// Deleted: a moderator needs what was said to record the reason. The usual
// limits still evict them.
func (b *Buffer) ClearChat(gen uint64, targetUser string) {
	if b.generation != 0 && gen != b.generation {
		return
	}
	target := strings.ToLower(strings.TrimSpace(targetUser))
	if target == "" {
		b.Clear()
		return
	}
	for i := range b.items {
		if strings.EqualFold(b.items[i].User, target) || strings.EqualFold(b.items[i].DisplayName, target) {
			b.items[i].Deleted = true
		}
	}
}

// ClearMsg marks the message matching targetMsgID as Deleted for the active
// generation, keeping its text. An ID no longer in the buffer is a no-op.
func (b *Buffer) ClearMsg(gen uint64, targetMsgID string) {
	if b.generation != 0 && gen != b.generation {
		return
	}
	if targetMsgID == "" {
		return
	}
	for i := range b.items {
		if b.items[i].ID == targetMsgID || slices.Contains(b.items[i].giftIDs, targetMsgID) {
			b.items[i].Deleted = true
		}
	}
}

// Messages returns a copy of the buffered messages in chronological order.
func (b *Buffer) Messages() []Message {
	out := make([]Message, len(b.items))
	copy(out, b.items)
	for i := range out {
		out[i].giftIDs = slices.Clone(out[i].giftIDs)
	}
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
