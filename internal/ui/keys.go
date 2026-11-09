package ui

import (
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

type keyMap struct {
	Up            key.Binding
	Down          key.Binding
	PageUp        key.Binding
	PageDown      key.Binding
	Home          key.Binding
	GoLive        key.Binding
	Select        key.Binding
	SwitchFocus   key.Binding
	Filter        key.Binding
	Search        key.Binding
	ChannelFilter key.Binding
	Categories    key.Binding
	Retry         key.Binding
	Clear         key.Binding
	Quit          key.Binding
	Pause         key.Binding
	VolUp         key.Binding
	VolDown       key.Binding
	StopVideo     key.Binding
	ReconnectChat key.Binding
	Compose       key.Binding
	CopyMessage   key.Binding
	Help          key.Binding
	HelpGlobal    key.Binding
	CloseHelp     key.Binding
	Favorite      key.Binding
	CycleLayout   key.Binding
	ToggleList    key.Binding
	ToggleChat    key.Binding
	ListWider     key.Binding
	ListNarrower  key.Binding
	ChatWider     key.Binding
	ChatNarrower  key.Binding
	// ToggleChatOverlay only acts in the hosted window.
	ToggleChatOverlay key.Binding
	SwapSides         key.Binding
	ChatMoreOpaque    key.Binding
	ChatLessOpaque    key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
		),
		Home: key.NewBinding(
			key.WithKeys("home"),
		),
		// End and vim-style G pin the viewport back to the live bottom.
		GoLive: key.NewBinding(
			key.WithKeys("end", "G"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
		),
		SwitchFocus: key.NewBinding(
			key.WithKeys("tab"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
		),
		// Ctrl+F is the primary search key; Ctrl+S is an alias. The app runs
		// the terminal in raw mode, which disables software flow control, so
		// the terminal forwards Ctrl+S instead of using it to pause output.
		Search: key.NewBinding(
			key.WithKeys("ctrl+f", "ctrl+s"),
		),
		// l opens the channel language/audience filter menu (ticket 01).
		ChannelFilter: key.NewBinding(
			key.WithKeys("l"),
		),
		Categories: key.NewBinding(
			key.WithKeys("ctrl+r"),
		),
		Retry: key.NewBinding(
			key.WithKeys("r"),
		),
		Clear: key.NewBinding(
			key.WithKeys("esc"),
		),
		// Ctrl+Q is the only in-app quit key. Ctrl+C is handled separately so
		// it never exits (raw mode delivers it as a key, not SIGINT).
		Quit: key.NewBinding(
			key.WithKeys("ctrl+q"),
		),
		Pause: key.NewBinding(
			key.WithKeys(" ", "space"),
		),
		VolUp: key.NewBinding(
			key.WithKeys("+", "="),
		),
		VolDown: key.NewBinding(
			key.WithKeys("-", "_"),
		),
		StopVideo: key.NewBinding(
			key.WithKeys("ctrl+x"),
		),
		ReconnectChat: key.NewBinding(
			key.WithKeys("ctrl+t"),
		),
		Compose: key.NewBinding(
			key.WithKeys("i"),
		),
		// y yanks the selected chat message (or the latest while live) via
		// wl-copy, xclip, or OSC 52 (see writeClipboard).
		CopyMessage: key.NewBinding(
			key.WithKeys("y"),
		),
		Help: key.NewBinding(
			key.WithKeys("?", "f1"),
		),
		HelpGlobal: key.NewBinding(
			key.WithKeys("f1"),
		),
		// q only means something inside the help; quitting stays on Ctrl+Q.
		CloseHelp: key.NewBinding(
			key.WithKeys("q"),
		),
		Favorite: key.NewBinding(
			key.WithKeys("f"),
		),
		// Layout keys (ticket 02): Ctrl+W cycles named modes; Ctrl+H / Ctrl+L
		// toggle list and chat independently. Bubble Tea maps these to distinct
		// KeyCtrl* types (Ctrl+H is BS, Backspace is DEL).
		CycleLayout: key.NewBinding(
			key.WithKeys("ctrl+w"),
		),
		ToggleList: key.NewBinding(
			key.WithKeys("ctrl+h"),
		),
		ToggleChat: key.NewBinding(
			key.WithKeys("ctrl+l"),
		),
		// The arrow moves the divider: the list's is to the right of it, the
		// chat's to the left, so each panel grows toward the video.
		ListWider:         key.NewBinding(key.WithKeys("ctrl+right")),
		ListNarrower:      key.NewBinding(key.WithKeys("ctrl+left")),
		ChatWider:         key.NewBinding(key.WithKeys("alt+left")),
		ChatNarrower:      key.NewBinding(key.WithKeys("alt+right")),
		ToggleChatOverlay: key.NewBinding(key.WithKeys("ctrl+o")),
		SwapSides:         key.NewBinding(key.WithKeys("ctrl+p")),
		ChatMoreOpaque:    key.NewBinding(key.WithKeys("alt+up")),
		ChatLessOpaque:    key.NewBinding(key.WithKeys("alt+down")),
	}
}

// expandBatchedKeyMsg splits a KeyMsg that carries several runes into one
// KeyMsg per rune, in order. Bubble Tea can deliver a burst such as "jjj" as a
// single KeyRunes message; without splitting, key.Matches never sees "j" and
// the movement is dropped. Paste events stay as one message so pasted text
// never fires navigation shortcuts rune-by-rune. Single-rune and non-rune
// keys are returned unchanged.
func expandBatchedKeyMsg(msg tea.KeyMsg) []tea.KeyMsg {
	if msg.Type != tea.KeyRunes || msg.Paste || len(msg.Runes) <= 1 {
		return []tea.KeyMsg{msg}
	}
	out := make([]tea.KeyMsg, len(msg.Runes))
	for i, r := range msg.Runes {
		out[i] = tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{r},
			Alt:   msg.Alt,
		}
	}
	return out
}

func (m Model) handleKeyMsgBatch(keys []tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	cur := m
	for _, k := range keys {
		next, cmd := cur.handleKeyMsg(k)
		cur = next.(Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cur.quitting {
			break
		}
	}
	return cur, tea.Batch(cmds...)
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if keys := expandBatchedKeyMsg(msg); len(keys) > 1 {
		return m.handleKeyMsgBatch(keys)
	}

	m.clearKeyNotices()
	if next, cmd, ok := m.handleAppKey(msg); ok {
		return next, cmd
	}

	// Help is modal: past this point no key reaches the list, chat, filter or
	// search behind it. Only the app-level Ctrl+Q and Ctrl+C above outrank it.
	if m.helpOpen() {
		return m.updateHelpKey(msg)
	}

	if next, cmd, ok := m.handleGlobalKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleLayoutKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleModeKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleCommandKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handlePlayerKey(msg); ok {
		return next, cmd
	}
	if next, cmd, ok := m.handleMoveKey(msg); ok {
		return next, cmd
	}
	return m, nil
}

// hiddenListKey rejects list actions while preserving shared chat shortcuts
// and text entry in a visible chat. Global controls are handled independently.
func (m Model) hiddenListKey(msg tea.KeyMsg) bool {
	if m.listVisible() || m.list.prefs.menuOpen || (m.chat.composing && m.chatColumnVisible()) {
		return false
	}
	if key.Matches(msg, m.keys.Filter, m.keys.Search, m.keys.Favorite, m.keys.ChannelFilter, m.keys.Retry) {
		return true
	}
	return !m.chatColumnVisible() && key.Matches(msg,
		m.keys.Up, m.keys.Down, m.keys.Select, m.keys.Clear, m.keys.CopyMessage)
}

// isSwitchFocusKey reports whether msg is Tab, including a lone '\t' rune some
// terminals deliver instead of tea.KeyTab.
func isSwitchFocusKey(msg tea.KeyMsg, keys keyMap) bool {
	if key.Matches(msg, keys.SwitchFocus) || msg.Type == tea.KeyTab {
		return true
	}
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '\t'
}

// switchPanelFocus toggles list ↔ chat. It leaves composition without clearing
// the draft. When layout hides either panel, Tab is a no-op; when width does, it
// says so.
func (m Model) switchPanelFocus() Model {
	if !m.listVisible() || !m.chatColumnVisible() {
		m.warnChatHiddenByWidth()
		return m
	}
	m.chat.composing = false
	if m.layout.focus == FocusChat {
		m.layout.focus = FocusList
	} else {
		m.layout.focus = FocusChat
	}
	return m
}

// toggleHelp opens or closes the help, always at the top.
func (m *Model) toggleHelp() {
	m.layout.helpVisible = !m.layout.helpVisible
	m.layout.helpScroll = 0
}

// updateHelpKey consumes every key while the help is open: it closes, scrolls,
// or does nothing. Nothing here may touch the state behind the help.
func (m Model) updateHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.renderHelpLines(m.helpTextWidth()))
	page := max(m.helpPageRows(total)-1, 1)
	offset := m.helpOffset(total)
	switch {
	case key.Matches(msg, m.keys.Help, m.keys.Clear, m.keys.CloseHelp):
		m.toggleHelp()
		return m, nil
	case key.Matches(msg, m.keys.Up):
		offset--
	case key.Matches(msg, m.keys.Down):
		offset++
	case key.Matches(msg, m.keys.PageUp):
		offset -= page
	case key.Matches(msg, m.keys.PageDown):
		offset += page
	case key.Matches(msg, m.keys.Home):
		offset = 0
	case key.Matches(msg, m.keys.GoLive):
		offset = total
	}
	m.layout.helpScroll = offset
	return m, nil
}

// enterCompose focuses the chat panel and opens the message input when sending
// is available. The existing draft, if any, stays in the input.
func (m Model) enterCompose() (tea.Model, tea.Cmd) {
	if m.warnChatHiddenByWidth() || !m.layout.showChat || m.chatHiddenByWidth() {
		return m, nil
	}
	if !m.chatHasToken() && (m.chat.session != nil || m.chat.panel.channel != "") {
		m.chat.panel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}
	if m.hasChatPanel() || m.chat.session != nil {
		m.layout.focus = FocusChat
	}
	m.chat.composing = true
	return m, nil
}

func (m Model) playerControllable() bool {
	switch m.player.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting:
		return true
	default:
		return false
	}
}

// adjustVolume answers a volume key. At a limit the key would change nothing, so
// the bar says so. The command still goes out: the volume here can lag one in
// flight, and the player clamps on its own.
func (m Model) adjustVolume(delta int) (tea.Model, tea.Cmd) {
	if m.playback == nil || !m.playerControllable() {
		return m, nil
	}
	switch {
	case delta > 0 && m.player.volume >= 100:
		m.warnStatus(m.t("player.vol_max"))
	case delta < 0 && m.player.volume <= 0:
		m.warnStatus(m.t("player.vol_min"))
	}
	return m, m.playback.AdjustVolume(delta)
}

func (m Model) updateComposingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		// Esc leaves composition and stays on the chat panel; draft is kept.
		m.chat.composing = false
		return m, nil

	case key.Matches(msg, m.keys.Select):
		return m.submitComposeMessage()

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.chat.composeDraft = trimLastRune(m.chat.composeDraft)
		m.clearResolvedComposeWarning()
		return m, nil

	case msg.Type == tea.KeySpace, key.Matches(msg, m.keys.Pause):
		m.chat.composeDraft += " "
		m.clearResolvedComposeWarning()
		return m, nil

	case key.Matches(msg, m.keys.VolUp):
		m.chat.composeDraft += "+"
		m.clearResolvedComposeWarning()
		return m, nil

	case key.Matches(msg, m.keys.VolDown):
		m.chat.composeDraft += "-"
		m.clearResolvedComposeWarning()
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.chat.composeDraft += sanitizeChatText(string(msg.Runes))
		m.clearResolvedComposeWarning()
		return m, nil
	}
	return m, nil
}

// clearResolvedComposeWarning drops a warning about the draft once the edit fixed
// it, so the footer stops describing a problem the draft no longer has.
func (m *Model) clearResolvedComposeWarning() {
	switch m.chat.panel.sendWarning {
	case "":
	case m.t("chat.warn.too_long"):
		if !m.composeDraftOverLimit() {
			m.chat.panel.sendWarning = ""
		}
	case m.t("chat.warn.command_blocked"):
		if !chat.IsBlockedCommand(m.chat.composeDraft) {
			m.chat.panel.sendWarning = ""
		}
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}
