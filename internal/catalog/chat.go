package catalog

import (
	"strings"
	"time"
)

// ChatCapabilities describes the features supported by a platform's chat implementation.
type ChatCapabilities struct {
	// CanSend reports whether sending messages is supported.
	CanSend bool
	// MaxMessageLength is the maximum character/rune count per message (0 means unlimited or send not supported).
	MaxMessageLength int
	// SupportsSlowMode reports whether slow mode and room moderation flags are supported.
	SupportsSlowMode bool
	// SupportsUserNotice reports whether platform-specific event notices (e.g. USERNOTICE) are supported.
	SupportsUserNotice bool
}

// TwitchChatCapabilities defines the standard capabilities of Twitch IRC chat.
var TwitchChatCapabilities = ChatCapabilities{
	CanSend:            true,
	MaxMessageLength:   500,
	SupportsSlowMode:   true,
	SupportsUserNotice: true,
}

// ReadOnlyChatCapabilities defines capabilities for read-only platform chats (Kick, YouTube).
var ReadOnlyChatCapabilities = ChatCapabilities{
	CanSend:            false,
	MaxMessageLength:   0,
	SupportsSlowMode:   false,
	SupportsUserNotice: false,
}

// ChatState describes the observable connection state of a chat session.
type ChatState string

const (
	ChatStateDisconnected ChatState = "desconectado"
	ChatStateConnecting   ChatState = "conectando"
	ChatStateConnected    ChatState = "conectado"
	ChatStateReconnecting ChatState = "reconectando"
	ChatStateError        ChatState = "erro"
)

// ChatMessageKind classifies an entry stored in the chat buffer.
type ChatMessageKind string

const (
	ChatMessageChat   ChatMessageKind = "chat"
	ChatMessageNotice ChatMessageKind = "notice"
	ChatMessageSystem ChatMessageKind = "system"
)

// ChatDeliveryStatus tracks the lifecycle of an outbound user message.
type ChatDeliveryStatus string

const (
	ChatDeliveryNone      ChatDeliveryStatus = ""
	ChatDeliverySending   ChatDeliveryStatus = "enviando"
	ChatDeliveryConfirmed ChatDeliveryStatus = "confirmada"
	ChatDeliveryRejected  ChatDeliveryStatus = "recusada"
	ChatDeliveryDiscarded ChatDeliveryStatus = "descartada"
)

// ChatRejectReason classifies why an outbound message was rejected or discarded.
type ChatRejectReason string

const (
	ChatRejectTimeout         ChatRejectReason = "timeout"
	ChatRejectConnectionLost  ChatRejectReason = "connection_lost"
	ChatRejectNone            ChatRejectReason = ""
	ChatRejectSlowMode        ChatRejectReason = "slow_mode"
	ChatRejectFollowersOnly   ChatRejectReason = "followers_only"
	ChatRejectSubsOnly        ChatRejectReason = "subs_only"
	ChatRejectEmoteOnly       ChatRejectReason = "emote_only"
	ChatRejectR9K             ChatRejectReason = "r9k"
	ChatRejectRateLimited     ChatRejectReason = "rate_limit"
	ChatRejectUnauthenticated ChatRejectReason = "unauthenticated"
	ChatRejectChannelSwitched ChatRejectReason = "channel_switched"
	ChatRejectReconnected     ChatRejectReason = "reconnected"
	ChatRejectBanned          ChatRejectReason = "banned"
	ChatRejectOther           ChatRejectReason = "other"
)

// ChatMessage is a single displayable item in the chat buffer.
type ChatMessage struct {
	Generation      uint64
	Kind            ChatMessageKind
	FirstMsg        bool
	ReceivedAt      time.Time
	ID              string
	Nonce           string
	User            string
	DisplayName     string
	Color           string
	Badges          []string
	MsgID           string
	Highlighted     bool
	Action          bool
	Body            string
	UserNotice      bool
	SystemMsg       string
	Months          int
	CommunityGiftID string
	GiftCount       int
	GiftRecipient   string
	Deleted         bool
	Self            bool
	Delivery        ChatDeliveryStatus
	RejectReason    ChatRejectReason
	RejectDetail    string

	giftReceived int
	giftIDs      []string
}

// ByteSize estimates the byte footprint of m for buffer accounting.
func (m ChatMessage) ByteSize() int {
	n := len(m.ID) + len(m.Nonce) + len(m.User) + len(m.DisplayName) + len(m.Color) + len(m.MsgID) + len(m.Body) + len(m.SystemMsg) + len(m.RejectDetail) + len(m.CommunityGiftID) + len(m.GiftRecipient) + 80
	for _, id := range m.giftIDs {
		n += len(id) + 16
	}
	for _, b := range m.Badges {
		n += len(b)
	}
	return n
}

// ChatRoomState describes channel moderation modes.
type ChatRoomState struct {
	Slow          int  // seconds; 0 = off
	FollowersOnly int  // minutes; -1 = off, >= 0 = enabled (0 = any follower)
	SubsOnly      bool // true = subscribers-only mode
	EmoteOnly     bool // true = emote-only mode
	R9K           bool // true = unique-chat mode
}

// ChatRoomStateDelta holds optional fields present in a partial room state update.
type ChatRoomStateDelta struct {
	Slow          *int
	FollowersOnly *int
	SubsOnly      *bool
	EmoteOnly     *bool
	R9K           *bool
}

// Apply merges non-nil fields from d into r and returns the updated ChatRoomState.
func (d ChatRoomStateDelta) Apply(r ChatRoomState) ChatRoomState {
	if d.Slow != nil {
		r.Slow = *d.Slow
	}
	if d.FollowersOnly != nil {
		r.FollowersOnly = *d.FollowersOnly
	}
	if d.SubsOnly != nil {
		r.SubsOnly = *d.SubsOnly
	}
	if d.EmoteOnly != nil {
		r.EmoteOnly = *d.EmoteOnly
	}
	if d.R9K != nil {
		r.R9K = *d.R9K
	}
	return r
}

// ChatSendRequest describes an outbound message tied to a channel generation.
type ChatSendRequest struct {
	Generation uint64
	Channel    string
	Nonce      string
	Body       string
}

// ChatSendStatusUpdate records delivery state transitions for an outbound message.
type ChatSendStatusUpdate struct {
	Generation uint64
	Channel    string
	Nonce      string
	Body       string
	Status     ChatDeliveryStatus
	Reason     ChatRejectReason
	Detail     string
}

// UserNoticeKind groups USERNOTICE msg-id values by how the chat presents them.
type UserNoticeKind string

const (
	UserNoticeOther        UserNoticeKind = "other"
	UserNoticeSub          UserNoticeKind = "sub"
	UserNoticeResub        UserNoticeKind = "resub"
	UserNoticeGift         UserNoticeKind = "gift"
	UserNoticeRaid         UserNoticeKind = "raid"
	UserNoticeAnnouncement UserNoticeKind = "announcement"
)

// ClassifyUserNotice maps a USERNOTICE msg-id onto a UserNoticeKind.
func ClassifyUserNotice(msgID string) UserNoticeKind {
	switch strings.ToLower(strings.TrimSpace(msgID)) {
	case "sub":
		return UserNoticeSub
	case "resub", "resubscribe":
		return UserNoticeResub
	case "subgift", "anonsubgift", "submysterygift", "anonsubmysterygift":
		return UserNoticeGift
	case "raid":
		return UserNoticeRaid
	case "announcement":
		return UserNoticeAnnouncement
	default:
		return UserNoticeOther
	}
}

// ChatEvent is delivered by a chat implementation whenever the session
// changes state, updates room moderation modes, updates outbound message status,
// or flushes a batch of messages.
type ChatEvent struct {
	Generation  uint64
	Channel     string
	State       ChatState
	Anonymous   bool
	Messages    []ChatMessage
	SendUpdates []ChatSendStatusUpdate
	RoomState   *ChatRoomState
	ClearChat   bool
	ClearUser   string
	ClearMsgID  string
	// BanDuration is set for CLEARCHAT timeouts (seconds). Nil with ClearUser
	// set and ClearChat true means a permanent ban.
	BanDuration *int
	Err         error
	Attempt     int
	NextBackoff time.Duration
}

// Chat is the neutral platform chat interface.
type Chat interface {
	// Capabilities returns the declared chat features before connecting.
	Capabilities() ChatCapabilities
	// Join connects to a channel's chat and returns the active session generation.
	Join(channel string) (uint64, error)
	// Reconnect reconnects the active chat session.
	Reconnect() (uint64, error)
	// Stop disconnects and clears the active chat session.
	Stop()
	// Send queues or transmits a message to the active channel.
	Send(req ChatSendRequest) (ChatSendStatusUpdate, error)
	// Events returns the read-only channel delivering chat events.
	Events() <-chan ChatEvent
	// Close shuts down the chat client and releases resources.
	Close()
	// HasToken reports whether the chat client holds a valid authentication token.
	HasToken() bool
	// Username returns the authenticated username, or empty if anonymous.
	Username() string
}

// IsBlockedCommand reports whether body would be published as plain text instead
// of running as a command. Commands starting with / other than /me are blocked.
func IsBlockedCommand(body string) bool {
	clean := sanitizeOutboundBody(body)
	if !strings.HasPrefix(clean, "/") {
		return false
	}
	return strings.Fields(clean)[0] != "/me"
}

func sanitizeOutboundBody(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\r' || r == '\n' || r == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
