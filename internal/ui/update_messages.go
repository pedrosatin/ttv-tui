package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// One function per message that update dispatches.

func (m Model) applyHostedNotice(msg hostedNoticeMsg) (tea.Model, tea.Cmd) {
	if m.statusNotice == msg.text {
		m.clearStatusNotice()
	}
	return m, nil
}

// applyBatch runs each command of the batch and feeds its message back in.
func (m Model) applyBatch(msg tea.BatchMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cur tea.Model = m
	for _, cmd := range msg {
		if cmd == nil {
			continue
		}
		var nextCmd tea.Cmd
		cur, nextCmd = cur.Update(cmd())
		if nextCmd != nil {
			cmds = append(cmds, nextCmd)
		}
	}
	return cur, tea.Batch(cmds...)
}

func (m Model) applyAuthValidated(msg AuthValidatedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.authNotice = m.t("auth.token_invalid_notice")
	}
	return m, nil
}

func (m Model) applyCategoriesLoaded(msg CategoriesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.generation {
		return m, nil
	}
	m.loading = false
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}
	m.err = nil
	if msg.Append {
		existing := make(map[string]struct{}, len(m.categories))
		for _, c := range m.categories {
			existing[c.ID] = struct{}{}
		}
		for _, c := range msg.Page.Items {
			if _, dup := existing[c.ID]; !dup {
				m.categories = append(m.categories, c)
				existing[c.ID] = struct{}{}
			}
		}
	} else {
		m.categories = make([]twitch.Category, len(msg.Page.Items))
		copy(m.categories, msg.Page.Items)
	}
	m.nextCursor = msg.Page.Cursor
	m.hasMore = msg.Page.HasMore
	m.reconcileHighlight()
	return m, nil
}

func (m Model) applyChannelsLoaded(msg ChannelsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.generation {
		return m, nil
	}
	m.loading = false
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}
	m.err = nil
	if msg.Append {
		existing := make(map[string]struct{}, len(m.channels))
		for _, c := range m.channels {
			existing[c.ID] = struct{}{}
		}
		for _, c := range msg.Page.Items {
			if _, dup := existing[c.ID]; !dup {
				m.channels = append(m.channels, c)
				existing[c.ID] = struct{}{}
			}
		}
	} else {
		m.channels = make([]twitch.Channel, len(msg.Page.Items))
		copy(m.channels, msg.Page.Items)
	}
	m.refreshSavedAvailability(msg.Page.Items)
	m.nextCursor = msg.Page.Cursor
	m.hasMore = msg.Page.HasMore
	m.settleHighlightAfterLoad()
	return m, nil
}

func (m Model) applySearchLoaded(msg SearchLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.generation {
		return m, nil
	}
	m.loading = false
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}
	m.err = nil
	// Prefer exact name then viewers (twitch.PreferSearchCategories).
	cats := twitch.PreferSearchCategories(msg.Query, msg.Categories)
	m.searchCategories = make([]twitch.Category, len(cats))
	copy(m.searchCategories, cats)
	// Prefer exact login, exact display name, then live among clones
	// (twitch.PreferSearchChannels).
	// Lives here so every Source gets the same Enter/highlight target.
	items := twitch.PreferSearchChannels(msg.Query, msg.Page.Items)
	m.channels = make([]twitch.Channel, len(items))
	copy(m.channels, items)
	m.refreshSavedAvailability(items)
	m.nextCursor = msg.Page.Cursor
	m.hasMore = msg.Page.HasMore
	m.settleHighlightAfterLoad()
	return m, nil
}

func (m Model) applySavedAvailability(msg SavedAvailabilityMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.hydrateGen {
		return m, nil
	}
	if msg.Err != nil {
		// Keep [unverified]; never surface a hard error for this soft check.
		return m, nil
	}
	m.refreshSavedAvailability(msg.Channels)
	return m, nil
}

// applyChannelSelected records the choice and starts the player and the chat.
func (m Model) applyChannelSelected(msg ChannelSelectedMsg) (tea.Model, tea.Cmd) {
	selected := msg.Channel
	m.selectedChannel = &selected
	m.clearStaleStatusErrors()
	m.queueRecent(selected)
	var cmds []tea.Cmd
	if m.store != nil {
		cmds = append(cmds, m.saveStoreCmd())
	}
	if m.playback != nil {
		// The player keeps the volume of a channel it restarts.
		if selected.Login != m.playerLogin {
			m.playerVolume = 100
		}
		m.playerLogin = selected.Login
		m.playerState = PlayerStateStarting
		m.playerLastLine = ""
		m.playerFailure = PlayerFailureNone
		m.playerBreakElapsed = 0
		m.playerSwitching = true
		cmds = append(cmds, m.playback.Start(selected.Login))
	}
	if m.chat != nil || m.chatPanel.channel != "" {
		if chatCmd := m.startChatChannel(selected.Login); chatCmd != nil {
			cmds = append(cmds, chatCmd)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) applyStoreSaved(msg storeSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.warnStatus(m.t("store.save_failed"))
	} else if msg.Notice != "" {
		m.clearStatusNotice()
		m.statusNotice = m.t(msg.Notice)
	}
	return m, nil
}

// applyPlayerStatus folds one player event into the model, unless it belongs
// to a playback the model already left.
func (m Model) applyPlayerStatus(msg PlayerStatusMsg) (tea.Model, tea.Cmd) {
	if !m.acceptPlayerStatus(msg) {
		return m, nil
	}
	stateBefore, volumeBefore := m.playerState, m.playerVolume
	m.setPlayerState(msg)
	m.setPlayerFailure(msg)
	m.playerBreakElapsed = msg.BreakElapsed
	m.warnPlayerDegraded(msg)
	if msg.State == PlayerStatePlaying && !m.playerWindowNotified && m.videoHost == nil {
		// mpv opens its own window; someone new to the app may not notice it.
		m.playerWindowNotified = true
		m.playerWindowNotice = true
	}
	if msg.Volume > 0 || msg.State == PlayerStatePlaying || msg.State == PlayerStatePaused || msg.State == PlayerStateBreak {
		m.playerVolume = clampPlayerVolume(msg.Volume)
	}
	if m.playerState != stateBefore || m.playerVolume != volumeBefore {
		m.clearVolumeLimitNotice()
	}
	return m, nil
}

// acceptPlayerStatus tracks the playback generation and reports whether msg is
// current. While switching channels only the event that opens the new playback
// passes.
func (m *Model) acceptPlayerStatus(msg PlayerStatusMsg) bool {
	if m.playerSwitching {
		if !msg.FromStart {
			return false
		}
		m.playerSwitching = false
		m.playerGen = msg.Generation
	} else if msg.Generation != 0 && m.playerGen != 0 && msg.Generation < m.playerGen {
		// Stale generation from a previous channel — discard.
		return false
	} else if msg.Generation > m.playerGen {
		m.playerGen = msg.Generation
	}
	return true
}

// setPlayerState copies the state, the login and the line shown with them.
func (m *Model) setPlayerState(msg PlayerStatusMsg) {
	if msg.State != m.playerState {
		// A pause in the mpv window never reaches the TUI as a key, so the
		// notice cannot wait for one: the bar needs the room for the state.
		m.playerWindowNotice = false
	}
	m.playerState = msg.State
	if msg.Login != "" {
		m.playerLogin = msg.Login
	}
	if msg.State == PlayerStateError {
		m.playerLastLine = msg.LastLine
		m.playerInfoLine = ""
	} else {
		m.playerLastLine = ""
		m.playerInfoLine = ""
		if strings.Contains(strings.ToLower(msg.LastLine), "[info]") {
			m.playerInfoLine = sanitizeScreenText(strings.TrimSpace(msg.LastLine))
		}
	}
}

func (m *Model) setPlayerFailure(msg PlayerStatusMsg) {
	if msg.State == PlayerStateError {
		m.playerFailure = msg.Failure
		// A classified offline failure is fresh evidence for this channel:
		// the listing may still show stale viewers from before play.
		if msg.Failure == PlayerFailureOffline {
			m.markChannelOffline(m.playerLogin)
		}
	} else {
		m.playerFailure = PlayerFailureNone
	}
}

// warnPlayerDegraded warns once per playback about each degraded condition.
func (m *Model) warnPlayerDegraded(msg PlayerStatusMsg) {
	if msg.ContainmentDegraded && msg.Generation != m.playerContainmentWarnedGen {
		m.playerContainmentWarnedGen = msg.Generation
		m.warnStatus(m.t("player.warn.containment"))
	}
	if msg.InputConfFailed && msg.Generation != m.playerKeysWarnedGen {
		// Every event of that playback carries the flag; warn once.
		m.playerKeysWarnedGen = msg.Generation
		m.warnStatus(m.t("player.warn.input_conf"))
	}
}
