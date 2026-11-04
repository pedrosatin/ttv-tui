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
	Select        key.Binding
	SwitchFocus   key.Binding
	Filter        key.Binding
	Search        key.Binding
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
	Help          key.Binding
	HelpGlobal    key.Binding
	Favorite      key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
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
		Search: key.NewBinding(
			key.WithKeys("ctrl+s"),
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
		// Ctrl+C is what someone new to terminal apps tries first. In raw mode
		// Bubble Tea delivers it as a key, not SIGINT, so it shares Ctrl+Q's
		// shutdown path. It is checked before every mode, composing included.
		Quit: key.NewBinding(
			key.WithKeys("ctrl+q", "ctrl+c"),
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
		Help: key.NewBinding(
			key.WithKeys("?", "f1"),
		),
		HelpGlobal: key.NewBinding(
			key.WithKeys("f1"),
		),
		Favorite: key.NewBinding(
			key.WithKeys("f"),
		),
	}
}

func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.authNotice = ""
	m.corruptNotice = ""
	m.statusNotice = ""
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

	// F1 toggles keyboard help at any time, even while typing.
	if key.Matches(msg, m.keys.HelpGlobal) {
		m.helpVisible = !m.helpVisible
		return m, nil
	}

	// When help overlay is visible, ? or Esc closes it.
	if m.helpVisible {
		if key.Matches(msg, m.keys.Help) || key.Matches(msg, m.keys.Clear) {
			m.helpVisible = false
			return m, nil
		}
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

	if key.Matches(msg, m.keys.Categories) {
		return m.returnToCategories()
	}

	if m.searching {
		return m.updateSearchingKey(msg)
	}

	if m.filtering {
		return m.updateFilteringKey(msg)
	}

	if m.composing && !m.chatHiddenByWidth() {
		return m.updateComposingKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Favorite):
		if m.Focus() == FocusChat {
			return m, nil
		}
		return m.toggleFavoriteHighlighted()

	case key.Matches(msg, m.keys.Help):
		m.helpVisible = !m.helpVisible
		return m, nil

	case key.Matches(msg, m.keys.SwitchFocus):
		if !m.hasChatPanel() || m.chatHiddenByWidth() {
			return m, nil
		}
		if m.Focus() == FocusChat {
			m.focus = FocusList
			m.composing = false
		} else {
			m.focus = FocusChat
		}
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
		// Esc steps back one level: from a category's channels or from search
		// results it returns to the categories, like Ctrl+R.
		if m.Mode() != ModeCategories {
			return m.returnToCategories()
		}
		return m, nil

	case key.Matches(msg, m.keys.Compose):
		if m.chatHiddenByWidth() {
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

	case key.Matches(msg, m.keys.Filter):
		m.focus = FocusList
		m.filtering = true
		return m, nil

	case key.Matches(msg, m.keys.Search):
		m.focus = FocusList
		if q := strings.TrimSpace(m.filter); q != "" {
			return m.startSearch(q)
		}
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
			return m, nil
		}
		m.moveHighlight(-1)
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.Focus() == FocusChat {
			return m, nil
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

	case key.Matches(msg, m.keys.Select):
		if m.Focus() == FocusChat {
			return m, nil
		}
		return m.selectHighlighted()
	}
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
		m.composing = false
		return m, nil

	case msg.Type == tea.KeyTab, key.Matches(msg, m.keys.SwitchFocus):
		m.composing = false
		m.focus = FocusList
		return m, nil

	case key.Matches(msg, m.keys.Select):
		return m.submitComposeMessage()

	case msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete:
		m.composeDraft = trimLastRune(m.composeDraft)
		return m, nil

	case msg.Type == tea.KeySpace, key.Matches(msg, m.keys.Pause):
		m.composeDraft += " "
		return m, nil

	case key.Matches(msg, m.keys.VolUp):
		m.composeDraft += "+"
		return m, nil

	case key.Matches(msg, m.keys.VolDown):
		m.composeDraft += "-"
		return m, nil

	case msg.Type == tea.KeyRunes:
		m.composeDraft += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}
