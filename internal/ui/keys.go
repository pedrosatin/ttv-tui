package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type keyMap struct {
	Up            key.Binding
	Down          key.Binding
	PageUp        key.Binding
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
	Favorite      key.Binding
	CycleLayout   key.Binding
	ToggleList    key.Binding
	ToggleChat    key.Binding
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

	m.authNotice = ""
	m.corruptNotice = ""
	m.statusNotice = ""
	m.ctrlCNotice = ""
	m.copyNotice = ""
	m.playerWindowNotice = false
	if key.Matches(msg, m.keys.Quit) {
		m.tracker.cancelActive()
		m.generation++
		m.loading = false
		m.quitting = true
		var cmds []tea.Cmd
		if m.playback != nil {
			cmds = append(cmds, m.playback.Stop())
		}
		if m.chat != nil {
			cmds = append(cmds, m.chat.Stop())
		}
		cmds = append(cmds, tea.Quit)
		return m, tea.Batch(cmds...)
	}

	// Ctrl+C reaches a raw-mode TUI as a key, not SIGINT. It never quits;
	// only Ctrl+Q takes the shutdown path. Show a localized hint instead.
	if msg.Type == tea.KeyCtrlC {
		m.ctrlCNotice = m.t("status.ctrl_c_hint")
		return m, nil
	}

	// F1 toggles keyboard help at any time, even while typing.
	if key.Matches(msg, m.keys.HelpGlobal) {
		m.helpVisible = !m.helpVisible
		return m, nil
	}

	// Ctrl+R returns to categories from anywhere, including compose and help.
	if key.Matches(msg, m.keys.Categories) {
		return m.returnToCategories()
	}

	// Ctrl+X ends video without leaving the app (works while composing).
	if key.Matches(msg, m.keys.StopVideo) {
		if m.playback == nil {
			return m, nil
		}
		if m.playerState == PlayerStateNone || m.playerState == PlayerStateEnded {
			return m, nil
		}
		return m, m.playback.Stop()
	}

	if key.Matches(msg, m.keys.ReconnectChat) {
		if m.chat == nil || m.chatPanel.channel == "" {
			return m, nil
		}
		m.chatPanel.state = ChatStateConnecting
		m.chatPanel.switching = true
		return m, m.chat.Reconnect()
	}

	// Layout controls work from any mode (compose, help, search, filter).
	if key.Matches(msg, m.keys.CycleLayout) {
		m.cycleLayout()
		return m, nil
	}
	if key.Matches(msg, m.keys.ToggleList) {
		m.toggleListPanel()
		return m, nil
	}
	if key.Matches(msg, m.keys.ToggleChat) {
		m.toggleChatPanel()
		return m, nil
	}

	if m.searching {
		return m.updateSearchingKey(msg)
	}

	if m.filtering {
		return m.updateFilteringKey(msg)
	}

	if m.channelFilterMenu {
		return m.updateChannelFilterMenuKey(msg)
	}

	// Tab switches list ↔ chat in every panel state: list focus, chat focus,
	// composing, and with help open. It never sends or clears the draft. When
	// help is open, Tab closes it so the new focus marker is visible (Esc and
	// ? still only dismiss help, without switching panels).
	if isSwitchFocusKey(msg, m.keys) {
		return m.switchPanelFocus(), nil
	}

	// When help overlay is visible, ? or Esc closes it without changing focus.
	if m.helpVisible {
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Clear) {
			m.helpVisible = false
			return m, nil
		}
	}

	if m.composing && m.showChat && !m.chatHiddenByWidth() {
		return m.updateComposingKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Favorite):
		if m.Focus() == FocusChat {
			return m, nil
		}
		return m.toggleFavoriteHighlighted()

	case key.Matches(msg, m.keys.ChannelFilter):
		if m.Focus() == FocusChat {
			return m, nil
		}
		return m.openChannelFilterMenu(), nil

	case key.Matches(msg, m.keys.Help):
		m.helpVisible = !m.helpVisible
		return m, nil

	case key.Matches(msg, m.keys.Clear):
		if m.Focus() == FocusChat {
			m.focus = FocusList
			m.composing = false
			return m, nil
		}
		if m.filter != "" {
			m.filter = ""
			m.reconcileHighlight()
			return m, nil
		}
		// Esc steps back one level (vim-style). Search returns to the list it
		// left; channels return to categories. Ctrl+R still jumps to categories.
		switch m.Mode() {
		case ModeSearch:
			return m.returnFromSearch()
		case ModeChannels:
			return m.returnToCategories()
		default:
			return m, nil
		}

	case key.Matches(msg, m.keys.Compose):
		return m.enterCompose()

	case key.Matches(msg, m.keys.CopyMessage):
		if m.Focus() != FocusChat {
			return m, nil
		}
		return m.copyChatMessage()

	case key.Matches(msg, m.keys.Filter):
		m.focus = FocusList
		m.clearStaleStatusErrors()
		m.filtering = true
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.focus = FocusList
		if q := strings.TrimSpace(m.filter); q != "" {
			return m.startSearch(q)
		}
		m.clearStaleStatusErrors()
		m.searching = true
		m.searchInput = ""
		return m, nil

	case key.Matches(msg, m.keys.Retry):
		if m.err == nil && m.playerState == PlayerStateError && m.playback != nil && m.playerLogin != "" {
			return m.retryPlayback()
		}
		return m.retryCurrentMode()

	case key.Matches(msg, m.keys.Pause):
		if m.playback == nil || !m.playerControllable() {
			return m, nil
		}
		return m, m.playback.TogglePause()

	case key.Matches(msg, m.keys.VolUp):
		if m.playback == nil || !m.playerControllable() {
			return m, nil
		}
		return m, m.playback.AdjustVolume(5)

	case key.Matches(msg, m.keys.VolDown):
		if m.playback == nil || !m.playerControllable() {
			return m, nil
		}
		return m, m.playback.AdjustVolume(-5)

	case key.Matches(msg, m.keys.Up):
		if m.Focus() == FocusChat {
			return m.scrollChatUp(1), nil
		}
		m.moveHighlight(-1)
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.Focus() == FocusChat {
			return m.scrollChatDown(1), nil
		}
		m.moveHighlight(1)
		if m.shouldLoadNextPage() {
			m.generation++
			m.loading = true
			if m.Mode() == ModeChannels && m.selectedCategory != nil {
				return m, m.fetchChannelsCmd(m.generation, *m.selectedCategory, m.nextCursor, true)
			}
			return m, m.fetchCategoriesCmd(m.generation, m.nextCursor, true)
		}
		return m, nil

	case key.Matches(msg, m.keys.PageUp):
		if m.Focus() != FocusChat {
			return m, nil
		}
		return m.scrollChatUp(m.chatPageSize()), nil

	case key.Matches(msg, m.keys.Home):
		if m.Focus() != FocusChat {
			return m, nil
		}
		return m.scrollChatHome(), nil

	case key.Matches(msg, m.keys.GoLive):
		if m.Focus() != FocusChat {
			return m, nil
		}
		m.chatFollowLiveNow()
		return m, nil

	case key.Matches(msg, m.keys.Select):
		// Enter on the chat panel starts composition (same as i), so Tab then
		// Enter is a natural path into the draft. Enter while composing still
		// sends, via updateComposingKey.
		if m.Focus() == FocusChat {
			return m.enterCompose()
		}
		return m.selectHighlighted()
	}
	return m, nil
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
// the draft, and closes help so the new focus marker is visible. When layout
// hides either panel, Tab is a no-op.
func (m Model) switchPanelFocus() Model {
	if !m.listVisible() || !m.chatColumnVisible() {
		return m
	}
	m.helpVisible = false
	m.composing = false
	if m.focus == FocusChat {
		m.focus = FocusList
	} else {
		m.focus = FocusChat
	}
	return m
}

// enterCompose focuses the chat panel and opens the message input when sending
// is available. The existing draft, if any, stays in the input.
func (m Model) enterCompose() (tea.Model, tea.Cmd) {
	if !m.showChat || m.chatHiddenByWidth() {
		return m, nil
	}
	if !m.chatHasToken() && (m.chat != nil || m.chatPanel.channel != "") {
		m.chatPanel.sendWarning = m.t("chat.warn.no_token")
		return m, nil
	}
	if m.hasChatPanel() || m.chat != nil {
		m.focus = FocusChat
	}
	m.composing = true
	return m, nil
}

func (m Model) playerControllable() bool {
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting:
		return true
	default:
		return false
	}
}

func (m Model) updateComposingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Clear):
		// Esc leaves composition and stays on the chat panel; draft is kept.
		m.composing = false
		return m, nil

	case key.Matches(msg, m.keys.Select):
		return m.submitComposeMessage()

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.composeDraft = trimLastRune(m.composeDraft)
		m.clearComposeTooLongWarning()
		return m, nil

	case msg.Type == tea.KeySpace, key.Matches(msg, m.keys.Pause):
		m.composeDraft += " "
		m.clearComposeTooLongWarning()
		return m, nil

	case key.Matches(msg, m.keys.VolUp):
		m.composeDraft += "+"
		m.clearComposeTooLongWarning()
		return m, nil

	case key.Matches(msg, m.keys.VolDown):
		m.composeDraft += "-"
		m.clearComposeTooLongWarning()
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.composeDraft += string(msg.Runes)
		m.clearComposeTooLongWarning()
		return m, nil
	}
	return m, nil
}

// clearComposeTooLongWarning drops the length warning once the draft fits again,
// so the footer stops claiming the message is too long while the user edits.
func (m *Model) clearComposeTooLongWarning() {
	if m.chatPanel.sendWarning == "" {
		return
	}
	if m.chatPanel.sendWarning != m.t("chat.warn.too_long") {
		return
	}
	if !m.composeDraftOverLimit() {
		m.chatPanel.sendWarning = ""
	}
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}
