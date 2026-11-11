package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// One function per message that update dispatches.

func (m Model) applyHostedNotice(msg hostedNoticeMsg) (tea.Model, tea.Cmd) {
	if m.notices.status == msg.text {
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
		m.notices.auth = m.t("auth.token_invalid_notice")
	}
	return m, nil
}

func (m Model) applyCategoriesLoaded(msg CategoriesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.list.generation {
		return m, nil
	}
	m.list.loading = false
	m.list.loadFailed = msg.Err != nil
	if msg.Err != nil {
		m.list.err = msg.Err
		return m, nil
	}
	m.list.err = nil
	if msg.Append {
		existing := make(map[string]struct{}, len(m.list.categories))
		for _, c := range m.list.categories {
			existing[c.ID] = struct{}{}
		}
		for _, c := range msg.Page.Items {
			if _, dup := existing[c.ID]; !dup {
				m.list.categories = append(m.list.categories, c)
				existing[c.ID] = struct{}{}
			}
		}
	} else {
		m.list.categories = make([]twitch.Category, len(msg.Page.Items))
		copy(m.list.categories, msg.Page.Items)
	}
	m.list.nextCursor = msg.Page.Cursor
	m.list.hasMore = msg.Page.HasMore
	m.reconcileHighlight()
	return m, nil
}

func (m Model) applyChannelsLoaded(msg ChannelsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.list.generation {
		return m, nil
	}
	m.list.loading = false
	m.list.loadFailed = msg.Err != nil
	if msg.Err != nil {
		m.list.err = msg.Err
		return m, nil
	}
	m.list.err = nil
	if msg.Append {
		existing := make(map[string]struct{}, len(m.list.channels))
		for _, c := range m.list.channels {
			existing[c.ID] = struct{}{}
		}
		for _, c := range msg.Page.Items {
			if _, dup := existing[c.ID]; !dup {
				m.list.channels = append(m.list.channels, c)
				existing[c.ID] = struct{}{}
			}
		}
	} else {
		m.list.channels = make([]twitch.Channel, len(msg.Page.Items))
		copy(m.list.channels, msg.Page.Items)
	}
	m.refreshSavedAvailability(msg.Page.Items)
	m.list.nextCursor = msg.Page.Cursor
	m.list.hasMore = msg.Page.HasMore
	m.settleHighlightAfterLoad()
	return m, nil
}

func (m Model) applySearchLoaded(msg SearchLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.list.generation {
		return m, nil
	}
	m.list.loading = false
	m.list.loadFailed = msg.Err != nil
	if msg.Err != nil {
		m.list.err = msg.Err
		return m, nil
	}
	m.list.err = nil
	// Prefer exact name then viewers (twitch.PreferSearchCategories).
	cats := twitch.PreferSearchCategories(msg.Query, msg.Categories)
	m.list.search.categories = make([]twitch.Category, len(cats))
	copy(m.list.search.categories, cats)
	// Prefer exact login, exact display name, then live among clones
	// (twitch.PreferSearchChannels).
	// Lives here so every Source gets the same Enter/highlight target.
	items := twitch.PreferSearchChannels(msg.Query, msg.Page.Items)
	m.list.channels = make([]twitch.Channel, len(items))
	copy(m.list.channels, items)
	m.refreshSavedAvailability(items)
	m.list.nextCursor = msg.Page.Cursor
	m.list.hasMore = msg.Page.HasMore
	m.settleHighlightAfterLoad()
	return m, nil
}

func (m Model) applySavedAvailability(msg SavedAvailabilityMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.list.hydrateGen {
		return m, nil
	}
	if msg.Err != nil {
		// Keep [unverified]; never surface a hard error for this soft check.
		return m, nil
	}
	// A batch lookup is not a category listing: a channel without stream data
	// has no evidence and must not be inferred live.
	known := slices.DeleteFunc(slices.Clone(msg.Channels), func(c twitch.Channel) bool {
		return c.Availability == twitch.AvailabilityUnknown
	})
	m.refreshSavedAvailability(known)
	m.reconcileHighlight()
	return m, nil
}

// applyChannelSelected records the choice and starts the player and the chat.
func (m Model) applyChannelSelected(msg ChannelSelectedMsg) (tea.Model, tea.Cmd) {
	selected := msg.Channel
	m.list.selectedChannel = &selected
	m.clearStaleStatusErrors()
	m.queueRecent(selected)
	var cmds []tea.Cmd
	if m.store != nil {
		cmds = append(cmds, m.saveStoreCmd())
	}
	if m.playback != nil {
		// The player keeps the volume of a channel it restarts.
		if selected.Login != m.player.login {
			m.player.volume = 100
		}
		m.player.login = selected.Login
		m.player.state = PlayerStateStarting
		m.player.lastLine = ""
		m.player.failure = PlayerFailureNone
		m.player.breakElapsed = 0
		m.player.switching = true
		cmds = append(cmds, m.playback.Start(selected.Login))
	}
	if m.chat.session != nil || m.chat.panel.channel != "" {
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
		m.notices.status = m.t(msg.Notice)
	}
	return m, nil
}

// configSavedMsg ends a save of the channel filters or the panel layout.
type configSavedMsg struct {
	Err error
}

// applyConfigSaved warns when the config was not saved. The change still holds
// for this run.
func (m Model) applyConfigSaved(msg configSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.warnStatus(m.t("config.save_failed"))
	}
	return m, nil
}

// applyPlayerStatus folds one player event into the model, unless it belongs
// to a playback the model already left.
func (m Model) applyPlayerStatus(msg PlayerStatusMsg) (tea.Model, tea.Cmd) {
	if !m.acceptPlayerStatus(msg) {
		return m, nil
	}
	stateBefore, volumeBefore := m.player.state, m.player.volume
	m.setPlayerState(msg)
	m.setPlayerFailure(msg)
	m.player.breakElapsed = msg.BreakElapsed
	m.warnPlayerDegraded(msg)
	if msg.State == PlayerStatePlaying && !m.player.windowNotified && m.hosted.host == nil {
		// mpv opens its own window; someone new to the app may not notice it.
		m.player.windowNotified = true
		m.player.windowNotice = true
	}
	if msg.Volume > 0 || msg.State == PlayerStatePlaying || msg.State == PlayerStatePaused || msg.State == PlayerStateBreak {
		m.player.volume = clampPlayerVolume(msg.Volume)
	}
	if m.player.state != stateBefore || m.player.volume != volumeBefore {
		m.clearVolumeLimitNotice()
	}
	return m, nil
}

// acceptPlayerStatus tracks the playback generation and reports whether msg is
// current. While switching channels only the event that opens the new playback
// passes.
func (m *Model) acceptPlayerStatus(msg PlayerStatusMsg) bool {
	if m.player.switching {
		if !msg.FromStart {
			return false
		}
		m.player.switching = false
		m.player.gen = msg.Generation
	} else if msg.Generation != 0 && m.player.gen != 0 && msg.Generation < m.player.gen {
		// Stale generation from a previous channel — discard.
		return false
	} else if msg.Generation > m.player.gen {
		m.player.gen = msg.Generation
	}
	return true
}

// setPlayerState copies the state, the login and the line shown with them.
func (m *Model) setPlayerState(msg PlayerStatusMsg) {
	if msg.State != m.player.state {
		// A pause in the mpv window never reaches the TUI as a key, so the
		// notice cannot wait for one: the bar needs the room for the state.
		m.player.windowNotice = false
	}
	m.player.state = msg.State
	if msg.Login != "" {
		m.player.login = msg.Login
	}
	if msg.State == PlayerStateError {
		m.player.lastLine = msg.LastLine
		m.player.infoLine = ""
	} else {
		m.player.lastLine = ""
		m.player.infoLine = ""
		if strings.Contains(strings.ToLower(msg.LastLine), "[info]") {
			m.player.infoLine = sanitizeScreenText(strings.TrimSpace(msg.LastLine))
		}
	}
}

func (m *Model) setPlayerFailure(msg PlayerStatusMsg) {
	if msg.State == PlayerStateError {
		m.player.failure = msg.Failure
		// A classified offline failure is fresh evidence for this channel:
		// the listing may still show stale viewers from before play.
		if msg.Failure == PlayerFailureOffline {
			m.markChannelOffline(m.player.login)
			m.reconcileHighlight()
		}
	} else {
		m.player.failure = PlayerFailureNone
	}
}

// warnPlayerDegraded warns once per playback about each degraded condition.
func (m *Model) warnPlayerDegraded(msg PlayerStatusMsg) {
	if msg.ContainmentDegraded && msg.Generation != m.player.containmentWarnedGen {
		m.player.containmentWarnedGen = msg.Generation
		m.warnStatus(m.t("player.warn.containment"))
	}
	if msg.InputConfFailed && msg.Generation != m.player.keysWarnedGen {
		// Every event of that playback carries the flag; warn once.
		m.player.keysWarnedGen = msg.Generation
		m.warnStatus(m.t("player.warn.input_conf"))
	}
}
