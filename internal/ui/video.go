package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// VideoPanel is the screen region reserved for video, in 1-based cells. The zero
// value means the panel is off.
type VideoPanel struct {
	Row, Col   int
	Rows, Cols int
	// Visible means the rows belong to the panel. View keeps them blank for as
	// long as Playing holds.
	Visible bool
	// Playing means frames may be drawn right now.
	Playing bool
}

// VideoSurface draws video outside the renderer. SetPanel runs inside Update, so
// it must not block; it is called only when the panel changes.
type VideoSurface interface {
	SetPanel(VideoPanel)
}

// WithVideoSurface attaches what draws the panel. Without one the panel is only
// a placeholder.
func WithVideoSurface(surface VideoSurface) Option {
	return func(m *Model) { m.videoSurface = surface }
}

// WithVideo enables the experimental layout only when both probes succeeded.
// reason is a localized catalog key; empty means supported.
func WithVideo(requested bool, reason string) Option {
	return func(m *Model) { m.videoRequested, m.videoReason = requested, reason }
}

func (m Model) videoRows(available int) int { return min(18, max(available-6, 0)) }

// embeddedLayout reports whether playback started now belongs in the panel. The
// flag only makes the layout reachable: every other layout keeps the mpv window.
func (m Model) embeddedLayout() bool {
	return m.showVideo && m.videoSurface != nil
}

// videoRestartOffered reports whether r restarts the player: its output no longer
// matches the layout, or it failed under the panel, where the hidden list would
// otherwise keep r from retrying. A list error on screen owns r.
func (m Model) videoRestartOffered() bool {
	if m.playback == nil || m.videoSurface == nil || m.playerLogin == "" || (m.err != nil && m.listVisible()) {
		return false
	}
	if m.playerState == PlayerStateError {
		return m.showVideo
	}
	return m.playerControllable() && m.playerEmbedded != m.embeddedLayout()
}

// videoAudioOnly reports a playback whose video went with the panel: it has no
// window to fall back on, and only sound reaches the user.
func (m Model) videoAudioOnly() bool {
	return m.playerEmbedded && !m.showVideo && m.playerControllable()
}

// typingText reports whether a letter key is text for an input on screen.
func (m Model) typingText() bool {
	return m.channelFilterMenu || (m.composing && m.chatColumnVisible()) ||
		((m.searching || m.filtering) && m.listVisible())
}

// videoPanel is the region as it stands now. An overlay takes the rows over, so
// the panel gives way to it instead of competing for them.
func (m Model) videoPanel() VideoPanel {
	if m.videoSurface == nil || !m.showVideo || m.quitting || m.width <= 0 || m.height <= 1 ||
		m.helpVisible || m.channelFilterMenu {
		return VideoPanel{}
	}
	rows := m.videoRows(m.height - 1)
	if rows == 0 {
		return VideoPanel{}
	}
	return VideoPanel{
		Row:     1,
		Col:     1,
		Rows:    rows,
		Cols:    m.width,
		Visible: true,
		Playing: m.playerEmbedded && m.playerState == PlayerStatePlaying,
	}
}

// videoImageOnScreen reports whether the panel may be holding an image: a paused
// or held stream keeps its last frame on screen.
func (m Model) videoImageOnScreen() bool {
	if !m.playerEmbedded || !m.videoPanel().Visible {
		return false
	}
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return true
	default:
		return false
	}
}

// syncVideoPanel publishes a changed panel. The renderer skips lines that did not
// change, so blank rows would never overwrite an image that has to go: the screen
// is cleared for that, and the frames stopped first so none can follow the clear.
func (m Model) syncVideoPanel(before Model, cmd tea.Cmd) (Model, tea.Cmd) {
	if m.videoSurface == nil {
		return m, cmd
	}
	if panel := m.videoPanel(); panel != m.videoPublished {
		m.videoSurface.SetPanel(panel)
		m.videoPublished = panel
	}
	if before.videoImageOnScreen() && !m.videoImageOnScreen() {
		cmd = tea.Batch(cmd, tea.ClearScreen)
	}
	return m, cmd
}

// videoPanelText is what the panel says while it has no image.
func (m Model) videoPanelText() string {
	if m.playerControllable() && m.videoRestartOffered() {
		return m.t("video.restart_embedded")
	}
	return m.t("video.placeholder")
}

func (m Model) videoView(available int, overlay []string, status string) string {
	rows := m.videoRows(available)
	lines := make([]string, rows)
	if rows > 0 && !m.videoImageOnScreen() {
		lines[0] = m.videoPanelText()
	}
	lines = append(lines, m.renderChatLines(available-rows, m.width)...)
	for len(lines) < available {
		lines = append(lines, "")
	}
	lines = lines[:available]
	for i := range lines {
		lines[i] = padOrTruncateVisual(lines[i], m.width)
	}
	lines = m.overlayHelpLines(lines, overlay, m.width, available)
	return strings.Join(lines, "\n") + "\n" + status
}
