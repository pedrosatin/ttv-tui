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

// HostedColumnsPersist saves the list and chat widths; 0 means automatic.
type HostedColumnsPersist func(list, chat int) error

// WithHostedColumns sets the saved widths and what saves them when they change.
func WithHostedColumns(list, chat int, persist HostedColumnsPersist) Option {
	return func(m *Model) {
		m.hostedListCols, m.hostedChatCols, m.hostedPersist = list, chat, persist
	}
}

// WithVideoHost attaches the window that draws the video itself. The TUI then keeps
// the list on the left and the chat on the right, and tells the host which cells
// between them are the video's.
func WithVideoHost(host VideoSurface) Option {
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

// handleHostedResizeKey moves a divider. It reports whether msg was one of its keys.
func (m *Model) handleHostedResizeKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.videoHost == nil {
		return false, nil
	}
	switch {
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

// hostedRoomToGrow is what a panel can take from the video, which keeps its minimum.
func hostedRoomToGrow(videoW int) int { return max(videoW-hostedMinVideo, 0) }

func (m *Model) resizeHostedList(delta int) tea.Cmd {
	listW, _, videoW := m.hostedWidths()
	if listW == 0 {
		m.warnStatus(m.t("layout.hidden_list_key"))
		return nil
	}
	listW = max(min(listW+delta, listW+hostedRoomToGrow(videoW)), hostedMinList)
	m.hostedListCols = listW
	m.statusNotice, m.statusNoticeWarns = fmt.Sprintf(m.t("layout.list_columns"), listW), false
	return m.scheduleHostedSave()
}

func (m *Model) resizeHostedChat(delta int) tea.Cmd {
	_, chatW, videoW := m.hostedWidths()
	if chatW == 0 {
		if m.hasChatPanel() {
			m.warnStatus(m.t("layout.hidden_chat_key"))
		} else {
			m.warnStatus(m.t("layout.no_chat"))
		}
		return nil
	}
	chatW = max(min(chatW+delta, chatW+hostedRoomToGrow(videoW)), hostedMinChat)
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
	persist, list, chat := m.hostedPersist, m.hostedListCols, m.hostedChatCols
	return func() tea.Msg {
		_ = persist(list, chat)
		return nil
	}
}

// hostedPanel is the video's region, in 1-based cells. An overlay takes the screen
// over, so the zero value tells the host to draw the TUI's cells there instead.
func (m Model) hostedPanel() VideoPanel {
	if m.videoHost == nil || m.quitting || m.width <= 0 || m.height <= 1 || m.helpVisible || m.channelFilterMenu {
		return VideoPanel{}
	}
	listW, _, videoW := m.hostedWidths()
	if videoW == 0 {
		return VideoPanel{}
	}
	col := 1
	if listW > 0 {
		col += listW + hostedSepWidth
	}
	return VideoPanel{Row: 1, Col: col, Rows: m.height - 1, Cols: videoW, Visible: true}
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
	idle := TruncateVisual(m.hostedIdleText(), videoW)
	if videoW == 0 {
		idle = ""
	}
	idle = strings.Repeat(" ", max(videoW-ansi.StringWidth(idle), 0)/2) + idle

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
		case i == available/2 && videoW > 0:
			row.WriteString(padOrTruncateVisual(idle, videoW))
		default:
			row.WriteString(strings.Repeat(" ", videoW))
		}
		if chatW > 0 {
			row.WriteString(separator)
			row.WriteString(cell(chat, i, chatW))
		}
		lines[i] = row.String()
	}
	lines = m.overlayHelpLines(lines, overlay, m.width, available)
	return strings.Join(lines, "\n") + "\n" + status
}
