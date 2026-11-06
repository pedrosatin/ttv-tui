package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	PlayerFailureUnknown           PlayerFailure = "unknown"
)

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
	// FromStart marks the handshake message returned by Playback.Start so the
	// UI can ignore in-flight pump events from the previous channel.
	FromStart bool
	// InputConfFailed marks a playback whose mpv window ignores keys because its
	// key file could not be written. The TUI controls keep working.
	InputConfFailed bool
}

// Playback is the UI→app bridge for starting and stopping video. The app
// injects an implementation that talks to internal/player; the UI never
// imports that package. Ongoing status updates arrive as PlayerStatusMsg
// (forwarded by the app from the player event pump).
type Playback interface {
	// Start begins playback for login (stopping any previous session) and
	// returns a Cmd that yields an initial PlayerStatusMsg.
	Start(login string) tea.Cmd
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
	return m.playerState
}

// PlayerLogin returns the login of the channel currently associated with the player.
func (m Model) PlayerLogin() string {
	return m.playerLogin
}

// PlayerGeneration returns the generation of the active playback the UI accepts.
func (m Model) PlayerGeneration() uint64 {
	return m.playerGen
}

// PlayerVolume returns the last known volume (0..100) shown in the status bar.
func (m Model) PlayerVolume() int {
	return m.playerVolume
}

// Composing reports whether chat compose mode is active (ticket 09). While
// composing, Space / + / - are treated as text instead of player controls.
func (m Model) Composing() bool {
	return m.composing
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
	switch m.playerState {
	case PlayerStateStarting:
		if m.playerLogin == "" {
			return m.t("player.starting")
		}
		return fmt.Sprintf(m.t("player.starting_channel"), m.playerLogin)
	case PlayerStatePlaying:
		return fmt.Sprintf(m.t("player.playing"), m.playerLogin)
	case PlayerStatePaused:
		return fmt.Sprintf(m.t("player.paused"), m.playerLogin)
	case PlayerStateBreak:
		sec := int(m.playerBreakElapsed.Seconds())
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

// playerVolumeText is the volume shown next to an active player state, or ""
// when the player has no volume to report.
func (m Model) playerVolumeText() string {
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting:
		return fmt.Sprintf(m.t("player.vol"), m.playerVolume)
	default:
		return ""
	}
}

// playerWindowNoticeShown reports whether the one-time "video opened in a
// separate window" notice should lead the status bar now.
func (m Model) playerWindowNoticeShown() bool {
	if !m.playerWindowNotice {
		return false
	}
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return true
	default:
		return false
	}
}

// PlayerWindowNotice reports whether the one-time separate-window notice is
// currently shown.
func (m Model) PlayerWindowNotice() bool {
	return m.playerWindowNoticeShown()
}

// PlayerFailure returns the classified cause of the current player error.
func (m Model) PlayerFailure() PlayerFailure {
	return m.playerFailure
}

// playerFailureText is the main message for a player error: what happened and
// the next step, never the raw streamlink line.
func (m Model) playerFailureText() string {
	switch m.playerFailure {
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
	default:
		return m.t("player.fail.unknown")
	}
}

// playerErrorLine is the original streamlink/mpv line of the last player
// error, or "" when there is none. Kept after the status bar dismisses the
// error so help (?) can still show the technical detail.
func (m Model) playerErrorLine() string {
	line := strings.TrimSpace(sanitizeChatText(m.playerLastLine))
	if strings.Contains(strings.ToLower(line), "[info]") {
		return ""
	}
	return line
}

// playerErrorDetail is the technical detail shown after the main message in
// the status bar, or "" when there is none.
func (m Model) playerErrorDetail() string {
	line := m.playerErrorLine()
	if line == "" {
		return ""
	}
	return fmt.Sprintf(m.t("player.fail.detail"), line)
}

// retryPlayback restarts playback of the channel whose player failed.
func (m Model) retryPlayback() (tea.Model, tea.Cmd) {
	login := m.playerLogin
	m.playerState = PlayerStateStarting
	m.playerFailure = PlayerFailureNone
	m.playerLastLine = ""
	m.playerBreakElapsed = 0
	m.playerVolume = 100
	m.playerSwitching = true
	return m, m.playback.Start(login)
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
