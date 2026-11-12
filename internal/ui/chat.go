package ui

import (
	"crypto/rand"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// ChatState describes the UI-facing state of the active chat session.
type ChatState string

const (
	ChatStateNone         ChatState = ""
	ChatStateDisconnected ChatState = ChatState(catalog.ChatStateDisconnected)
	ChatStateConnecting   ChatState = ChatState(catalog.ChatStateConnecting)
	ChatStateConnected    ChatState = ChatState(catalog.ChatStateConnected)
	ChatStateReconnecting ChatState = ChatState(catalog.ChatStateReconnecting)
	ChatStateError        ChatState = ChatState(catalog.ChatStateError)
)

// ChatMessage is the UI-facing alias for catalog.ChatMessage.
type ChatMessage = catalog.ChatMessage

// ChatRoomState is the UI-facing alias for catalog.ChatRoomState.
type ChatRoomState = catalog.ChatRoomState

// ChatSendRequest is the UI-facing alias for catalog.ChatSendRequest.
type ChatSendRequest = catalog.ChatSendRequest

// ChatSendStatusUpdate is the UI-facing alias for catalog.ChatSendStatusUpdate.
type ChatSendStatusUpdate = catalog.ChatSendStatusUpdate

// ChatEventMsg is delivered by the injected Chat bridge whenever the chat
// session changes state, updates ROOMSTATE, updates outbound message status,
// or flushes a batch of messages.
type ChatEventMsg struct {
	// Generation numbers the sessions across every platform: the bridge hands
	// out one monotonic sequence, so a larger value is always a newer session.
	Generation uint64
	// Platform is the platform whose chat produced the event. Empty means the
	// default platform; an event from a platform other than the panel's is
	// stale and dropped.
	Platform    string
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

// Chat is the UI->app bridge for managing the active channel chat session. It
// fronts the chats of every platform: each call goes to the chat of the
// platform of the channel, and the events of all of them arrive as ChatEventMsg.
type Chat interface {
	// Capabilities reports what the chat of ch's platform can do. The bool is
	// false when the platform has no chat at all.
	Capabilities(ch catalog.Channel) (catalog.ChatCapabilities, bool)
	// Join opens the chat of ch, stopping the one that was active.
	Join(ch catalog.Channel) tea.Cmd
	Reconnect() tea.Cmd
	Stop() tea.Cmd
}

// ChatSender is an optional extension of Chat that supports sending messages
// with rate limiting and authentication status inspection. HasToken and
// Username describe the chat of the channel joined last.
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
	buf       *catalog.ChatBuffer
	gen       uint64
	switching bool
	state     ChatState
	// platform and channel name the room: channel alone is ambiguous, since
	// twitch:foo and kick:foo are different rooms.
	platform    string
	channel     string
	anonymous   bool
	username    string
	roomState   ChatRoomState
	errText     string
	sendWarning string
	// gapPending asks for the reconnect line once the new session is up.
	gapPending bool
	// unsupported marks the chat panel for a platform without chat.
	unsupported bool
	// hasChat reports whether the current platform provides a chat session.
	hasChat bool
	// caps declares the active platform's chat capabilities.
	caps catalog.ChatCapabilities

	// Scroll state (see chat_scroll.go). followLive pins the viewport to the
	// newest message; bottomIdx is the buffer index shown at the bottom when
	// paused; pendingNew counts arrivals while paused.
	followLive bool
	bottomIdx  int
	pendingNew int
}

func newChatPanel() chatPanel {
	return chatPanel{
		buf: catalog.NewChatBuffer(catalog.DefaultChatMaxLines, catalog.DefaultChatMaxBytes),
		roomState: ChatRoomState{
			FollowersOnly: -1,
		},
		followLive: true,
		caps:       catalog.TwitchChatCapabilities,
		hasChat:    true,
	}
}

// setChatUnsupported shows the panel of a platform that has no chat.
func (m *Model) setChatUnsupported(ch catalog.Channel) {
	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}
	plat := platform.Normalize(ch.Platform)
	m.chat.panel.buf.Clear()
	m.chat.panel.platform = plat
	m.chat.panel.channel = normalizeChatChannel(plat, chatChannelName(ch))
	m.chat.panel.unsupported = true
	m.chat.panel.hasChat = false
	m.chat.panel.caps = catalog.ChatCapabilities{}
	m.chat.panel.state = ChatStateDisconnected
	m.chat.panel.errText = ""
	m.chat.panel.sendWarning = ""
	m.chat.panel.switching = false
	m.chat.composing = false
}

// maxRuneLength is the message limit of the active chat; 0 means no limit.
func (c chatState) maxRuneLength() int {
	return max(c.panel.caps.MaxMessageLength, 0)
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
		if msg.Self && msg.Delivery == catalog.ChatDeliverySending {
			return true
		}
	}
	return false
}

// chatChannelName is the name the panel shows for ch: its login, or the
// identifier it is known by on its platform when it has no login.
func chatChannelName(ch catalog.Channel) string {
	if ch.Login != "" {
		return ch.Login
	}
	return ch.StableID()
}

// normalizeChatChannel brings a chat room name to the form the panel compares.
// Only Twitch folds case and drops the IRC "#": on the other platforms the name
// is an identifier (a YouTube video id is case-sensitive).
func normalizeChatChannel(plat, name string) string {
	name = strings.TrimSpace(name)
	if platform.IsTwitch(plat) {
		return strings.ToLower(strings.TrimPrefix(name, "#"))
	}
	return name
}

// sameChatChannel reports whether a and b name the same room on plat.
func sameChatChannel(plat, a, b string) bool {
	a, b = normalizeChatChannel(plat, a), normalizeChatChannel(plat, b)
	if platform.Normalize(plat) == platform.YouTube {
		return a == b
	}
	return strings.EqualFold(a, b)
}

// chatRoomKey names a chat room across platforms: twitch/foo and kick/foo are
// different rooms. It is a map key, never shown.
type chatRoomKey struct {
	platform string
	channel  string
}

// chatKey builds the key of a room. The name folds case except on YouTube, where
// an identifier differing in case is another one.
func chatKey(plat, channel string) chatRoomKey {
	plat = platform.Normalize(plat)
	channel = normalizeChatChannel(plat, channel)
	if plat != platform.YouTube {
		channel = strings.ToLower(channel)
	}
	return chatRoomKey{platform: plat, channel: channel}
}

// panelKey is the chatKey of the room the panel shows; ok is false when it
// shows none.
func (c chatState) panelKey() (key chatRoomKey, ok bool) {
	if c.panel.channel == "" {
		return chatRoomKey{}, false
	}
	return chatKey(c.panel.platform, c.panel.channel), true
}

// panelPlatform reports whether msg comes from the platform the panel shows.
// An event with no platform is taken as the panel's.
func (c chatState) panelPlatform(msg ChatEventMsg) bool {
	return msg.Platform == "" || c.panel.platform == "" || platform.Normalize(msg.Platform) == platform.Normalize(c.panel.platform)
}

// panelRoom reports whether the room of msg is the one the panel shows.
func (c chatState) panelRoom(msg ChatEventMsg) bool {
	return c.panelPlatform(msg) && sameChatChannel(c.panel.platform, msg.Channel, c.panel.channel)
}

func (m *Model) startChatChannel(ch catalog.Channel) tea.Cmd {
	plat := platform.Normalize(ch.Platform)
	name := normalizeChatChannel(plat, chatChannelName(ch))
	key := chatKey(plat, name)
	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}
	prevKey, hadRoom := m.chat.panelKey()
	hadPending := m.chat.hasPendingOutgoing() && hadRoom && prevKey != key
	if m.chat.drafts == nil {
		m.chat.drafts = make(map[chatRoomKey]string)
	}
	if hadRoom {
		m.chat.drafts[prevKey] = m.chat.composeDraft
	}
	m.chat.composeDraft = m.chat.drafts[key]
	m.chat.panel.buf.Clear()
	m.chat.panel.platform = plat
	m.chat.panel.channel = name
	m.chat.panel.unsupported = false
	m.chat.panel.hasChat = true
	if m.chat.session != nil {
		if caps, ok := m.chat.session.Capabilities(ch); ok {
			m.chat.panel.caps = caps
		}
	}
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
	if m.chat.session == nil {
		return nil
	}
	// Join first: the sender answers for the chat of the channel joined last.
	cmd := m.chat.session.Join(ch)
	if sender, ok := m.chat.session.(ChatSender); ok {
		m.chat.panel.anonymous = !sender.HasToken()
		m.chat.panel.username = sender.Username()
	}
	return cmd
}

func (m Model) submitComposeMessage() (tea.Model, tea.Cmd) {
	body := strings.TrimSpace(m.chat.composeDraft)
	if body == "" {
		return m, nil
	}

	if !m.chat.panel.caps.CanSend {
		return m, nil
	}

	if !m.chat.hasToken() {
		m.chat.panel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}

	if catalog.IsBlockedCommand(body) {
		// Keep the draft and stay in compose mode: sending would publish it.
		m.chat.panel.sendWarning = m.t("chat.warn.command_blocked")
		return m, nil
	}

	if limit := m.chat.maxRuneLength(); limit > 0 && utf8.RuneCountInString(body) > limit {
		// Keep the draft and stay in compose mode; never truncate on send.
		m.chat.panel.sendWarning = m.t("chat.warn.too_long")
		return m, nil
	}

	if m.chat.panel.unsupported || m.chat.panel.switching || m.chat.panel.channel == "" || m.chat.session == nil {
		return m, nil
	}
	sender, ok := m.chat.session.(ChatSender)
	if !ok {
		return m, nil
	}

	if m.chat.panel.buf == nil {
		m.chat.panel = newChatPanel()
	}

	nonce := rand.Text()
	displayName := m.chat.panel.username
	if displayName == "" {
		displayName = m.t("chat.self_name")
	}

	m.chat.composeDraft = ""
	m.chat.panel.sendWarning = ""
	beforeLen := m.chat.panel.buf.Len()
	appended := m.chat.panel.buf.Append(ChatMessage{
		Generation:  m.chat.panel.gen,
		Kind:        catalog.ChatMessageChat,
		ReceivedAt:  m.chat.now(),
		Nonce:       nonce,
		User:        m.chat.panel.username,
		DisplayName: displayName,
		Body:        body,
		Self:        true,
		Delivery:    catalog.ChatDeliverySending,
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

	// Generations are global across platforms, but a late event of another
	// platform's chat must never reach this panel, whatever its number.
	if !m.chat.panelPlatform(msg) {
		return m, nil
	}

	if m.chat.panel.unsupported {
		// Ignore events from the previous session while the panel is unsupported.
		if msg.Generation <= m.chat.panel.gen || (msg.Channel != "" && !m.chat.panelRoom(msg)) {
			return m, nil
		}
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
		m.chat.panel.channel = normalizeChatChannel(m.chat.panel.platform, msg.Channel)
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
			if m.chat.panel.caps.SupportsUserNotice {
				item = m.userNoticeMessage(item)
			}
		}
		beforeLen := m.chat.panel.buf.Len()
		appended := m.chat.panel.buf.Append(item)
		m.noteChatAppend(beforeLen, appended)
	}
	return m, nil
}

// adoptChatGeneration moves the panel to the session of msg. Every reconnect
// gets a new generation, so only a different channel empties the panel; opening
// a channel clears the buffer earlier, in startChatChannel.
func (m *Model) adoptChatGeneration(msg ChatEventMsg) {
	m.chat.panel.gen = msg.Generation
	if m.chat.panelRoom(msg) {
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
		Kind:       catalog.ChatMessageSystem,
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
	if item.CommunityGiftID != "" && catalog.ClassifyUserNotice(item.MsgID) == catalog.UserNoticeGift {
		item.Generation, item.Kind = gen, catalog.ChatMessageSystem
		item.Body, item.SystemMsg, item.UserNotice = "", "", false
		return item
	}
	text := strings.TrimSpace(item.Body)
	tbody := strings.TrimSpace(item.SystemMsg)
	switch {
	case tbody == "":
		tbody = m.userNoticeFallback(item)
		if text != "" {
			tbody = fmt.Sprintf(m.t("chat.usernotice.with_text"), tbody, text)
		}
	case text != "":
		tbody += " — " + text
	}
	return ChatMessage{
		Generation:      gen,
		Kind:            catalog.ChatMessageSystem,
		ID:              item.ID,
		ReceivedAt:      item.ReceivedAt,
		User:            item.User,
		DisplayName:     item.DisplayName,
		MsgID:           item.MsgID,
		Months:          item.Months,
		CommunityGiftID: item.CommunityGiftID,
		GiftCount:       item.GiftCount,
		GiftRecipient:   item.GiftRecipient,
		Body:            tbody,
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
	switch catalog.ClassifyUserNotice(item.MsgID) {
	case catalog.UserNoticeSub:
		return fmt.Sprintf(m.t("chat.usernotice.sub"), name)
	case catalog.UserNoticeResub:
		if item.Months > 0 {
			return fmt.Sprintf(m.t("chat.usernotice.resub_months"), name, item.Months)
		}
		return fmt.Sprintf(m.t("chat.usernotice.resub"), name)
	case catalog.UserNoticeGift:
		if item.GiftRecipient != "" {
			return fmt.Sprintf(m.t("chat.usernotice.gift_recipient"), name, item.GiftRecipient)
		}
		return fmt.Sprintf(m.t("chat.usernotice.gift"), name)
	case catalog.UserNoticeRaid:
		return fmt.Sprintf(m.t("chat.usernotice.raid"), name)
	case catalog.UserNoticeAnnouncement:
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
		Kind:       catalog.ChatMessageSystem,
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
		Kind:       catalog.ChatMessageSystem,
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
	case catalog.ChatDeliveryDiscarded:
		switch upd.Reason {
		case catalog.ChatRejectTimeout, catalog.ChatRejectConnectionLost:
			if m.chat.composeDraft == "" && sameChatChannel(m.chat.panel.platform, upd.Channel, m.chat.panel.channel) {
				m.chat.composeDraft = upd.Body
			}
		case catalog.ChatRejectRateLimited:
			m.chat.panel.sendWarning = m.t("chat.warn.rate_limit")
		case catalog.ChatRejectChannelSwitched:
			m.chat.panel.sendWarning = m.t("chat.warn.channel_switched")
		case catalog.ChatRejectReconnected:
			m.chat.panel.sendWarning = m.t("chat.warn.reconnected")
		}
	case catalog.ChatDeliveryRejected:
		if upd.Reason == catalog.ChatRejectUnauthenticated {
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
		if msgs[i].CommunityGiftID != "" && catalog.ClassifyUserNotice(msgs[i].MsgID) == catalog.UserNoticeGift {
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
		return catalog.DefaultChatMaxLines, catalog.DefaultChatMaxBytes
	}
	return m.chat.panel.buf.MaxLines(), m.chat.panel.buf.MaxBytes()
}

// ChatCapabilities returns the declared capabilities for the active chat panel.
func (m Model) ChatCapabilities() catalog.ChatCapabilities {
	return m.chat.panel.caps
}
