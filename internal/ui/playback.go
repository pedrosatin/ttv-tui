package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// PlayerState is the UI-facing playback status shown in the status bar.
// Values mirror internal/player states without importing that package.
type PlayerState string

const (
	PlayerStateNone     PlayerState = ""
	PlayerStateStarting PlayerState = "iniciando"
	PlayerStatePlaying  PlayerState = "tocando"
	PlayerStatePaused   PlayerState = "pausado"
	PlayerStateBreak    PlayerState = "intervalo"
	PlayerStateError    PlayerState = "erro"
	PlayerStateEnded    PlayerState = "encerrado"
)

// PlayerFailure classifies a player error. Values mirror internal/player
// failure classes without importing that package.
type PlayerFailure string

const (
	PlayerFailureNone              PlayerFailure = ""
	PlayerFailureOffline           PlayerFailure = "offline"
	PlayerFailureNetwork           PlayerFailure = "network"
	PlayerFailureTimeout           PlayerFailure = "timeout"
	PlayerFailureStreamlinkMissing PlayerFailure = "streamlink_missing"
	PlayerFailureMPVMissing        PlayerFailure = "mpv_missing"
	PlayerFailureNoPlugin          PlayerFailure = "no_plugin"
	PlayerFailureBlocked           PlayerFailure = "blocked"
	PlayerFailureUnknown           PlayerFailure = "unknown"
)

// PlayTarget describes what to play: a catalog.Channel carrying identity, platform, and optional stream URL.
type PlayTarget struct {
	Channel catalog.Channel
}

// PlayerStatusMsg is delivered by the injected Playback bridge whenever the
// supervised player changes state. Update discards messages whose Generation
// is older than the active playback generation.
type PlayerStatusMsg struct {
	Generation   uint64
	State        PlayerState
	Login        string
	LastLine     string
	BreakElapsed time.Duration
	Volume       int
	// Failure classifies a PlayerStateError message; LastLine keeps the
	// original line as technical detail.
	Failure PlayerFailure
	// Selection is set only on the reply of Playback.Start: the selection it
	// was asked for. During a switch the UI takes only the reply of the latest
	// one, and ignores pump events from the previous channel.
	Selection uint64
	// InputConfFailed marks a playback whose mpv window ignores keys because its
	// key file could not be written. The TUI controls keep working.
	InputConfFailed bool
	// ContainmentDegraded means Windows refused Job Object containment.
	ContainmentDegraded bool
	// DownloadFailed marks an error that occurred while attempting to download a managed component.
	DownloadFailed bool
}

// ComponentProgressMsg reports progress of an automatic background download
// of a managed dependency (such as streamlink).
type ComponentProgressMsg struct {
	Component string
	Percent   int
	Read      int64
	Total     int64
	Done      bool
	Waiting   bool
}

// Playback is the UI→app bridge for starting and stopping video. The app
// injects an implementation that talks to internal/player; the UI never
// imports that package. Ongoing status updates arrive as PlayerStatusMsg
// (forwarded by the app from the player event pump).
type Playback interface {
	// Start begins playback for login (stopping any previous session) and
	// returns a Cmd that yields an initial PlayerStatusMsg carrying selection.
	// selection grows with each call; a Start that reaches the player after a
	// newer one is superseded.
	Start(login string, selection uint64) tea.Cmd
	// StartTarget begins playback for target (with URL and qualified label).
	StartTarget(target PlayTarget, selection uint64) tea.Cmd
	// Stop ends the active playback and returns a Cmd that yields a terminal status.
	Stop() tea.Cmd
	// TogglePause flips pause on the active playback. Status arrives via PlayerStatusMsg.
	TogglePause() tea.Cmd
	// AdjustVolume changes volume by delta (clamped by the player). Status arrives via PlayerStatusMsg.
	AdjustVolume(delta int) tea.Cmd
	// Listen is optional; implementations that push events via the tea program
	// may return nil.
	Listen() tea.Cmd
}

// WithPlayback attaches a Playback bridge to the Model.
func WithPlayback(p Playback) Option {
	return func(m *Model) {
		m.playback = p
	}
}

// PlayerState returns the current UI-facing player state.
func (m Model) PlayerState() PlayerState {
	return m.player.state
}

// PlayerLogin returns the login of the channel currently associated with the player.
func (m Model) PlayerLogin() string {
	return m.player.login
}

// PlayerGeneration returns the generation of the active playback the UI accepts.
func (m Model) PlayerGeneration() uint64 {
	return m.player.gen
}

// PlayerVolume returns the last known volume (0..100) shown in the status bar.
func (m Model) PlayerVolume() int {
	return m.player.volume
}

// Composing reports whether chat compose mode is active (ticket 09). While
// composing, Space / + / - are treated as text instead of player controls.
func (m Model) Composing() bool {
	return m.chat.composing
}

func (m Model) playerStatusText() string {
	base := m.playerStatusBase()
	if vol := m.playerVolumeText(); vol != "" {
		return base + " · " + vol
	}
	return base
}

// playerStatusBase is the player state without the volume.
func (m Model) playerStatusBase() string {
	switch m.player.state {
	case PlayerStateStarting:
		if m.player.login == "" {
			return m.t("player.starting")
		}
		return fmt.Sprintf(m.t("player.starting_channel"), m.playerLabel())
	case PlayerStatePlaying:
		return fmt.Sprintf(m.t("player.playing"), m.playerLabel())
	case PlayerStatePaused:
		return fmt.Sprintf(m.t("player.paused"), m.playerLabel())
	case PlayerStateBreak:
		sec := int(m.player.breakElapsed.Seconds())
		if sec < 0 {
			sec = 0
		}
		return fmt.Sprintf(m.t("player.break"), sec)
	case PlayerStateError:
		return m.playerFailureText()
	case PlayerStateEnded:
		return m.t("player.ended")
	default:
		return ""
	}
}

// playerLabel names what the player shows, as a person reads it. Outside Twitch
// the session label is qualified ("kick:gaules") so two platforms never collide;
// the bar shows the channel and its platform instead ("gaules (Kick)").
func (m Model) playerLabel() string {
	label := m.player.login
	ch := m.player.target.Channel
	if label == "" || platform.IsTwitch(ch.Platform) || !strings.EqualFold(ch.PlayerLabel(), label) {
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

// playerVolumeText is the volume shown next to an active player state, or ""
// when the player has no volume to report.
func (m Model) playerVolumeText() string {
	switch m.player.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting:
		return fmt.Sprintf(m.t("player.vol"), m.player.volume)
	default:
		return ""
	}
}

// windowNoticeShown reports whether the one-time "video opened in a
// separate window" notice should lead the status bar now.
func (p playbackState) windowNoticeShown() bool {
	if !p.windowNotice {
		return false
	}
	switch p.state {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return true
	default:
		return false
	}
}

// PlayerWindowNotice reports whether the one-time separate-window notice is
// currently shown.
func (m Model) PlayerWindowNotice() bool {
	return m.player.windowNoticeShown()
}

// PlayerFailure returns the classified cause of the current player error.
func (m Model) PlayerFailure() PlayerFailure {
	return m.player.failure
}

// playerFailureText is the main message for a player error: what happened and
// the next step, never the raw streamlink line.
func (m Model) playerFailureText() string {
	switch m.player.failure {
	case PlayerFailureOffline:
		return m.t("player.fail.offline")
	case PlayerFailureNetwork:
		return m.t("player.fail.network")
	case PlayerFailureTimeout:
		return m.t("player.fail.timeout")
	case PlayerFailureStreamlinkMissing:
		return m.t("player.fail.streamlink_missing")
	case PlayerFailureMPVMissing:
		return m.t("player.fail.mpv_missing")
	case PlayerFailureNoPlugin:
		return m.t("player.fail.no_plugin")
	case PlayerFailureBlocked:
		return m.t("player.fail.blocked")
	default:
		return m.t("player.fail.unknown")
	}
}

// errorLine is the original streamlink/mpv line of the last player
// error, or "" when there is none. Kept after the status bar dismisses the
// error so help (?) can still show the technical detail.
func (p playbackState) errorLine() string {
	line := strings.TrimSpace(sanitizeChatText(p.lastLine))
	if strings.Contains(strings.ToLower(line), "[info]") {
		return ""
	}
	return line
}

// playerErrorDetail is the technical detail shown after the main message in
// the status bar, or "" when there is none.
func (m Model) playerErrorDetail() string {
	line := m.player.errorLine()
	if line == "" {
		return ""
	}
	return fmt.Sprintf(m.t("player.fail.detail"), line)
}

// retryPlayback restarts playback of the player's channel with the volume it had:
// the player keeps that one.
func (m Model) retryPlayback() (tea.Model, tea.Cmd) {
	target := m.player.target
	if target.Channel.URL == "" && target.Channel.Login == "" && target.Channel.ID == "" {
		target = PlayTarget{Channel: catalog.Channel{Platform: platform.Twitch, Login: m.player.login}}
	}
	m.player.state = PlayerStateStarting
	m.player.failure = PlayerFailureNone
	m.player.lastLine = ""
	m.player.breakElapsed = 0
	m.player.selection++
	m.player.awaiting = m.player.selection
	m.player.stopAfterStart = false
	return m, m.playback.StartTarget(target, m.player.selection)
}

func clampPlayerVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// manualInstallHint returns platform-specific instructions for installing streamlink manually.
func (m Model) manualInstallHint() string {
	return m.manualInstallHintFor(runtime.GOOS)
}

func (m Model) manualInstallHintFor(goos string) string {
	switch goos {
	case "windows":
		return m.t("components.manual_install.windows")
	case "darwin":
		return m.t("components.manual_install.darwin")
	default:
		return m.t("components.manual_install.linux")
	}
}

// manualInstallMPVHint returns platform-specific instructions for installing mpv manually.
func (m Model) manualInstallMPVHint() string {
	return m.manualInstallMPVHintFor(runtime.GOOS)
}

func (m Model) manualInstallMPVHintFor(goos string) string {
	switch goos {
	case "windows":
		return m.t("components.manual_install_mpv.windows")
	case "darwin":
		return m.t("components.manual_install_mpv.darwin")
	default:
		return m.t("components.manual_install_mpv.linux")
	}
}
