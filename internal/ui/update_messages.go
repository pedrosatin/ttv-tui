package ui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// One function per message that update dispatches.

func (m Model) applyHostedNotice(msg hostedNoticeMsg) (tea.Model, tea.Cmd) {
	if msg.gen == m.notices.gen {
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
		m.list.categories = make([]catalog.Category, len(msg.Page.Items))
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
		m.list.channels = make([]catalog.Channel, len(msg.Page.Items))
		copy(m.list.channels, msg.Page.Items)
	}
	m.rememberListLanguages()
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
	// Prefer exact name then viewers (catalog.PreferSearchCategories).
	cats := catalog.PreferSearchCategories(msg.Query, msg.Categories)
	m.list.search.categories = make([]catalog.Category, len(cats))
	copy(m.list.search.categories, cats)
	// Prefer exact login, exact display name, then live among clones
	// (catalog.PreferSearchChannels).
	// Lives here so every Source gets the same Enter/highlight target.
	items := catalog.PreferSearchChannels(msg.Query, msg.Page.Items)
	m.list.channels = make([]catalog.Channel, len(items))
	copy(m.list.channels, items)
	m.refreshSavedAvailability(items)
	m.list.nextCursor = msg.Page.Cursor
	m.list.hasMore = msg.Page.HasMore
	m.settleHighlightAfterLoad()
	return m, nil
}

func (m Model) applySavedAvailability(msg SavedAvailabilityMsg) (tea.Model, tea.Cmd) {
	if !msg.ChannelLookup && msg.Generation != m.list.hydrateGen {
		return m, nil
	}
	if msg.Err != nil {
		// Keep [unverified]; never surface a hard error for this soft check.
		return m, nil
	}
	// A batch lookup is not a category listing: a channel without stream data
	// has no evidence and must not be inferred live.
	known := slices.DeleteFunc(slices.Clone(msg.Channels), func(c catalog.Channel) bool {
		return c.Availability == catalog.AvailabilityUnknown
	})
	m.refreshSavedAvailability(known)
	if sel := m.list.selectedChannel; sel != nil {
		// The channel being watched may have been opened with no evidence of its
		// state (a pasted URL); the bar shows what the lookup found.
		updated := m.withLoadedAvailability([]catalog.Channel{*sel}, known)[0]
		m.list.selectedChannel = &updated
		if m.player.login != "" && samePlayerLabel(updated.Platform, m.player.login, updated.PlayerLabel()) {
			m.player.target.Channel = updated
		}
	}
	m.reconcileHighlight()
	return m, nil
}

// applyChannelSelected records the choice and starts the player and the chat.
func (m Model) applyChannelSelected(msg ChannelSelectedMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		// The player was already stopped for the exit; nothing would stop this one.
		return m, nil
	}
	if msg.Enter != m.list.enters {
		// A later Enter chose another row; its message is the selection.
		return m, nil
	}
	selected := msg.Channel
	m.list.selectedChannel = &selected
	m.clearStaleStatusErrors()

	if platform.IsNonTwitch(selected.Platform) && strings.TrimSpace(selected.URL) == "" {
		m.warnStatus(m.t("player.warn.missing_url"))
		return m, nil
	}

	m.queueRecent(selected)
	var cmds []tea.Cmd
	if m.store != nil {
		cmds = append(cmds, m.saveStoreCmd())
	}
	target := PlayTarget{
		Channel: selected,
	}
	label := selected.PlayerLabel()
	if label == "" {
		label = selected.Login
	}
	if m.playback != nil {
		// The player keeps the volume of a channel it restarts.
		if label != m.player.login {
			m.player.volume = 100
		}
		m.player.target = target
		m.player.login = label
		m.player.state = PlayerStateStarting
		m.player.lastLine = ""
		m.player.failure = PlayerFailureNone
		m.player.breakElapsed = 0
		m.player.selection++
		m.player.awaiting = m.player.selection
		m.player.stopAfterStart = false
		cmds = append(cmds, m.playback.StartTarget(target, m.player.selection))
	}
	if needsStreamLookup(selected) {
		// Ask its platform now, so the bar and the recents row say whether it
		// is live, with title and viewers.
		if stateCmd := m.fetchChannelStateCmd(selected); stateCmd != nil {
			cmds = append(cmds, stateCmd)
		}
	}
	// Only a platform without a chat gets the "unsupported" panel; one with a
	// read-only chat still joins it and its capabilities hide the input.
	hasChat := true
	if m.chat.session != nil {
		_, hasChat = m.chat.session.Capabilities(selected)
	}
	if !hasChat {
		cmds = append(cmds, m.chat.session.Stop())
		m.setChatUnsupported(selected)
	} else if m.chat.session != nil || m.chat.panel.channel != "" {
		if chatCmd := m.startChatChannel(selected); chatCmd != nil {
			cmds = append(cmds, chatCmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// needsStreamLookup reports a channel opened without what the bar shows about
// its stream: one with no evidence of its state (a pasted URL), or a search hit
// that says it is live and nothing else. A row of a category listing has title
// and viewers, or hides the count on purpose.
func needsStreamLookup(ch catalog.Channel) bool {
	if ch.Viewers != nil {
		return false
	}
	switch ch.Availability {
	case catalog.AvailabilityUnknown:
		return true
	case catalog.AvailabilityLive:
		return ch.Title == nil
	default:
		return false
	}
}

func (m Model) applyStoreSaved(msg storeSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.warnStatus(m.t("store.save_failed"))
	} else if msg.Notice != "" {
		m.clearStatusNotice()
		m.notices.status = m.t(msg.Notice)
		if msg.Name != "" {
			m.notices.status = fmt.Sprintf(m.notices.status, sanitizeScreenText(msg.Name))
		}
	}
	return m, nil
}

// configSavedMsg ends a save of the channel filters or the panel layout.
type configSavedMsg struct {
	Err error
}

// ErrConfigRemoved is what a save of the channel filters or the panel layout
// returns when it went through but had to recreate a config deleted while the
// app ran. The deleted file took the token with it. Its text is the catalog key
// of the notice.
var ErrConfigRemoved = errors.New("config.removed")

// applyConfigSaved warns when the config was not saved, or when the save found
// it deleted. A failed change still holds for this run.
func (m Model) applyConfigSaved(msg configSavedMsg) (tea.Model, tea.Cmd) {
	switch {
	case errors.Is(msg.Err, ErrConfigRemoved):
		m.warnStatus(m.t("config.removed"))
	case msg.Err != nil:
		m.warnStatus(m.t("config.save_failed"))
	}
	return m, nil
}

// applyPlayerStatus folds one player event into the model, unless it belongs
// to a playback the model already left.
func (m Model) applyPlayerStatus(msg PlayerStatusMsg) (tea.Model, tea.Cmd) {
	var stop tea.Cmd
	if msg.Selection != 0 && msg.Selection == m.player.awaiting && m.player.stopAfterStart {
		// The Stop reply was dropped with the switch; this Start may have
		// attached after it ran.
		m.player.stopAfterStart = false
		msg.State, msg.Failure, msg.LastLine = PlayerStateEnded, PlayerFailureNone, ""
		stop = m.playback.Stop()
	}
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
	m.player.muted = msg.Muted
	if m.player.state != stateBefore || m.player.volume != volumeBefore {
		m.clearVolumeLimitNotice()
	}
	return m, stop
}

// acceptPlayerStatus tracks the playback generation and reports whether msg is
// current. While switching channels only the reply to the Start of the latest
// selection passes, and a Start reply passes only then.
func (m *Model) acceptPlayerStatus(msg PlayerStatusMsg) bool {
	if m.player.awaiting != 0 || msg.Selection != 0 {
		if msg.Selection != m.player.awaiting {
			return false
		}
		m.player.awaiting = 0
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
		m.player.downloadFailed = msg.DownloadFailed
		// A classified offline failure is fresh evidence for this channel:
		// the listing may still show stale viewers from before play.
		if msg.Failure == PlayerFailureOffline || msg.Failure == PlayerFailureVOD {
			m.markChannelOffline(m.player.login)
			m.reconcileHighlight()
		}
	} else {
		m.player.failure = PlayerFailureNone
		m.player.downloadFailed = false
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

func (m Model) applyComponentProgress(msg ComponentProgressMsg) (tea.Model, tea.Cmd) {
	if msg.Done {
		if m.notices.componentProgress {
			m.clearStatusNotice()
		}
		return m, nil
	}
	m.notices.gen++
	if msg.Waiting {
		m.notices.status = m.t("components.waiting_lock")
	} else {
		m.notices.status = fmt.Sprintf(m.t("components.downloading_progress"), msg.Component, msg.Percent)
	}
	m.notices.statusWarns = false
	m.notices.componentProgress = true
	return m, nil
}
