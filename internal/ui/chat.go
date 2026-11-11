package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

// ChatState describes the UI-facing state of the active chat session.
type ChatState string

const (
	ChatStateNone         ChatState = ""
	ChatStateDisconnected ChatState = ChatState(chat.StateDisconnected)
	ChatStateConnecting   ChatState = ChatState(chat.StateConnecting)
	ChatStateConnected    ChatState = ChatState(chat.StateConnected)
	ChatStateReconnecting ChatState = ChatState(chat.StateReconnecting)
	ChatStateError        ChatState = ChatState(chat.StateError)
)

// ChatMessage is the UI-facing alias for chat.Message.
type ChatMessage = chat.Message

// ChatRoomState is the UI-facing alias for chat.RoomState.
type ChatRoomState = chat.RoomState

// ChatSendRequest is the UI-facing alias for chat.SendRequest.
type ChatSendRequest = chat.SendRequest

// ChatSendStatusUpdate is the UI-facing alias for chat.SendStatusUpdate.
type ChatSendStatusUpdate = chat.SendStatusUpdate

// ChatEventMsg is delivered by the injected Chat bridge whenever the chat
// session changes state, updates ROOMSTATE, updates outbound message status,
// or flushes a batch of messages.
type ChatEventMsg struct {
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
	// and ClearChat means a permanent ban.
	BanDuration *int
	ErrText     string
	AuthFailed  bool
	FromJoin    bool
}

// Chat is the UI->app bridge for managing the active channel chat session.
type Chat interface {
	Join(login string) tea.Cmd
	Reconnect() tea.Cmd
	Stop() tea.Cmd
}

// ChatSender is an optional extension of Chat that supports sending PRIVMSG
// with rate limiting and authentication status inspection.
type ChatSender interface {
	Send(req ChatSendRequest) tea.Cmd
	HasToken() bool
	Username() string
}

// WithChat attaches a Chat bridge to the Model.
func WithChat(c Chat) Option {
	return func(m *Model) {
		m.chat.session = c
		if sender, ok := c.(ChatSender); ok {
			m.chat.panel.anonymous = !sender.HasToken()
			m.chat.panel.username = sender.Username()
		}
	}
}

type chatPanel struct {
	buf         *chat.Buffer
	gen         uint64
	switching   bool
	state       ChatState
	channel     string
	anonymous   bool
	username    string
	roomState   ChatRoomState
	errText     string
	sendWarning string
	nextNonce   uint64
	// gapPending asks for the reconnect line once the new session is up.
	gapPending bool

	// Scroll state (see chat_scroll.go). followLive pins the viewport to the
	// newest message; bottomIdx is the buffer index shown at the bottom when
	// paused; pendingNew counts arrivals while paused.
	followLive bool
	bottomIdx  int
	pendingNew int
}

func newChatPanel() chatPanel {
	return chatPanel{
		buf: chat.NewBuffer(chat.DefaultMaxLines, chat.DefaultMaxBytes),
		roomState: ChatRoomState{
			FollowersOnly: -1,
		},
		followLive: true,
	}
}

func (c chatState) hasToken() bool {
	if c.panel.anonymous {
		return false
	}
	if sender, ok := c.session.(ChatSender); ok {
		return sender.HasToken()
	}
	return false
}

func (c chatState) hasPendingOutgoing() bool {
	if c.panel.buf == nil {
		return false
	}
	for _, msg := range c.panel.buf.Messages() {
		if msg.Self && msg.Delivery == chat.DeliverySending {
			return true
		}
	}
	return false
}

func (m *Model) startChatChannel(login string) tea.Cmd {
	norm := normalizeChatChannel(login)
	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}
	hadPending := m.chat.hasPendingOutgoing() && m.chat.panel.channel != "" && m.chat.panel.channel != norm
	m.chat.panel.buf.Clear()
	m.chat.panel.channel = norm
	m.chat.panel.state = ChatStateConnecting
	m.chat.panel.errText = ""
	if hadPending {
		m.chat.panel.sendWarning = m.t("chat.warn.channel_switched")
	} else {
		m.chat.panel.sendWarning = ""
	}
	m.chat.panel.roomState = ChatRoomState{FollowersOnly: -1}
	m.chat.panel.switching = true
	m.chat.panel.gapPending = false
	m.resetChatScroll()
	if sender, ok := m.chat.session.(ChatSender); ok {
		m.chat.panel.anonymous = !sender.HasToken()
		m.chat.panel.username = sender.Username()
	}
	if m.chat.session == nil {
		return nil
	}
	return m.chat.session.Join(norm)
}

func (m Model) submitComposeMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.chat.composeDraft)
	if body == "" {
		return m, nil
	}

	if !m.chat.hasToken() {
		m.chat.panel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}

	if chat.IsBlockedCommand(body) {
		// Keep the draft and stay in compose mode: sending would publish it.
		m.chat.panel.sendWarning = m.t("chat.warn.command_blocked")
		return m, nil
	}

	if utf8.RuneCountInString(body) > twitchMessageRuneLimit {
		// Keep the draft and stay in compose mode; never truncate on send.
		m.chat.panel.sendWarning = m.t("chat.warn.too_long")
		return m, nil
	}

	if m.chat.panel.channel == "" || m.chat.session == nil {
		return m, nil
	}
	sender, ok := m.chat.session.(ChatSender)
	if !ok {
		return m, nil
	}

	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}

	m.chat.panel.nextNonce++
	nonce := fmt.Sprintf("ui-%d-%d", m.chat.panel.gen, m.chat.panel.nextNonce)
	displayName := m.chat.panel.username
	if displayName == "" {
		displayName = m.t("chat.self_name")
	}

	m.chat.composeDraft = ""
	m.chat.panel.sendWarning = ""
	beforeLen := m.chat.panel.buf.Len()
	appended := m.chat.panel.buf.Append(ChatMessage{
		Generation:  m.chat.panel.gen,
		Kind:        chat.MessageChat,
		ReceivedAt:  m.chat.now(),
		Nonce:       nonce,
		User:        m.chat.panel.username,
		DisplayName: displayName,
		Body:        body,
		Self:        true,
		Delivery:    chat.DeliverySending,
	})
	m.noteChatAppend(beforeLen, appended)

	req := ChatSendRequest{
		Generation: m.chat.panel.gen,
		Channel:    m.chat.panel.channel,
		Nonce:      nonce,
		Body:       body,
	}
	return m, sender.Send(req)
}

func (m Model) handleChatEvent(msg ChatEventMsg) (tea.Model, tea.Cmd) {
	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}

	if m.chat.panel.switching {
		if !msg.FromJoin && msg.Generation <= m.chat.panel.gen {
			return m, nil
		}
		m.chat.panel.switching = false
		// Generation 0 is a Join or Reconnect that failed: no session replaced
		// the current one, so there is nothing to adopt.
		if msg.Generation != 0 {
			m.adoptChatGeneration(msg)
		}
	} else if msg.Generation != 0 && m.chat.panel.gen != 0 && msg.Generation < m.chat.panel.gen {
		// Stale generation from a previous channel — discard.
		return m, nil
	} else if msg.Generation > m.chat.panel.gen {
		m.adoptChatGeneration(msg)
	} else if m.chat.panel.gen != 0 && m.chat.panel.buf.Generation() != m.chat.panel.gen {
		m.chat.panel.buf.SetGeneration(m.chat.panel.gen)
		m.resetChatScroll()
	}

	if msg.Channel != "" {
		m.chat.panel.channel = normalizeChatChannel(msg.Channel)
	}
	if msg.State != ChatStateNone {
		m.chat.panel.state = msg.State
	}
	if msg.State == ChatStateReconnecting && m.chat.panel.buf.Len() > 0 {
		m.chat.panel.gapPending = true
	}
	if msg.State == ChatStateConnected && m.chat.panel.gapPending {
		m.markReconnectGap()
	}
	if msg.Anonymous {
		m.chat.panel.anonymous = true
	} else if sender, ok := m.chat.session.(ChatSender); ok {
		m.chat.panel.anonymous = !sender.HasToken()
	}
	if msg.AuthFailed {
		m.chat.panel.errText = m.t("chat.auth_failed")
	} else if msg.ErrText != "" {
		m.chat.panel.errText = msg.ErrText
	} else if msg.State == ChatStateConnected {
		m.chat.panel.errText = ""
	}
	if msg.RoomState != nil {
		m.chat.panel.roomState = *msg.RoomState
	}
	if msg.ClearChat {
		m.chat.panel.buf.ClearChat(msg.Generation, msg.ClearUser)
		if msg.ClearUser == "" {
			// Full clear empties the buffer; scroll returns to live.
			m.resetChatScroll()
		}
		beforeLen := m.chat.panel.buf.Len()
		appended := m.chat.panel.buf.Append(m.moderationClearChatMessage(msg))
		m.noteChatAppend(beforeLen, appended)
	}
	if msg.ClearMsgID != "" {
		m.chat.panel.buf.ClearMsg(msg.Generation, msg.ClearMsgID)
		beforeLen := m.chat.panel.buf.Len()
		appended := m.chat.panel.buf.Append(m.moderationClearMsgMessage(msg))
		m.noteChatAppend(beforeLen, appended)
	}
	for _, upd := range msg.SendUpdates {
		m.applySendUpdate(upd)
	}
	for _, item := range msg.Messages {
		if item.ReceivedAt.IsZero() {
			item.ReceivedAt = m.chat.now()
		}
		if item.Generation == 0 {
			item.Generation = msg.Generation
		}
		if item.UserNotice {
			item = m.userNoticeMessage(item)
		}
		beforeLen := m.chat.panel.buf.Len()
		appended := m.chat.panel.buf.Append(item)
		m.noteChatAppend(beforeLen, appended)
	}
	return m, nil
}

func normalizeChatChannel(login string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "#"))
}

// adoptChatGeneration moves the panel to the session of msg. Every reconnect
// gets a new generation, so only a different channel empties the panel; opening
// a channel clears the buffer earlier, in startChatChannel.
func (m *Model) adoptChatGeneration(msg ChatEventMsg) {
	m.chat.panel.gen = msg.Generation
	if normalizeChatChannel(msg.Channel) == m.chat.panel.channel {
		m.chat.panel.buf.AdoptGeneration(msg.Generation)
		m.chat.panel.gapPending = m.chat.panel.buf.Len() > 0
		return
	}
	m.chat.panel.buf.SetGeneration(msg.Generation)
	m.chat.panel.gapPending = false
	m.resetChatScroll()
}

// markReconnectGap appends the line separating the history from what the new
// session delivers. Back-to-back reconnects with nothing in between share one
// line. It carries no times: the client has no clock for when the gap began.
func (m *Model) markReconnectGap() {
	m.chat.panel.gapPending = false
	msgs := m.chat.panel.buf.Messages()
	if len(msgs) == 0 || msgs[len(msgs)-1].MsgID == systemMsgIDReconnect {
		return
	}
	appended := m.chat.panel.buf.Append(ChatMessage{
		Generation: m.chat.panel.gen,
		Kind:       chat.MessageSystem,
		MsgID:      systemMsgIDReconnect,
		Body:       m.t("chat.reconnected_gap"),
	})
	m.noteChatAppend(len(msgs), appended)
}

// userNoticeMessage is the single renderer for USERNOTICE. Twitch's system-msg
// is content and stays verbatim; the catalog only fills in when it is empty,
// which is the normal case for announcements. The text the user typed follows
// either way. Twitch sentences end in punctuation, so they get a dash, not ": ".
func (m Model) userNoticeMessage(item ChatMessage) ChatMessage {
	gen := item.Generation
	if gen == 0 {
		gen = m.chat.panel.gen
	}
	if item.CommunityGiftID != "" && chat.ClassifyUserNotice(item.MsgID) == chat.UserNoticeGift {
		item.Generation, item.Kind = gen, chat.MessageSystem
		item.Body, item.SystemMsg, item.UserNotice = "", "", false
		return item
	}
	text := strings.TrimSpace(item.Body)
	body := strings.TrimSpace(item.SystemMsg)
	switch {
	case body == "":
		body = m.userNoticeFallback(item)
		if text != "" {
			body = fmt.Sprintf(m.t("chat.usernotice.with_text"), body, text)
		}
	case text != "":
		body += " — " + text
	}
	return ChatMessage{
		Generation:      gen,
		Kind:            chat.MessageSystem,
		ID:              item.ID,
		ReceivedAt:      item.ReceivedAt,
		User:            item.User,
		DisplayName:     item.DisplayName,
		MsgID:           item.MsgID,
		Months:          item.Months,
		CommunityGiftID: item.CommunityGiftID,
		GiftCount:       item.GiftCount,
		GiftRecipient:   item.GiftRecipient,
		Body:            body,
	}
}

func (m Model) userNoticeFallback(item ChatMessage) string {
	name := strings.TrimSpace(item.DisplayName)
	if name == "" {
		name = strings.TrimSpace(item.User)
	}
	if name == "" {
		name = "?"
	}
	switch chat.ClassifyUserNotice(item.MsgID) {
	case chat.UserNoticeSub:
		return fmt.Sprintf(m.t("chat.usernotice.sub"), name)
	case chat.UserNoticeResub:
		if item.Months > 0 {
			return fmt.Sprintf(m.t("chat.usernotice.resub_months"), name, item.Months)
		}
		return fmt.Sprintf(m.t("chat.usernotice.resub"), name)
	case chat.UserNoticeGift:
		if item.GiftRecipient != "" {
			return fmt.Sprintf(m.t("chat.usernotice.gift_recipient"), name, item.GiftRecipient)
		}
		return fmt.Sprintf(m.t("chat.usernotice.gift"), name)
	case chat.UserNoticeRaid:
		return fmt.Sprintf(m.t("chat.usernotice.raid"), name)
	case chat.UserNoticeAnnouncement:
		return fmt.Sprintf(m.t("chat.usernotice.announcement"), name)
	}
	if id := strings.TrimSpace(item.MsgID); id != "" {
		return fmt.Sprintf(m.t("chat.usernotice.event_id"), id)
	}
	return m.t("chat.usernotice.event")
}

func (m Model) moderationClearChatMessage(msg ChatEventMsg) ChatMessage {
	gen := msg.Generation
	if gen == 0 {
		gen = m.chat.panel.gen
	}
	user := strings.TrimSpace(msg.ClearUser)
	var body string
	switch {
	case user == "":
		body = m.t("chat.mod.clear_all")
	case msg.BanDuration != nil:
		body = fmt.Sprintf(m.t("chat.mod.timeout"), user, m.formatBanDuration(*msg.BanDuration))
	default:
		body = fmt.Sprintf(m.t("chat.mod.ban"), user)
	}
	return ChatMessage{
		Generation: gen,
		Kind:       chat.MessageSystem,
		MsgID:      systemMsgIDModeration,
		Body:       body,
	}
}

func (m Model) moderationClearMsgMessage(msg ChatEventMsg) ChatMessage {
	gen := msg.Generation
	if gen == 0 {
		gen = m.chat.panel.gen
	}
	user := strings.TrimSpace(msg.ClearUser)
	var body string
	if user == "" {
		body = m.t("chat.mod.msg_deleted_unknown")
	} else {
		body = fmt.Sprintf(m.t("chat.mod.msg_deleted"), user)
	}
	return ChatMessage{
		Generation: gen,
		Kind:       chat.MessageSystem,
		MsgID:      systemMsgIDModeration,
		Body:       body,
	}
}

func (m Model) formatBanDuration(secs int) string {
	if secs < 0 {
		secs = 0
	}
	if secs < 60 {
		return fmt.Sprintf(m.t("chat.mod.dur_s"), secs)
	}
	if secs < 3600 {
		mins := secs / 60
		rem := secs % 60
		if rem == 0 {
			return fmt.Sprintf(m.t("chat.mod.dur_m"), mins)
		}
		return fmt.Sprintf(m.t("chat.mod.dur_m_s"), mins, rem)
	}
	hours := secs / 3600
	mins := (secs % 3600) / 60
	if mins == 0 {
		return fmt.Sprintf(m.t("chat.mod.dur_h"), hours)
	}
	return fmt.Sprintf(m.t("chat.mod.dur_h_m"), hours, mins)
}

func (m *Model) applySendUpdate(upd ChatSendStatusUpdate) {
	if upd.Generation == 0 {
		upd.Generation = m.chat.panel.gen
	}
	m.chat.panel.buf.UpdateDelivery(upd)

	switch upd.Status {
	case chat.DeliveryDiscarded:
		switch upd.Reason {
		case chat.RejectTimeout, chat.RejectConnectionLost:
			if m.chat.composeDraft == "" && normalizeChatChannel(upd.Channel) == m.chat.panel.channel {
				m.chat.composeDraft = upd.Body
			}
		case chat.RejectRateLimited:
			m.chat.panel.sendWarning = m.t("chat.warn.rate_limit")
		case chat.RejectChannelSwitched:
			m.chat.panel.sendWarning = m.t("chat.warn.channel_switched")
		case chat.RejectReconnected:
			m.chat.panel.sendWarning = m.t("chat.warn.reconnected")
		}
	case chat.DeliveryRejected:
		if upd.Reason == chat.RejectUnauthenticated {
			m.chat.panel.sendWarning = m.t("chat.warn.no_token")
		}
	}
}

// ChatState returns the current UI-facing chat connection state.
func (m Model) ChatState() ChatState {
	return m.chat.panel.state
}

// ChatChannel returns the login of the channel currently bound to the chat panel.
func (m Model) ChatChannel() string {
	return m.chat.panel.channel
}

// ChatGeneration returns the active chat session generation accepted by the UI.
func (m Model) ChatGeneration() uint64 {
	return m.chat.panel.gen
}

// ChatMessages returns a snapshot of the messages currently in the chat buffer.
func (m Model) ChatMessages() []ChatMessage {
	if m.chat.panel.buf == nil {
		return nil
	}
	msgs := m.chat.panel.buf.Messages()
	for i := range msgs {
		if msgs[i].CommunityGiftID != "" && chat.ClassifyUserNotice(msgs[i].MsgID) == chat.UserNoticeGift {
			name := strings.TrimSpace(msgs[i].DisplayName)
			if name == "" {
				name = strings.TrimSpace(msgs[i].User)
			}
			if name == "" {
				name = "?"
			}
			if msgs[i].GiftCount == 1 && msgs[i].GiftRecipient != "" {
				msgs[i].Body = fmt.Sprintf(m.t("chat.usernotice.gift_recipient"), name, msgs[i].GiftRecipient)
			} else {
				msgs[i].Body = fmt.Sprintf(m.t("chat.usernotice.gift_community"), name, msgs[i].GiftCount)
			}
		}
	}
	return msgs
}

// ChatRoomState returns the current ROOMSTATE flags for the active channel.
func (m Model) ChatRoomState() ChatRoomState {
	return m.chat.panel.roomState
}

// ComposeDraft returns the current unsent message text in the chat input box.
func (m Model) ComposeDraft() string {
	return m.chat.composeDraft
}

// ChatSendWarning returns the active rate-limit, channel-switch, or auth warning on the chat panel.
func (m Model) ChatSendWarning() string {
	return m.chat.panel.sendWarning
}

// ChatBufferLimits returns the chat buffer's configured max lines and max bytes.
func (m Model) ChatBufferLimits() (maxLines, maxBytes int) {
	if m.chat.panel.buf == nil {
		return chat.DefaultMaxLines, chat.DefaultMaxBytes
	}
	return m.chat.panel.buf.MaxLines(), m.chat.panel.buf.MaxBytes()
}
