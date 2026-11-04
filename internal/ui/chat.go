package ui

import (
	"fmt"
	"strings"

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
}

func newChatPanel() chatPanel {
	return chatPanel{
		buf: chat.NewBuffer(chat.DefaultMaxLines, chat.DefaultMaxBytes),
		roomState: ChatRoomState{
			FollowersOnly: -1,
		},
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
	norm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(login), "#"))
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
	m.chatPanel.buf.Append(ChatMessage{
		Generation:  m.chatPanel.gen,
		Kind:        chat.MessageChat,
		Nonce:       nonce,
		User:        m.chatPanel.username,
		DisplayName: displayName,
		Body:        body,
		Self:        true,
		Delivery:    chat.DeliverySending,
	})

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
		m.chatPanel.gen = msg.Generation
		m.chatPanel.buf.SetGeneration(msg.Generation)
	} else if msg.Generation != 0 && m.chatPanel.gen != 0 && msg.Generation < m.chatPanel.gen {
		// Stale generation from a previous channel — discard.
		return m, nil
	} else if msg.Generation > m.chatPanel.gen {
		m.chatPanel.gen = msg.Generation
		m.chatPanel.buf.SetGeneration(msg.Generation)
	} else if m.chatPanel.gen != 0 && m.chatPanel.buf.Generation() != m.chatPanel.gen {
		m.chatPanel.buf.SetGeneration(m.chatPanel.gen)
	}

	if msg.Channel != "" {
		m.chatPanel.channel = strings.ToLower(strings.TrimPrefix(msg.Channel, "#"))
	}
	if msg.State != ChatStateNone {
		m.chatPanel.state = msg.State
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
		m.chatPanel.buf.Append(m.moderationClearChatMessage(msg))
	}
	if msg.ClearMsgID != "" {
		m.chatPanel.buf.ClearMsg(msg.Generation, msg.ClearMsgID)
		m.chatPanel.buf.Append(m.moderationClearMsgMessage(msg))
	}
	for _, upd := range msg.SendUpdates {
		m.applySendUpdate(upd)
	}
	for _, item := range msg.Messages {
		if item.Generation == 0 {
			item.Generation = msg.Generation
		}
		m.chatPanel.buf.Append(item)
	}
	return m, nil
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
		case chat.RejectRateLimited:
			m.chatPanel.sendWarning = m.t("chat.warn.rate_limit")
		case chat.RejectChannelSwitched:
			m.chatPanel.sendWarning = m.t("chat.warn.channel_switched")
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
