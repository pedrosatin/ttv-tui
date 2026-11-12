package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// The stages below are the steps of handleKeyMsg, in its order. Each reports
// whether it consumed the key; one that did not leaves the model untouched.

// handled marks a key as consumed by the handler whose result it wraps.
func handled(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd, bool) {
	return next, cmd, true
}

// clearKeyNotices drops the notices that last until the next key.
func (m *Model) clearKeyNotices() {
	m.notices.auth = ""
	m.notices.corrupt = ""
	m.clearStatusNotice()
	m.notices.ctrlC = ""
	m.chat.copyNotice = ""
	m.player.windowNotice = false
}

// handleAppKey answers the two keys that outrank the help: Ctrl+Q and Ctrl+C.
func (m Model) handleAppKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if key.Matches(msg, m.keys.Quit) {
		return handled(m.quit())
	}
	// Ctrl+C reaches a raw-mode TUI as a key, not SIGINT. It never quits;
	// only Ctrl+Q takes the shutdown path. Show a localized hint instead.
	if msg.Type == tea.KeyCtrlC {
		m.notices.ctrlC = m.t("status.ctrl_c_hint")
		return m, nil, true
	}
	return m, nil, false
}

// quit cancels the request in flight and stops the player and the chat.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.loading = false
	m.quitting = true
	var cmds []tea.Cmd
	if m.playback != nil {
		cmds = append(cmds, m.playback.Stop())
	}
	if m.chat.session != nil {
		cmds = append(cmds, m.chat.session.Stop())
	}
	cmds = append(cmds, tea.Quit)
	return m, tea.Batch(cmds...)
}

// handleGlobalKey answers the keys that work from any mode, even while typing.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	// F1 opens keyboard help at any time, even while typing.
	case key.Matches(msg, m.keys.HelpGlobal):
		m.toggleHelp()
		return m, nil, true

	// Ctrl+R returns to categories from anywhere, including compose.
	case key.Matches(msg, m.keys.Categories):
		return handled(m.returnToCategories())

	// Ctrl+X ends video without leaving the app (works while composing).
	case key.Matches(msg, m.keys.StopVideo):
		return handled(m.keyStopVideo())

	case key.Matches(msg, m.keys.ReconnectChat):
		return handled(m.keyReconnectChat())
	}
	return m, nil, false
}

func (m Model) keyStopVideo() (tea.Model, tea.Cmd) {
	if m.playback == nil {
		return m, nil
	}
	if m.player.state == PlayerStateNone || m.player.state == PlayerStateEnded {
		return m, nil
	}
	m.player.stopAfterStart = m.player.awaiting != 0
	return m, m.playback.Stop()
}

func (m Model) keyReconnectChat() (tea.Model, tea.Cmd) {
	if m.chat.session == nil || m.chat.panel.channel == "" {
		return m, nil
	}
	m.chat.panel.state = ChatStateConnecting
	m.chat.panel.switching = true
	return m, m.chat.session.Reconnect()
}

// handleLayoutKey answers the layout controls, which work from any mode
// (compose, search, filter), and the hosted window's own.
func (m Model) handleLayoutKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if key.Matches(msg, m.keys.CycleLayout, m.keys.ToggleList, m.keys.ToggleChat) && !m.chat.hasPanel() {
		m.keepListWithoutChat()
		return m, nil, true
	}
	if key.Matches(msg, m.keys.CycleLayout) {
		m.cycleLayout()
		return m, nil, true
	}
	if key.Matches(msg, m.keys.ToggleList) {
		m.toggleListPanel()
		return m, nil, true
	}
	if key.Matches(msg, m.keys.ToggleChat) {
		m.toggleChatPanel()
		return m, nil, true
	}
	if ok, cmd := m.handleHostedResizeKey(msg); ok {
		return m, cmd, true
	}
	return m, nil, false
}

// handleModeKey hands the key to the input mode that owns it: search, filter,
// the filter menu or composition. Tab sits between the menu and composition.
func (m Model) handleModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// The filter menu is a visible overlay in every layout. Other list
	// inputs stay suspended until the list is shown again.
	if m.hiddenListKey(msg) {
		m.warnStatus(m.t("layout.hidden_list_key"))
		return m, nil, true
	}

	if m.list.search.active && m.listVisible() {
		return handled(m.updateSearchingKey(msg))
	}

	if m.list.filtering && m.listVisible() {
		return handled(m.updateFilteringKey(msg))
	}

	if m.list.prefs.menuOpen {
		return handled(m.updateChannelFilterMenuKey(msg))
	}

	// Tab switches list ↔ chat in every panel state: list focus, chat focus,
	// and composing. It never sends or clears the draft.
	if isSwitchFocusKey(msg, m.keys) {
		return m.switchPanelFocus(), nil, true
	}

	if m.chat.composing && m.Focus() == FocusChat && m.layout.showChat && !m.chatHiddenByWidth() {
		return handled(m.updateComposingKey(msg))
	}
	return m, nil, false
}

// handleCommandKey answers the keys that start an action on the list or chat.
func (m Model) handleCommandKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Favorite):
		return handled(m.keyFavorite())

	case key.Matches(msg, m.keys.ChannelFilter):
		return handled(m.keyChannelFilter())

	case key.Matches(msg, m.keys.SwitchPlatform):
		return handled(m.keySwitchPlatform())

	case key.Matches(msg, m.keys.Help):
		m.toggleHelp()
		return m, nil, true

	case key.Matches(msg, m.keys.Clear):
		return handled(m.keyClear())

	case key.Matches(msg, m.keys.Compose):
		return handled(m.enterCompose())

	case key.Matches(msg, m.keys.CopyMessage):
		return handled(m.keyCopyMessage())

	case key.Matches(msg, m.keys.Filter):
		return handled(m.keyFilter())

	case key.Matches(msg, m.keys.Search):
		return handled(m.keySearch())

	case key.Matches(msg, m.keys.Retry):
		return handled(m.keyRetry())
	}
	return m, nil, false
}

func (m Model) keyFavorite() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m, nil
	}
	return m.toggleFavoriteHighlighted()
}

func (m Model) keyChannelFilter() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m, nil
	}
	return m.openChannelFilterMenu(), nil
}

// keyClear is Esc outside any input mode: it leaves the chat, then clears the
// filter, then steps back one level.
func (m Model) keyClear() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		m.setFocus(FocusList)
		return m, nil
	}
	if m.list.filter != "" {
		m.list.filter = ""
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
}

func (m Model) keyCopyMessage() (tea.Model, tea.Cmd) {
	if m.Focus() != FocusChat {
		return m.showCopyFocusWarning()
	}
	return m.copyChatMessage()
}

func (m Model) keyFilter() (tea.Model, tea.Cmd) {
	m.setFocus(FocusList)
	m.clearStaleStatusErrors()
	m.list.filtering = true
	return m, nil
}

func (m Model) keySearch() (tea.Model, tea.Cmd) {
	m.setFocus(FocusList)
	if q := strings.TrimSpace(m.list.filter); q != "" {
		return m.startSearch(q)
	}
	m.clearStaleStatusErrors()
	m.list.search.active = true
	m.list.search.input = ""
	return m, nil
}

// canRetryPlayer reports whether r restarts the player. A player error takes
// precedence over a list error, as in the status bar lead.
func (m Model) canRetryPlayer() bool {
	return m.player.state == PlayerStateError && m.playback != nil && m.player.login != ""
}

func (m Model) keyRetry() (tea.Model, tea.Cmd) {
	if m.canRetryPlayer() {
		return m.retryPlayback()
	}
	return m.retryCurrentMode()
}

// handlePlayerKey answers pause and volume.
func (m Model) handlePlayerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Pause):
		return handled(m.keyPause())

	case key.Matches(msg, m.keys.VolUp):
		return handled(m.adjustVolume(5))

	case key.Matches(msg, m.keys.VolDown):
		return handled(m.adjustVolume(-5))
	}
	return m, nil, false
}

func (m Model) keyPause() (tea.Model, tea.Cmd) {
	if m.playback == nil || !m.player.controllable() {
		return m, nil
	}
	return m, m.playback.TogglePause()
}

// handleMoveKey answers movement and Enter, in the list or in the chat.
func (m Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Up):
		return handled(m.keyUp())

	case key.Matches(msg, m.keys.Down):
		return handled(m.keyDown())

	case key.Matches(msg, m.keys.PageUp):
		return handled(m.keyPageUp())

	case key.Matches(msg, m.keys.Home):
		return handled(m.keyHome())

	case key.Matches(msg, m.keys.GoLive):
		return handled(m.keyGoLive())

	case key.Matches(msg, m.keys.Select):
		return handled(m.keySelect())
	}
	return m, nil, false
}

func (m Model) keyUp() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m.scrollChatUp(1), nil
	}
	if m.list.hardErr() {
		return m, nil
	}
	m.moveHighlight(-1)
	return m, nil
}

// keyDown also asks for the next page when the highlight nears the end.
func (m Model) keyDown() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m.scrollChatDown(1), nil
	}
	if m.list.hardErr() {
		return m, nil
	}
	m.moveHighlight(1)
	if m.shouldLoadNextPage() {
		m.list.generation++
		m.list.loading = true
		if m.Mode() == ModeChannels && m.list.selectedCategory != nil {
			return m, m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, m.list.nextCursor, true)
		}
		return m, m.fetchCategoriesCmd(m.list.generation, m.list.nextCursor, true)
	}
	return m, nil
}

func (m Model) keyPageUp() (tea.Model, tea.Cmd) {
	if m.Focus() != FocusChat {
		return m, nil
	}
	return m.scrollChatUp(m.chatPageSize()), nil
}

func (m Model) keyHome() (tea.Model, tea.Cmd) {
	if m.Focus() != FocusChat {
		return m, nil
	}
	return m.scrollChatHome(), nil
}

func (m Model) keyGoLive() (tea.Model, tea.Cmd) {
	if m.Focus() != FocusChat {
		return m, nil
	}
	m.chatFollowLiveNow()
	return m, nil
}

// keySelect is Enter. On the chat panel it starts composition (same as i), so
// Tab then Enter is a natural path into the draft. Enter while composing still
// sends, via updateComposingKey.
func (m Model) keySelect() (tea.Model, tea.Cmd) {
	if m.Focus() == FocusChat {
		return m.enterCompose()
	}
	return m.selectHighlighted()
}
