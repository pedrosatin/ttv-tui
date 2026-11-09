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

	// hostedHelpPadding is the room around the help inside the video column.
	hostedHelpPadding = 2

	hostedDefaultOpacity = 70
	hostedMinOpacity     = 10
	hostedOpacityStep    = 5
	hostedSaveDelay      = 400 * time.Millisecond
	// hostedNoticeDelay is how long a layout notice keeps the playback state off the bar.
	hostedNoticeDelay = 2500 * time.Millisecond
)

// HostPanel is the video's region in the hosted window, in 1-based cells. The last
// OverlayCols columns of it carry the chat, drawn translucent over the picture.
type HostPanel struct {
	Row, Col, Rows, Cols int
	Visible              bool
	OverlayCols          int
	// OverlayLeft puts the chat at the start of the region instead of the end.
	OverlayLeft bool
	// OverlayOpacity is the percent opacity of the background behind the overlaid chat.
	OverlayOpacity int
}

// VideoHost is the window that draws the video. SetPanel runs inside Update, so it
// must not block.
type VideoHost interface {
	SetPanel(HostPanel)
}

// HostedColumnsPersist saves the panel widths (0 means automatic), the chat placement
// and which side each panel is on.
type HostedColumnsPersist func(list, chat int, overlay, swapped bool, opacity int) error

// WithHostedColumns sets the saved layout and what saves it when it changes.
func WithHostedColumns(list, chat int, overlay, swapped bool, opacity int, persist HostedColumnsPersist) Option {
	return func(m *Model) {
		m.hostedListCols, m.hostedChatCols, m.chatOverlay, m.panelsSwapped, m.hostedPersist = list, chat, overlay, swapped, persist
		m.chatOverlayOpacity = opacity
	}
}

// WithVideoHost attaches the window that draws the video itself. The TUI then keeps
// the list on the left and the chat on the right, and tells the host which cells
// between them are the video's.
func WithVideoHost(host VideoHost) Option {
	return func(m *Model) {
		m.videoHost = host
		exactCellWidths = true
	}
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
	return m.announceHosted(m.t(notice))
}

// chatOpacity is the percent opacity of the overlaid chat's background.
func (m Model) chatOpacity() int {
	if m.chatOverlayOpacity > 0 {
		return m.chatOverlayOpacity
	}
	return hostedDefaultOpacity
}

// adjustChatOpacity changes the background behind the overlaid chat.
func (m *Model) adjustChatOpacity(delta int) tea.Cmd {
	if !m.chatOverlay || !m.chatColumnVisible() {
		m.warnStatus(m.t("layout.opacity_needs_overlay"))
		return nil
	}
	m.chatOverlayOpacity = min(max(m.chatOpacity()+delta, hostedMinOpacity), 100)
	return m.announceHosted(fmt.Sprintf(m.t("layout.chat_opacity"), m.chatOverlayOpacity))
}

// togglePanelSides swaps the list and the chat around the video.
func (m *Model) togglePanelSides() tea.Cmd {
	m.panelsSwapped = !m.panelsSwapped
	notice := "layout.sides_normal"
	if m.panelsSwapped {
		notice = "layout.sides_swapped"
	}
	return m.announceHosted(m.t(notice))
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
	// The arrow moves the divider, so with the sides swapped each key does the opposite.
	step := hostedResizeStep
	if m.panelsSwapped {
		step = -step
	}
	switch {
	case key.Matches(msg, m.keys.ToggleChatOverlay):
		return true, m.toggleChatOverlay()
	case key.Matches(msg, m.keys.SwapSides):
		return true, m.togglePanelSides()
	case key.Matches(msg, m.keys.ChatMoreOpaque):
		return true, m.adjustChatOpacity(hostedOpacityStep)
	case key.Matches(msg, m.keys.ChatLessOpaque):
		return true, m.adjustChatOpacity(-hostedOpacityStep)
	case key.Matches(msg, m.keys.ListWider):
		return true, m.resizeHostedList(step)
	case key.Matches(msg, m.keys.ListNarrower):
		return true, m.resizeHostedList(-step)
	case key.Matches(msg, m.keys.ChatWider):
		return true, m.resizeHostedChat(step)
	case key.Matches(msg, m.keys.ChatNarrower):
		return true, m.resizeHostedChat(-step)
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
	return m.announceHosted(fmt.Sprintf(m.t("layout.list_columns"), listW))
}

func (m *Model) resizeHostedChat(delta int) tea.Cmd {
	_, chatW, videoW := m.hostedWidths()
	if chatW == 0 {
		m.warnChatUnavailable()
		return nil
	}
	chatW = max(min(chatW+delta, chatW+m.hostedRoomToGrow(chatW, videoW)), hostedMinChat)
	m.hostedChatCols = chatW
	return m.announceHosted(fmt.Sprintf(m.t("layout.chat_columns"), chatW))
}

// hostedNoticeMsg ends a layout notice, unless another one replaced it.
type hostedNoticeMsg struct{ text string }

// announceHosted shows a layout notice for a moment and saves the layout.
func (m *Model) announceHosted(text string) tea.Cmd {
	m.statusNotice, m.statusNoticeWarns = text, false
	expire := tea.Tick(hostedNoticeDelay, func(time.Time) tea.Msg { return hostedNoticeMsg{text: text} })
	return tea.Batch(m.scheduleHostedSave(), expire)
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
	persist, list, chat, overlay, swapped, opacity := m.hostedPersist, m.hostedListCols, m.hostedChatCols, m.chatOverlay, m.panelsSwapped, m.chatOverlayOpacity
	return func() tea.Msg {
		_ = persist(list, chat, overlay, swapped, opacity)
		return nil
	}
}

// syncHostPanel tells the window about a changed video region.
func (m Model) syncHostPanel() Model {
	if m.videoHost == nil {
		return m
	}
	if panel := m.hostedPanel(); panel != m.hostPublished {
		m.videoHost.SetPanel(panel)
		m.hostPublished = panel
	}
	return m
}

// hostedPanel is the video's region, in 1-based cells. An overlay takes the screen
// over, so the zero value tells the host to draw the TUI's cells there instead.
func (m Model) hostedPanel() HostPanel {
	if m.videoHost == nil || m.quitting || m.width <= 0 || m.height <= 1 || m.channelFilterMenu {
		return HostPanel{}
	}
	listW, chatW, videoW := m.hostedWidths()
	if videoW == 0 {
		return HostPanel{}
	}
	leftW := listW
	if m.panelsSwapped {
		leftW = chatW
	}
	overlaid := m.chatOverlay && chatW > 0
	col := 1
	if leftW > 0 && (!overlaid || !m.panelsSwapped) {
		col += leftW + hostedSepWidth
	}
	panel := HostPanel{Row: 1, Col: col, Rows: m.height - 1, Cols: videoW, Visible: true}
	if overlaid {
		panel.OverlayCols, panel.OverlayLeft, panel.OverlayOpacity = chatW, m.panelsSwapped, m.chatOpacity()
	}
	if m.helpOpen() { // a ajuda ocupa a coluna do vídeo inteira, com a opacidade do chat
		panel.OverlayCols, panel.OverlayLeft, panel.OverlayOpacity = videoW, false, m.chatOpacity()
	}
	return panel
}

// hostedIdleText is what the video column says while no playback owns it.
func (m Model) hostedIdleText() string {
	switch m.playerState {
	case PlayerStateStarting, PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak:
		return ""
	default:
		return m.t("video.hosted_idle")
	}
}

// hostedView draws the three columns. The video column stays blank: the host paints
// over those cells while a playback is on.
func (m Model) hostedView(available int, overlay []string, status string) string {
	listW, chatW, videoW := m.hostedWidths()
	var list, chat []string
	if listW > 0 {
		list = m.renderBodyLines(listW)
	}
	// The help takes the video column over, so a chat lying there gives way to it.
	helpRows := m.helpOpen() && len(overlay) > 0 && videoW > hostedHelpPadding
	if helpRows && m.chatOverlay {
		chatW = 0
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
	helpTop := max((available-len(overlay))/2, 0)
	leftW, rightW, left, right := listW, chatW, list, chat
	if m.panelsSwapped {
		leftW, rightW, left, right = chatW, listW, chat, list
	}
	lines := make([]string, available)
	for i := range lines {
		var row strings.Builder
		if leftW > 0 {
			row.WriteString(cell(left, i, leftW))
			if !overlaid || !m.panelsSwapped {
				row.WriteString(separator)
			}
		}
		switch {
		case helpRows:
			text := ""
			if at := i - helpTop; at >= 0 && at < len(overlay) {
				text = " " + overlay[at]
			}
			row.WriteString(padOrTruncateVisual(text, openW))
		case i == available/2 && openW > 0:
			row.WriteString(padOrTruncateVisual(idle, openW))
		default:
			row.WriteString(strings.Repeat(" ", openW))
		}
		if rightW > 0 {
			if !overlaid || m.panelsSwapped {
				row.WriteString(separator)
			}
			row.WriteString(cell(right, i, rightW))
		}
		lines[i] = row.String()
	}
	if !helpRows {
		lines = m.overlayHelpLines(lines, overlay, m.width, available)
	}
	return strings.Join(lines, "\n") + "\n" + status
}
