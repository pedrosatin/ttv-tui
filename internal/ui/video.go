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
		Playing: m.playerState == PlayerStatePlaying,
	}
}

// videoImageOnScreen reports whether the panel may be holding an image: a paused
// or held stream keeps its last frame on screen.
func (m Model) videoImageOnScreen() bool {
	if !m.videoPanel().Visible {
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

func (m Model) videoView(available int, overlay []string, status string) string {
	rows := m.videoRows(available)
	lines := make([]string, rows)
	if rows > 0 && !m.videoImageOnScreen() {
		lines[0] = m.t("video.placeholder")
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
