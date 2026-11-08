package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
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
	// Still means the last frame stays valid while no new one comes: it is drawn
	// again if the rows need it, now or when they come back.
	Still bool
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

// videoStateLines is the persistent line for a mode that was asked for and cannot
// run: which side failed and what to do, wrapped so the way out is never cut. It
// is a state, not a notice, so it does not compete with the status bar.
func (m Model) videoStateLines() []string {
	if !m.videoRequested || m.videoReason == "" {
		return nil
	}
	rows := wrapChatRow("  ! ", m.t(m.videoReason), m.width)
	for i, row := range rows {
		rows[i] = styleWarning(row)
	}
	return rows
}

// applyWindowSize records the terminal size. The state line takes its rows here,
// once, so every height computed from m.height already leaves them out. The bar
// keeps its row even on a terminal too short for the whole line. The video panel
// never coexists with the line: it needs a reason-free start.
func (m *Model) applyWindowSize(width, height int) {
	m.width, m.height = width, height
	m.videoStateRows = min(len(m.videoStateLines()), max(height-1, 0))
	m.height -= m.videoStateRows
}

// videoStateView returns the rows of the state line the frame has room for. An
// unsized model has no budget to respect and shows it whole.
func (m Model) videoStateView() []string {
	lines := m.videoStateLines()
	if m.height > 0 {
		lines = lines[:min(len(lines), m.videoStateRows)]
	}
	return lines
}

// defaultVideoChatRows is the height of the chat strip under the video.
const defaultVideoChatRows = 8

// WithVideoChatRows sets the height of the chat strip in the video mode. Zero or
// less keeps the current height.
func WithVideoChatRows(rows int) Option {
	return func(m *Model) {
		if rows > 0 {
			m.videoChatRows = rows
		}
	}
}

// WithVideoChatOnTop puts the chat strip of the video mode above the panel. The
// default is under it.
func WithVideoChatOnTop(top bool) Option {
	return func(m *Model) { m.videoChatTop = top }
}

// videoChatStripRows is how many of the mode's available rows go to the chat: the
// configured height, never more than there is, and none while the chat is off.
func (m Model) videoChatStripRows(available int) int {
	if m.videoChatOff {
		return 0
	}
	rows := m.videoChatRows
	if rows <= 0 {
		rows = defaultVideoChatRows
	}
	return min(rows, max(available, 0))
}

// videoRows is the panel's height: every row the chat strip leaves. The strip is
// opaque and full width above or under the panel, so the two never share a line.
func (m Model) videoRows(available int) int {
	return max(available-m.videoChatStripRows(available), 0)
}

// videoChatHidden reports whether the screen is the video body with its chat off.
func (m Model) videoChatHidden() bool { return m.videoChatOff && m.videoBodyShown() }

// videoChatKey reports whether msg shows or hides the chat strip. Only the video
// body has one, and an input on screen keeps the letter.
func (m Model) videoChatKey(msg tea.KeyMsg) bool {
	return m.videoBodyShown() && !m.typingText() && key.Matches(msg, m.keys.ToggleVideoChat)
}

// toggleVideoChat shows or hides the strip. The panel follows in syncVideoPanel,
// and the output asks the player for the new frame size: nothing restarts.
func (m *Model) toggleVideoChat() {
	m.videoChatOff = !m.videoChatOff
	m.normalizeFocusAfterLayout()
}

// videoBodyShown reports whether the screen is the panel above the chat. The list
// opened over it swaps that for the ordinary list and chat.
func (m Model) videoBodyShown() bool { return m.showVideo && !m.videoListOpen }

// openVideoList shows the list without leaving the mode. The panel gives its rows
// up for as long as it stays open, so no frame shares a line with the list.
func (m *Model) openVideoList() {
	m.videoListOpen = true
	m.focus = FocusList
}

// closeVideoList goes back to the panel, if the list was open over it.
func (m *Model) closeVideoList() {
	if !m.videoListOpen {
		return
	}
	m.videoListOpen = false
	m.normalizeFocusAfterLayout()
}

func (m *Model) enterVideoLayout() {
	m.showVideo = true
	m.showList, m.showChat = false, true
}

func (m *Model) leaveVideoLayout() {
	m.showVideo, m.videoListOpen = false, false
}

// openInVideoLayout makes the video layout the first screen when it can draw: asked
// for, supported, and with a surface, which only a real terminal output provides.
// The list opens over the panel because nothing plays yet and the mode hides it.
// Any other start stays in the split.
func (m *Model) openInVideoLayout() {
	if !m.videoRequested || m.videoReason != "" || m.videoSurface == nil {
		return
	}
	m.enterVideoLayout()
	m.openVideoList()
	if m.statusNotice == "" {
		m.statusNotice = m.t("video.active")
	}
}

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

// videoListKey reports whether msg opens the list over the panel. An input on
// screen keeps both keys.
func (m Model) videoListKey(msg tea.KeyMsg) bool {
	return m.videoBodyShown() && !m.typingText() && key.Matches(msg, m.keys.Clear, m.keys.ChannelFilter)
}

// typingText reports whether a letter key is text for an input on screen.
func (m Model) typingText() bool {
	return m.channelFilterMenu || (m.composing && m.chatColumnVisible()) ||
		((m.searching || m.filtering) && m.listVisible())
}

// videoPanel is the region as it stands now. An overlay takes the rows over, so
// the panel gives way to it instead of competing for them. Only the list keeps the
// image for its return, even with help or the filter menu opened on top of it.
func (m Model) videoPanel() VideoPanel {
	if m.videoSurface == nil || !m.showVideo || m.quitting || m.width <= 0 || m.height <= 1 {
		return VideoPanel{}
	}
	available := m.height - 1
	rows := m.videoRows(available)
	if rows == 0 {
		return VideoPanel{}
	}
	panel := VideoPanel{Row: 1, Col: 1, Rows: rows, Cols: m.width, Still: m.videoHasImage()}
	if m.videoChatTop {
		panel.Row += available - rows
	}
	if m.videoListOpen {
		return panel
	}
	if m.helpVisible || m.channelFilterMenu {
		return VideoPanel{}
	}
	panel.Visible = true
	panel.Playing = m.playerEmbedded && m.playerState == PlayerStatePlaying
	return panel
}

// videoHasImage reports whether the playback has a picture for the panel: a paused
// or held stream keeps its last frame.
func (m Model) videoHasImage() bool {
	if !m.playerEmbedded {
		return false
	}
	switch m.playerState {
	case PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return true
	default:
		return false
	}
}

// videoImageOnScreen reports whether the panel may be holding an image.
func (m Model) videoImageOnScreen() bool {
	return m.videoHasImage() && m.videoPanel().Visible
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
	} else if before.videoImageOnScreen() && before.videoChatOff != m.videoChatOff {
		// The image moves inside a panel of another height, and the rows it
		// leaves may stay blank, which the renderer would not repaint.
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
	panel := make([]string, rows)
	if rows > 0 && !m.videoImageOnScreen() {
		panel[0] = m.videoPanelText()
	}
	var strip []string
	if n := available - rows; n > 0 {
		strip = m.renderChatLines(n, m.width)
		for len(strip) < n {
			strip = append(strip, "")
		}
		strip = strip[:n]
	}
	first, second := panel, strip
	if m.videoChatTop {
		first, second = strip, panel
	}
	lines := make([]string, 0, available)
	lines = append(lines, first...)
	lines = append(lines, second...)
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
