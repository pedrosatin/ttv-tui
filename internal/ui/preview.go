package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// PreviewState is the UI-facing status of the live preview shown in the bar.
// It is separate from the main PlayerState so a preview never overwrites it.
type PreviewState string

const (
	PreviewStateClosed   PreviewState = ""
	PreviewStateStarting PreviewState = "iniciando"
	PreviewStatePlaying  PreviewState = "tocando"
	PreviewStateBreak    PreviewState = "intervalo"
	PreviewStateOffline  PreviewState = "offline"
	PreviewStateFailed   PreviewState = "erro"
	PreviewStateEnded    PreviewState = "encerrado"
)

// previewSwitchDelay is how long the highlight must stay still before the
// open preview switches to the newly highlighted channel.
const previewSwitchDelay = 400 * time.Millisecond

// PreviewStatusMsg is delivered by the Playback bridge for the preview
// manager. Update discards messages whose Generation is older than the active
// preview generation.
type PreviewStatusMsg struct {
	Generation   uint64
	State        PreviewState
	Login        string
	LastLine     string
	BreakElapsed time.Duration
	Failure      PlayerFailure
	Selection    uint64
}

// previewState holds the open preview, if any.
type previewState struct {
	gen          uint64
	selection    uint64
	awaiting     uint64
	state        PreviewState
	login        string
	target       PlayTarget
	lastLine     string
	failure      PlayerFailure
	breakElapsed time.Duration
	// switchGen invalidates a pending debounce when the highlight moves again.
	switchGen uint64
}

// previewSwitchMsg fires after previewSwitchDelay with no further movement.
type previewSwitchMsg struct {
	gen uint64
}

// PreviewState returns the current preview state.
func (m Model) PreviewState() PreviewState {
	return m.preview.state
}

// PreviewLogin returns the login shown for the open preview.
func (m Model) PreviewLogin() string {
	return m.preview.login
}

// previewOpen reports whether a preview is loading or playing.
func (m Model) previewOpen() bool {
	switch m.preview.state {
	case PreviewStateStarting, PreviewStatePlaying, PreviewStateBreak, PreviewStateOffline, PreviewStateFailed:
		return true
	default:
		return false
	}
}

// previewActive reports a preview that still owns a process (not merely a
// failed/offline notice left on the bar).
func (m Model) previewActive() bool {
	switch m.preview.state {
	case PreviewStateStarting, PreviewStatePlaying, PreviewStateBreak:
		return true
	default:
		return false
	}
}

func (m Model) previewLabel() string {
	label := m.preview.login
	ch := m.preview.target.Channel
	if label == "" || platform.IsTwitch(ch.Platform) || !samePlayerLabel(ch.Platform, ch.PlayerLabel(), label) {
		return label
	}
	name := strings.TrimSpace(ch.DisplayName)
	if name == "" {
		name = strings.TrimSpace(ch.StableID())
	}
	if name == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", name, platform.DisplayName(ch.Platform))
}

func (m Model) previewStatusText() string {
	switch m.preview.state {
	case PreviewStateStarting:
		if m.preview.login == "" {
			return m.t("preview.starting")
		}
		return fmt.Sprintf(m.t("preview.starting_channel"), m.previewLabel())
	case PreviewStatePlaying:
		return fmt.Sprintf(m.t("preview.playing"), m.previewLabel())
	case PreviewStateBreak:
		sec := int(m.preview.breakElapsed.Seconds())
		if sec < 0 {
			sec = 0
		}
		return fmt.Sprintf(m.t("preview.break"), sec)
	case PreviewStateOffline:
		return m.t("preview.offline")
	case PreviewStateFailed:
		return m.previewFailureText()
	default:
		return ""
	}
}

func (m Model) previewFailureText() string {
	switch m.preview.failure {
	case PlayerFailureOffline:
		return m.t("preview.offline")
	case PlayerFailureNetwork:
		return m.t("preview.fail.network")
	case PlayerFailureTimeout:
		return m.t("preview.fail.timeout")
	case PlayerFailureStreamlinkMissing:
		return m.t("preview.fail.streamlink_missing")
	case PlayerFailureMPVMissing:
		return m.t("preview.fail.mpv_missing")
	case PlayerFailureNoPlugin:
		return m.t("preview.fail.no_plugin")
	case PlayerFailureBlocked:
		return m.t("preview.fail.blocked")
	case PlayerFailureBrowserMissing:
		return m.t("preview.fail.browser_missing")
	case PlayerFailureVOD:
		return m.t("preview.fail.vod")
	case PlayerFailureBotCheck:
		return m.t("preview.fail.bot_check")
	case PlayerFailureLoginRequired:
		return m.t("preview.fail.login_required")
	default:
		return m.t("preview.fail.unknown")
	}
}

// stopPreviewCmd asks the bridge to stop the preview, if any.
func (m Model) stopPreviewCmd() tea.Cmd {
	if m.playback == nil || !m.previewActive() {
		return nil
	}
	return m.playback.StopPreview()
}

// closePreview clears preview UI state, stops the process, and restores the
// main mute saved when the preview opened.
func (m *Model) closePreview() tea.Cmd {
	var cmds []tea.Cmd
	if stop := m.stopPreviewCmd(); stop != nil {
		cmds = append(cmds, stop)
	}
	if m.playback != nil {
		if restore := m.playback.RestoreMainMute(); restore != nil {
			cmds = append(cmds, restore)
		}
	}
	m.preview = previewState{}
	return tea.Batch(cmds...)
}

// highlightedPlayTarget is the channel under the highlight, if the row is a
// playable channel (live, favorite, recent).
func (m Model) highlightedPlayTarget() (PlayTarget, bool) {
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil {
		return PlayTarget{}, false
	}
	switch it.Type {
	case ItemTypeChannel, ItemTypeFavorite, ItemTypeRecent, ItemTypeExactChannel, ItemTypeYouTubeOffer:
		ch := *it.Channel
		return PlayTarget{Channel: ch}, true
	default:
		return PlayTarget{}, false
	}
}

// startPreviewFor opens a preview of target without recording a recent or
// connecting chat. When a main video is active it mutes that session first
// (ticket 02 uses this with Space while video plays; ticket 01 has no video).
func (m Model) startPreviewFor(target PlayTarget) (tea.Model, tea.Cmd) {
	if m.playback == nil {
		return m, nil
	}
	label := target.Channel.PlayerLabel()
	if label == "" {
		label = target.Channel.Login
	}
	m.preview.target = target
	m.preview.login = label
	m.preview.state = PreviewStateStarting
	m.preview.failure = PlayerFailureNone
	m.preview.lastLine = ""
	m.preview.breakElapsed = 0
	m.preview.selection++
	m.preview.awaiting = m.preview.selection
	m.preview.switchGen++ // cancel any pending switch
	var cmds []tea.Cmd
	if m.mainVideoActive() {
		if mute := m.playback.MuteMainForPreview(); mute != nil {
			cmds = append(cmds, mute)
		}
	}
	cmds = append(cmds, m.playback.StartPreview(target, m.preview.selection))
	return m, tea.Batch(cmds...)
}

// togglePreview is Space on the list with no main video: open or close.
func (m Model) togglePreview() (tea.Model, tea.Cmd) {
	if m.previewOpen() {
		cmd := m.closePreview()
		return m, cmd
	}
	target, ok := m.highlightedPlayTarget()
	if !ok {
		// Ticket 02 owns the "preview is only for channels" notice.
		return m, nil
	}
	return m.startPreviewFor(target)
}

// schedulePreviewSwitch arms the debounce after the highlight moved with a
// preview open. It returns the model so the bumped switchGen is kept.
func (m Model) schedulePreviewSwitch() (Model, tea.Cmd) {
	if !m.previewOpen() {
		return m, nil
	}
	m.preview.switchGen++
	gen := m.preview.switchGen
	return m, m.after(previewSwitchDelay, func(time.Time) tea.Msg {
		return previewSwitchMsg{gen: gen}
	})
}

// applyPreviewSwitch runs after the debounce: switch to the new channel or
// close when the row is not a channel.
func (m Model) applyPreviewSwitch(msg previewSwitchMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.preview.switchGen || !m.previewOpen() {
		return m, nil
	}
	target, ok := m.highlightedPlayTarget()
	if !ok {
		return m, m.closePreview()
	}
	// Same channel: keep the current preview.
	if samePreviewTarget(m.preview.target, target) {
		return m, nil
	}
	var cmds []tea.Cmd
	if stop := m.stopPreviewCmd(); stop != nil {
		cmds = append(cmds, stop)
	}
	next, start := m.startPreviewFor(target)
	m = next.(Model)
	if start != nil {
		cmds = append(cmds, start)
	}
	return m, tea.Batch(cmds...)
}

func samePreviewTarget(a, b PlayTarget) bool {
	if a.Channel.Platform != b.Channel.Platform {
		return false
	}
	if la, lb := a.Channel.PlayerLabel(), b.Channel.PlayerLabel(); la != "" && lb != "" {
		return samePlayerLabel(a.Channel.Platform, la, lb)
	}
	return a.Channel.Login != "" && strings.EqualFold(a.Channel.Login, b.Channel.Login)
}

// applyPreviewStatus folds one preview event into the model.
func (m Model) applyPreviewStatus(msg PreviewStatusMsg) (tea.Model, tea.Cmd) {
	if !m.acceptPreviewStatus(msg) {
		return m, nil
	}
	m.setPreviewState(msg)
	return m, nil
}

func (m *Model) acceptPreviewStatus(msg PreviewStatusMsg) bool {
	if m.preview.awaiting != 0 || msg.Selection != 0 {
		if msg.Selection != m.preview.awaiting {
			return false
		}
		m.preview.awaiting = 0
		m.preview.gen = msg.Generation
	} else if msg.Generation != 0 && m.preview.gen != 0 && msg.Generation < m.preview.gen {
		return false
	} else if msg.Generation > m.preview.gen {
		m.preview.gen = msg.Generation
	}
	return true
}

func (m *Model) setPreviewState(msg PreviewStatusMsg) {
	state := msg.State
	m.preview.state = state
	if msg.Login != "" {
		m.preview.login = msg.Login
	}
	m.preview.breakElapsed = msg.BreakElapsed
	if state == PreviewStateFailed || state == PreviewStateOffline {
		m.preview.failure = msg.Failure
		m.preview.lastLine = msg.LastLine
	} else {
		m.preview.failure = PlayerFailureNone
		m.preview.lastLine = ""
	}
	if state == PreviewStateClosed || state == PreviewStateEnded {
		m.preview = previewState{}
	}
}

// mainVideoActive reports a main player that is starting or playing (ticket
// 01 keeps Space as pause in that case; ticket 02 always uses Space for preview).
func (m Model) mainVideoActive() bool {
	return m.player.controllable()
}
