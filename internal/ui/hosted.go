package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Column limits of the hosted layout. The minimums add up to minSplitColumns with
// the two separators, so the three columns fit wherever the split does.
const (
	hostedMinList  = 22
	hostedMaxList  = 44
	hostedMinChat  = 24
	hostedMaxChat  = 50
	hostedMinVideo = 32
	hostedSepWidth = 1

	// hostedResizeStep is how many columns one press moves a divider.
	hostedResizeStep = 2
	hostedSaveDelay  = 400 * time.Millisecond
)

// HostPanel is the video's region in the hosted window, in 1-based cells. The last
// OverlayCols columns of it carry the chat, drawn translucent over the picture.
type HostPanel struct {
	Row, Col, Rows, Cols int
	Visible              bool
	OverlayCols          int
}

// VideoHost is the window that draws the video. SetPanel runs inside Update, so it
// must not block.
type VideoHost interface {
	SetPanel(HostPanel)
}

// HostedColumnsPersist saves the panel widths (0 means automatic) and the chat placement.
type HostedColumnsPersist func(list, chat int, overlay bool) error

// WithHostedColumns sets the saved layout and what saves it when it changes.
func WithHostedColumns(list, chat int, overlay bool, persist HostedColumnsPersist) Option {
	return func(m *Model) {
		m.hostedListCols, m.hostedChatCols, m.chatOverlay, m.hostedPersist = list, chat, overlay, persist
	}
}

// WithVideoHost attaches the window that draws the video itself. The TUI then keeps
// the list on the left and the chat on the right, and tells the host which cells
// between them are the video's.
func WithVideoHost(host VideoHost) Option {
	return func(m *Model) { m.videoHost = host }
}

// hostedDefaultList and hostedDefaultChat are the widths nobody asked to change.
func (m Model) hostedDefaultList() int {
	return min(max(m.width/5, hostedMinList), hostedMaxList)
}

func (m Model) hostedDefaultChat() int {
	return min(max(m.width*22/100, hostedMinChat), hostedMaxChat)
}

// hostedWidths returns the list, chat and video columns. Zero means hidden; the
// video takes whatever the panels leave. A window too small for the widths asked
// for takes columns from the wider panel first, so the video keeps its minimum
// for as long as the panels can give way.
func (m Model) hostedWidths() (listW, chatW, videoW int) {
	if m.width <= 0 {
		return 0, 0, 0
	}
	separators := 0
	if m.listVisible() {
		listW = m.hostedDefaultList()
		if m.hostedListCols > 0 {
			listW = max(m.hostedListCols, hostedMinList)
		}
		separators += hostedSepWidth
	}
	if m.chatColumnVisible() {
		chatW = m.hostedDefaultChat()
		if m.hostedChatCols > 0 {
			chatW = max(m.hostedChatCols, hostedMinChat)
		}
		separators += hostedSepWidth
	}
	if m.chatOverlay && chatW > 0 {
		return m.hostedOverlayWidths(listW, chatW)
	}
	for over := listW + chatW + separators + hostedMinVideo - m.width; over > 0; over-- {
		switch {
		case listW > hostedMinList && listW >= chatW:
			listW--
		case chatW > hostedMinChat:
			chatW--
		case listW > hostedMinList:
			listW--
		default:
			return listW, chatW, max(m.width-listW-chatW-separators, 0)
		}
	}
	return listW, chatW, max(m.width-listW-chatW-separators, 0)
}

// hostedOverlayWidths is the layout with the chat over the video: only the list
// takes columns from the picture, and the chat always leaves part of it uncovered.
func (m Model) hostedOverlayWidths(listW, chatW int) (int, int, int) {
	separators := 0
	if listW > 0 {
		separators = hostedSepWidth
	}
	videoW := func() int { return max(m.width-listW-separators, 0) }
	for videoW()-chatW < hostedMinVideo {
		switch {
		case listW > hostedMinList && listW >= chatW:
			listW--
		case chatW > hostedMinChat:
			chatW--
		case listW > hostedMinList:
			listW--
		default:
			return listW, min(chatW, videoW()), videoW()
		}
	}
	return listW, chatW, videoW()
}

// toggleChatOverlay moves the chat between the side column and over the video.
func (m *Model) toggleChatOverlay() tea.Cmd {
	if !m.chatColumnVisible() {
		m.warnChatUnavailable()
		return nil
	}
	m.chatOverlay = !m.chatOverlay
	notice := "layout.chat_side"
	if m.chatOverlay {
		notice = "layout.chat_overlay"
	}
	m.statusNotice, m.statusNoticeWarns = m.t(notice), false
	return m.scheduleHostedSave()
}

// warnChatUnavailable says why a chat key cannot act.
func (m *Model) warnChatUnavailable() {
	if m.warnChatHiddenByWidth() {
		return
	}
	if m.hasChatPanel() {
		m.warnStatus(m.t("layout.hidden_chat_key"))
	} else {
		m.warnStatus(m.t("layout.no_chat"))
	}
}

// handleHostedResizeKey moves a divider. It reports whether msg was one of its keys.
func (m *Model) handleHostedResizeKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.videoHost == nil {
		return false, nil
	}
	switch {
	case key.Matches(msg, m.keys.ToggleChatOverlay):
		return true, m.toggleChatOverlay()
	case key.Matches(msg, m.keys.ListWider):
		return true, m.resizeHostedList(hostedResizeStep)
	case key.Matches(msg, m.keys.ListNarrower):
		return true, m.resizeHostedList(-hostedResizeStep)
	case key.Matches(msg, m.keys.ChatWider):
		return true, m.resizeHostedChat(hostedResizeStep)
	case key.Matches(msg, m.keys.ChatNarrower):
		return true, m.resizeHostedChat(-hostedResizeStep)
	}
	return false, nil
}

// hostedRoomToGrow is what a panel can still take: the picture outside the panels
// keeps its minimum.
func (m Model) hostedRoomToGrow(chatW, videoW int) int {
	if m.chatOverlay {
		videoW -= chatW
	}
	return max(videoW-hostedMinVideo, 0)
}

func (m *Model) resizeHostedList(delta int) tea.Cmd {
	listW, chatW, videoW := m.hostedWidths()
	if listW == 0 {
		m.warnStatus(m.t("layout.hidden_list_key"))
		return nil
	}
	listW = max(min(listW+delta, listW+m.hostedRoomToGrow(chatW, videoW)), hostedMinList)
	m.hostedListCols = listW
	m.statusNotice, m.statusNoticeWarns = fmt.Sprintf(m.t("layout.list_columns"), listW), false
	return m.scheduleHostedSave()
}

func (m *Model) resizeHostedChat(delta int) tea.Cmd {
	_, chatW, videoW := m.hostedWidths()
	if chatW == 0 {
		m.warnChatUnavailable()
		return nil
	}
	chatW = max(min(chatW+delta, chatW+m.hostedRoomToGrow(chatW, videoW)), hostedMinChat)
	m.hostedChatCols = chatW
	m.statusNotice, m.statusNoticeWarns = fmt.Sprintf(m.t("layout.chat_columns"), chatW), false
	return m.scheduleHostedSave()
}

// hostedSaveMsg asks to save the widths if no newer change came after it.
type hostedSaveMsg struct{ gen int }

// scheduleHostedSave waits for the key to stop: a held arrow repeats about thirty
// times a second, and only the last width is worth writing.
func (m *Model) scheduleHostedSave() tea.Cmd {
	if m.hostedPersist == nil {
		return nil
	}
	m.hostedSaveGen++
	gen := m.hostedSaveGen
	return tea.Tick(hostedSaveDelay, func(time.Time) tea.Msg { return hostedSaveMsg{gen: gen} })
}

// saveHostedColumns writes the widths, off the update loop, if gen is still the newest.
func (m Model) saveHostedColumns(gen int) tea.Cmd {
	if m.hostedPersist == nil || gen != m.hostedSaveGen {
		return nil
	}
	persist, list, chat, overlay := m.hostedPersist, m.hostedListCols, m.hostedChatCols, m.chatOverlay
	return func() tea.Msg {
		_ = persist(list, chat, overlay)
		return nil
	}
}

// hostedPanel is the video's region, in 1-based cells. An overlay takes the screen
// over, so the zero value tells the host to draw the TUI's cells there instead.
func (m Model) hostedPanel() HostPanel {
	if m.videoHost == nil || m.quitting || m.width <= 0 || m.height <= 1 || m.helpVisible || m.channelFilterMenu {
		return HostPanel{}
	}
	listW, chatW, videoW := m.hostedWidths()
	if videoW == 0 {
		return HostPanel{}
	}
	col := 1
	if listW > 0 {
		col += listW + hostedSepWidth
	}
	panel := HostPanel{Row: 1, Col: col, Rows: m.height - 1, Cols: videoW, Visible: true}
	if m.chatOverlay {
		panel.OverlayCols = chatW
	}
	return panel
}

// hostedIdleText is what the video column says while no playback owns it.
func (m Model) hostedIdleText() string {
	switch m.playerState {
	case PlayerStateStarting, PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return ""
	default:
		return m.t("video.active")
	}
}

// hostedView draws the three columns. The video column stays blank: the host paints
// over those cells while a playback is on.
func (m Model) hostedView(available int, overlay []string, status string) string {
	listW, chatW, videoW := m.hostedWidths()
	var list, chat []string
	if listW > 0 {
		list = m.renderBodyLines(listW)
		if m.shouldShowNarrowChatNotice() {
			list = append(list, m.narrowChatNoticeLines(listW)...)
		}
	}
	if chatW > 0 {
		chat = m.renderChatLines(available, chatW)
	}
	// With the chat over the video, its cells come out of the video column.
	overlaid := m.chatOverlay && chatW > 0
	openW := videoW
	if overlaid {
		openW = max(videoW-chatW, 0)
	}
	idle := TruncateVisual(m.hostedIdleText(), openW)
	if openW == 0 {
		idle = ""
	}
	idle = strings.Repeat(" ", max(openW-ansi.StringWidth(idle), 0)/2) + idle

	separator := strings.TrimSpace(panelSeparator)
	cell := func(rows []string, i, width int) string {
		if i < len(rows) {
			return padOrTruncateVisual(rows[i], width)
		}
		return strings.Repeat(" ", width)
	}
	lines := make([]string, available)
	for i := range lines {
		var row strings.Builder
		if listW > 0 {
			row.WriteString(cell(list, i, listW))
			row.WriteString(separator)
		}
		switch {
		case i == available/2 && openW > 0:
			row.WriteString(padOrTruncateVisual(idle, openW))
		default:
			row.WriteString(strings.Repeat(" ", openW))
		}
		if chatW > 0 {
			if !overlaid {
				row.WriteString(separator)
			}
			row.WriteString(cell(chat, i, chatW))
		}
		lines[i] = row.String()
	}
	lines = m.overlayHelpLines(lines, overlay, m.width, available)
	return strings.Join(lines, "\n") + "\n" + status
}
