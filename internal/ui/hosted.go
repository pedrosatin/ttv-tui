package ui

import (
	"fmt"
	"strings"
	"time"

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
		m.hosted.listCols, m.hosted.chatCols, m.hosted.chatOverlay, m.hosted.panelsSwapped, m.hosted.persist = list, chat, overlay, swapped, persist
		m.hosted.chatOverlayOpacity = opacity
	}
}

// WithVideoHost attaches the window that draws the video itself. The TUI then keeps
// the list on the left and the chat on the right, and tells the host which cells
// between them are the video's.
func WithVideoHost(host VideoHost) Option {
	return func(m *Model) {
		m.hosted.host = host
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
		if m.hosted.listCols > 0 {
			listW = max(m.hosted.listCols, hostedMinList)
		}
		separators += hostedSepWidth
	}
	if m.chatColumnVisible() {
		chatW = m.hostedDefaultChat()
		if m.hosted.chatCols > 0 {
			chatW = max(m.hosted.chatCols, hostedMinChat)
		}
		separators += hostedSepWidth
	}
	if m.hosted.chatOverlay && chatW > 0 {
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
	m.hosted.chatOverlay = !m.hosted.chatOverlay
	notice := "layout.chat_side"
	if m.hosted.chatOverlay {
		notice = "layout.chat_overlay"
	}
	return m.announceHosted(m.t(notice))
}

// chatOpacity is the percent opacity of the overlaid chat's background.
func (h hostedState) chatOpacity() int {
	if h.chatOverlayOpacity > 0 {
		return h.chatOverlayOpacity
	}
	return hostedDefaultOpacity
}

// adjustChatOpacity changes the background behind the overlaid chat.
func (m *Model) adjustChatOpacity(delta int) tea.Cmd {
	if !m.hosted.chatOverlay || !m.chatColumnVisible() {
		m.warnStatus(m.t("layout.opacity_needs_overlay"))
		return nil
	}
	m.hosted.chatOverlayOpacity = min(max(m.hosted.chatOpacity()+delta, hostedMinOpacity), 100)
	return m.announceHosted(fmt.Sprintf(m.t("layout.chat_opacity"), m.hosted.chatOverlayOpacity))
}

// togglePanelSides swaps the list and the chat around the video.
func (m *Model) togglePanelSides() tea.Cmd {
	m.hosted.panelsSwapped = !m.hosted.panelsSwapped
	notice := "layout.sides_normal"
	if m.hosted.panelsSwapped {
		notice = "layout.sides_swapped"
	}
	return m.announceHosted(m.t(notice))
}

// warnChatUnavailable says why a chat key cannot act.
func (m *Model) warnChatUnavailable() {
	if m.warnChatHiddenByWidth() {
		return
	}
	if m.chat.hasPanel() {
		m.warnStatus(m.t("layout.hidden_chat_key"))
	} else {
		m.warnStatus(m.t("layout.no_chat"))
	}
}

// handleHostedResizeKey moves a divider. It reports whether msg was one of its keys.
func (m *Model) handleHostedResizeKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.hosted.host == nil {
		return false, nil
	}
	// The arrow moves the divider, so with the sides swapped each key does the opposite.
	step := hostedResizeStep
	if m.hosted.panelsSwapped {
		step = -step
	}
	switch m.keys.actionFor(modeWindow, msg) {
	case actChatOverlay:
		return true, m.toggleChatOverlay()
	case actSwapSides:
		return true, m.togglePanelSides()
	case actChatMoreOpaque:
		return true, m.adjustChatOpacity(hostedOpacityStep)
	case actChatLessOpaque:
		return true, m.adjustChatOpacity(-hostedOpacityStep)
	case actListWider:
		return true, m.resizeHostedList(step)
	case actListNarrower:
		return true, m.resizeHostedList(-step)
	case actChatWider:
		return true, m.resizeHostedChat(step)
	case actChatNarrower:
		return true, m.resizeHostedChat(-step)
	}
	return false, nil
}

// roomToGrow is what a panel can still take: the picture outside the panels
// keeps its minimum.
func (h hostedState) roomToGrow(chatW, videoW int) int {
	if h.chatOverlay {
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
	listW = max(min(listW+delta, listW+m.hosted.roomToGrow(chatW, videoW)), hostedMinList)
	m.hosted.listCols = listW
	return m.announceHosted(fmt.Sprintf(m.t("layout.list_columns"), listW))
}

func (m *Model) resizeHostedChat(delta int) tea.Cmd {
	_, chatW, videoW := m.hostedWidths()
	if chatW == 0 {
		m.warnChatUnavailable()
		return nil
	}
	chatW = max(min(chatW+delta, chatW+m.hosted.roomToGrow(chatW, videoW)), hostedMinChat)
	m.hosted.chatCols = chatW
	return m.announceHosted(fmt.Sprintf(m.t("layout.chat_columns"), chatW))
}

// hostedNoticeMsg ends a layout notice, unless another one replaced it.
type hostedNoticeMsg struct{ gen uint64 }

// announceHosted shows a layout notice for a moment and saves the layout.
func (m *Model) announceHosted(text string) tea.Cmd {
	m.notices.gen++
	gen := m.notices.gen
	m.notices.status, m.notices.statusWarns = text, false
	expire := m.after(hostedNoticeDelay, func(time.Time) tea.Msg { return hostedNoticeMsg{gen: gen} })
	return tea.Batch(m.scheduleHostedSave(), expire)
}

// hostedSaveMsg asks to save the widths if no newer change came after it.
type hostedSaveMsg struct{ gen int }

// scheduleHostedSave waits for the key to stop: a held arrow repeats about thirty
// times a second, and only the last width is worth writing.
func (m *Model) scheduleHostedSave() tea.Cmd {
	if m.hosted.persist == nil {
		return nil
	}
	m.hosted.saveGen++
	gen := m.hosted.saveGen
	return m.after(hostedSaveDelay, func(time.Time) tea.Msg { return hostedSaveMsg{gen: gen} })
}

// saveColumns writes the widths, off the update loop, if gen is still the newest.
func (h hostedState) saveColumns(gen int) tea.Cmd {
	if h.persist == nil || gen != h.saveGen {
		return nil
	}
	persist, list, chat, overlay, swapped, opacity := h.persist, h.listCols, h.chatCols, h.chatOverlay, h.panelsSwapped, h.chatOverlayOpacity
	return h.saves.cmd(func() tea.Msg {
		return configSavedMsg{Err: persist(list, chat, overlay, swapped, opacity)}
	})
}

// syncHostPanel tells the window about a changed video region.
func (m Model) syncHostPanel() Model {
	if m.hosted.host == nil {
		return m
	}
	if panel := m.hostedPanel(); panel != m.hosted.published {
		m.hosted.host.SetPanel(panel)
		m.hosted.published = panel
	}
	return m
}

// hostedPanel is the video's region, in 1-based cells. An overlay takes the screen
// over, so the zero value tells the host to draw the TUI's cells there instead.
func (m Model) hostedPanel() HostPanel {
	if m.hosted.host == nil || m.quitting || m.width <= 0 || m.height <= 1 || m.list.prefs.menuOpen {
		return HostPanel{}
	}
	listW, chatW, videoW := m.hostedWidths()
	if videoW == 0 {
		return HostPanel{}
	}
	leftW := listW
	if m.hosted.panelsSwapped {
		leftW = chatW
	}
	overlaid := m.hosted.chatOverlay && chatW > 0
	col := 1
	if leftW > 0 && (!overlaid || !m.hosted.panelsSwapped) {
		col += leftW + hostedSepWidth
	}
	panel := HostPanel{Row: 1, Col: col, Rows: m.height - 1, Cols: videoW, Visible: true}
	if overlaid {
		panel.OverlayCols, panel.OverlayLeft, panel.OverlayOpacity = chatW, m.hosted.panelsSwapped, m.hosted.chatOpacity()
	}
	if m.helpOpen() { // a ajuda ocupa a coluna do vídeo inteira, com a opacidade do chat
		panel.OverlayCols, panel.OverlayLeft, panel.OverlayOpacity = videoW, false, m.hosted.chatOpacity()
	}
	return panel
}

// hostedIdleText is what the video column says while no playback owns it.
func (m Model) hostedIdleText() string {
	switch m.player.state {
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
	if helpRows && m.hosted.chatOverlay {
		chatW = 0
	}
	if chatW > 0 {
		chat = m.renderChatLines(available, chatW)
	}
	// With the chat over the video, its cells come out of the video column.
	overlaid := m.hosted.chatOverlay && chatW > 0
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
	if m.hosted.panelsSwapped {
		leftW, rightW, left, right = chatW, listW, chat, list
	}
	lines := make([]string, available)
	for i := range lines {
		var row strings.Builder
		if leftW > 0 {
			row.WriteString(cell(left, i, leftW))
			if !overlaid || !m.hosted.panelsSwapped {
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
			if !overlaid || m.hosted.panelsSwapped {
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
