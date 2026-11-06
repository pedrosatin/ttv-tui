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
		m.chat = c
		if sender, ok := c.(ChatSender); ok {
			m.chatPanel.anonymous = !sender.HasToken()
			m.chatPanel.username = sender.Username()
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

func (m Model) chatHasToken() bool {
	if m.chatPanel.anonymous {
		return false
	}
	if sender, ok := m.chat.(ChatSender); ok {
		return sender.HasToken()
	}
	return false
}

func (m Model) hasPendingOutgoing() bool {
	if m.chatPanel.buf == nil {
		return false
	}
	for _, msg := range m.chatPanel.buf.Messages() {
		if msg.Self && msg.Delivery == chat.DeliverySending {
			return true
		}
	}
	return false
}

func (m *Model) startChatChannel(login string) tea.Cmd {
	norm := normalizeChatChannel(login)
	if m.chatPanel.buf == nil {
		m.chatPanel = newChatPanel()
	}
	hadPending := m.hasPendingOutgoing() && m.chatPanel.channel != "" && m.chatPanel.channel != norm
	m.chatPanel.buf.Clear()
	m.chatPanel.channel = norm
	m.chatPanel.state = ChatStateConnecting
	m.chatPanel.errText = ""
	if hadPending {
		m.chatPanel.sendWarning = m.t("chat.warn.channel_switched")
	} else {
		m.chatPanel.sendWarning = ""
	}
	m.chatPanel.roomState = ChatRoomState{FollowersOnly: -1}
	m.chatPanel.switching = true
	m.chatPanel.gapPending = false
	m.resetChatScroll()
	if sender, ok := m.chat.(ChatSender); ok {
		m.chatPanel.anonymous = !sender.HasToken()
		m.chatPanel.username = sender.Username()
	}
	if m.chat == nil {
		return nil
	}
	return m.chat.Join(norm)
}

func (m Model) submitComposeMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.composeDraft)
	if body == "" {
		return m, nil
	}

	if !m.chatHasToken() {
		m.chatPanel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}

	if chat.IsBlockedCommand(body) {
		// Keep the draft and stay in compose mode: sending would publish it.
		m.chatPanel.sendWarning = m.t("chat.warn.command_blocked")
		return m, nil
	}

	if utf8.RuneCountInString(body) > twitchMessageRuneLimit {
		// Keep the draft and stay in compose mode; never truncate on send.
		m.chatPanel.sendWarning = m.t("chat.warn.too_long")
		return m, nil
	}

	if m.chatPanel.channel == "" || m.chat == nil {
		return m, nil
	}
	sender, ok := m.chat.(ChatSender)
	if !ok {
		return m, nil
	}

	if m.chatPanel.buf == nil {
		m.chatPanel = newChatPanel()
	}

	m.chatPanel.nextNonce++
	nonce := fmt.Sprintf("ui-%d-%d", m.chatPanel.gen, m.chatPanel.nextNonce)
	displayName := m.chatPanel.username
	if displayName == "" {
		displayName = m.t("chat.self_name")
	}

	m.composeDraft = ""
	m.chatPanel.sendWarning = ""
	beforeLen := m.chatPanel.buf.Len()
	appended := m.chatPanel.buf.Append(ChatMessage{
		Generation:  m.chatPanel.gen,
		Kind:        chat.MessageChat,
		Nonce:       nonce,
		User:        m.chatPanel.username,
		DisplayName: displayName,
		Body:        body,
		Self:        true,
		Delivery:    chat.DeliverySending,
	})
	m.noteChatAppend(beforeLen, appended)

	req := ChatSendRequest{
		Generation: m.chatPanel.gen,
		Channel:    m.chatPanel.channel,
		Nonce:      nonce,
		Body:       body,
	}
	return m, sender.Send(req)
}

func (m Model) handleChatEvent(msg ChatEventMsg) (tea.Model, tea.Cmd) {
	if m.chatPanel.buf == nil {
		m.chatPanel = newChatPanel()
	}

	if m.chatPanel.switching {
		if !msg.FromJoin && msg.Generation <= m.chatPanel.gen {
			return m, nil
		}
		m.chatPanel.switching = false
		// Generation 0 is a Join or Reconnect that failed: no session replaced
		// the current one, so there is nothing to adopt.
		if msg.Generation != 0 {
			m.adoptChatGeneration(msg)
		}
	} else if msg.Generation != 0 && m.chatPanel.gen != 0 && msg.Generation < m.chatPanel.gen {
		// Stale generation from a previous channel — discard.
		return m, nil
	} else if msg.Generation > m.chatPanel.gen {
		m.adoptChatGeneration(msg)
	} else if m.chatPanel.gen != 0 && m.chatPanel.buf.Generation() != m.chatPanel.gen {
		m.chatPanel.buf.SetGeneration(m.chatPanel.gen)
		m.resetChatScroll()
	}

	if msg.Channel != "" {
		m.chatPanel.channel = normalizeChatChannel(msg.Channel)
	}
	if msg.State != ChatStateNone {
		m.chatPanel.state = msg.State
	}
	if msg.State == ChatStateReconnecting && m.chatPanel.buf.Len() > 0 {
		m.chatPanel.gapPending = true
	}
	if msg.State == ChatStateConnected && m.chatPanel.gapPending {
		m.markReconnectGap()
	}
	if msg.Anonymous {
		m.chatPanel.anonymous = true
	} else if sender, ok := m.chat.(ChatSender); ok {
		m.chatPanel.anonymous = !sender.HasToken()
	}
	if msg.ErrText != "" {
		m.chatPanel.errText = msg.ErrText
	} else if msg.State == ChatStateConnected {
		m.chatPanel.errText = ""
	}
	if msg.RoomState != nil {
		m.chatPanel.roomState = *msg.RoomState
	}
	if msg.ClearChat {
		m.chatPanel.buf.ClearChat(msg.Generation, msg.ClearUser)
		if msg.ClearUser == "" {
			// Full clear empties the buffer; scroll returns to live.
			m.resetChatScroll()
		}
		beforeLen := m.chatPanel.buf.Len()
		appended := m.chatPanel.buf.Append(m.moderationClearChatMessage(msg))
		m.noteChatAppend(beforeLen, appended)
	}
	if msg.ClearMsgID != "" {
		m.chatPanel.buf.ClearMsg(msg.Generation, msg.ClearMsgID)
		beforeLen := m.chatPanel.buf.Len()
		appended := m.chatPanel.buf.Append(m.moderationClearMsgMessage(msg))
		m.noteChatAppend(beforeLen, appended)
	}
	for _, upd := range msg.SendUpdates {
		m.applySendUpdate(upd)
	}
	for _, item := range msg.Messages {
		if item.Generation == 0 {
			item.Generation = msg.Generation
		}
		if item.UserNotice {
			item = m.userNoticeMessage(item)
		}
		beforeLen := m.chatPanel.buf.Len()
		appended := m.chatPanel.buf.Append(item)
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
	m.chatPanel.gen = msg.Generation
	if normalizeChatChannel(msg.Channel) == m.chatPanel.channel {
		m.chatPanel.buf.AdoptGeneration(msg.Generation)
		m.chatPanel.gapPending = m.chatPanel.buf.Len() > 0
		return
	}
	m.chatPanel.buf.SetGeneration(msg.Generation)
	m.chatPanel.gapPending = false
	m.resetChatScroll()
}

// markReconnectGap appends the line separating the history from what the new
// session delivers. Back-to-back reconnects with nothing in between share one
// line. It carries no times: the client has no clock for when the gap began.
func (m *Model) markReconnectGap() {
	m.chatPanel.gapPending = false
	msgs := m.chatPanel.buf.Messages()
	if len(msgs) == 0 || msgs[len(msgs)-1].MsgID == systemMsgIDReconnect {
		return
	}
	appended := m.chatPanel.buf.Append(ChatMessage{
		Generation: m.chatPanel.gen,
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
		gen = m.chatPanel.gen
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
		Generation:  gen,
		Kind:        chat.MessageSystem,
		ID:          item.ID,
		User:        item.User,
		DisplayName: item.DisplayName,
		MsgID:       item.MsgID,
		Months:      item.Months,
		Body:        body,
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
		gen = m.chatPanel.gen
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
		gen = m.chatPanel.gen
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
		upd.Generation = m.chatPanel.gen
	}
	m.chatPanel.buf.UpdateDelivery(upd)

	switch upd.Status {
	case chat.DeliveryDiscarded:
		switch upd.Reason {
		case chat.RejectTimeout, chat.RejectConnectionLost:
			if m.composeDraft == "" && normalizeChatChannel(upd.Channel) == m.chatPanel.channel {
				m.composeDraft = upd.Body
			}
		case chat.RejectRateLimited:
			m.chatPanel.sendWarning = m.t("chat.warn.rate_limit")
		case chat.RejectChannelSwitched:
			m.chatPanel.sendWarning = m.t("chat.warn.channel_switched")
		case chat.RejectReconnected:
			m.chatPanel.sendWarning = m.t("chat.warn.reconnected")
		}
	case chat.DeliveryRejected:
		if upd.Reason == chat.RejectUnauthenticated {
			m.chatPanel.sendWarning = m.t("chat.warn.no_token")
		}
	}
}

// ChatState returns the current UI-facing chat connection state.
func (m Model) ChatState() ChatState {
	return m.chatPanel.state
}

// ChatChannel returns the login of the channel currently bound to the chat panel.
func (m Model) ChatChannel() string {
	return m.chatPanel.channel
}

// ChatGeneration returns the active chat session generation accepted by the UI.
func (m Model) ChatGeneration() uint64 {
	return m.chatPanel.gen
}

// ChatMessages returns a snapshot of the messages currently in the chat buffer.
func (m Model) ChatMessages() []ChatMessage {
	if m.chatPanel.buf == nil {
		return nil
	}
	return m.chatPanel.buf.Messages()
}

// ChatRoomState returns the current ROOMSTATE flags for the active channel.
func (m Model) ChatRoomState() ChatRoomState {
	return m.chatPanel.roomState
}

// ComposeDraft returns the current unsent message text in the chat input box.
func (m Model) ComposeDraft() string {
	return m.composeDraft
}

// ChatSendWarning returns the active rate-limit, channel-switch, or auth warning on the chat panel.
func (m Model) ChatSendWarning() string {
	return m.chatPanel.sendWarning
}

// ChatBufferLimits returns the chat buffer's configured max lines and max bytes.
func (m Model) ChatBufferLimits() (maxLines, maxBytes int) {
	if m.chatPanel.buf == nil {
		return chat.DefaultMaxLines, chat.DefaultMaxBytes
	}
	return m.chatPanel.buf.MaxLines(), m.chatPanel.buf.MaxBytes()
}
