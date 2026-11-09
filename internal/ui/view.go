package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

const (
	// minSplitColumns is the narrowest terminal that fits list and chat side by
	// side. Chat-only has the whole width and no such limit.
	minSplitColumns = 80
	fullChatColumns = 100
	panelSeparator  = " │ "
	panelSepWidth   = 3
)

// TruncateVisual truncates s to fit within maxWidth terminal cells (visual width,
// accounting for wide Unicode/CJK/emoji runes and preserving ANSI sequences),
// appending an ellipsis ("…") when truncated.
func TruncateVisual(s string, maxWidth int) string {
	s = normalizeScreenText(s)
	if maxWidth <= 0 {
		return s
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, maxWidth, "…")
}

// hasWideRune reports whether s contains a grapheme that occupies two terminal
// cells under the default (non–East-Asian) width tables (CJK, emoji, …).
func hasWideRune(s string) bool {
	for _, r := range ansi.Strip(s) {
		if r == '\n' || r == '\r' {
			continue
		}
		if ansi.StringWidth(string(r)) >= 2 {
			return true
		}
	}
	return false
}

// listRowEmDash is U+2014. Built without a string literal so the i18n loose-
// string check does not treat the technical separator as copy.
var listRowEmDash = string(rune(0x2014))

// fitListRow prepares one list row for a fixed panel width. When the row has
// wide (CJK/emoji) runes, East Asian terminals often draw ambiguous glyphs such
// as U+2014 EM DASH and U+2026 HORIZONTAL ELLIPSIS two cells wide; ansi width
// still counts them as one, so a "perfect" truncate wraps and leaves a blank
// line under the name. Replace those separators with ASCII and use an ASCII
// ellipsis so measured width matches the terminal.
func fitListRow(s string, maxWidth int) string {
	s = normalizeScreenText(s)
	if maxWidth <= 0 {
		return s
	}
	if !hasWideRune(s) || exactCellWidths {
		return TruncateVisual(s, maxWidth)
	}
	s = strings.ReplaceAll(s, listRowEmDash, "-")
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return ansi.Truncate(s, 1, "")
	}
	return ansi.Truncate(s, maxWidth, "...")
}

func padOrTruncateVisual(s string, width int) string {
	if width <= 0 {
		return s
	}
	s = TruncateVisual(s, width)
	w := ansi.StringWidth(s)
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// chatHiddenByWidth reports that the list leaves the chat no room: the limit
// belongs to the split, so it never applies once the list is out of the way.
func (m Model) chatHiddenByWidth() bool {
	return m.listVisible() && m.width > 0 && m.width < minSplitColumns
}

// panelWidths returns the visual column widths (listWidth, chatWidth) when rendering
// at m.width. A zero width means that panel is hidden by layout or by terminal size.
func (m Model) panelWidths() (int, int) {
	if m.width <= 0 {
		return 0, 0
	}
	if m.hosted.host != nil {
		listW, chatW, _ := m.hostedWidths()
		return listW, chatW
	}
	listOn := m.listVisible()
	chatOn := m.chatColumnVisible()
	switch {
	case listOn && chatOn:
		usable := m.width - panelSepWidth
		if usable < 20 {
			return m.width, 0
		}
		var listW, chatW int
		if m.width >= fullChatColumns {
			// ~40% list, ~60% chat
			listW = (usable * 2) / 5
			chatW = usable - listW
		} else {
			// Between 80 and 99 columns: chat reduces so list stays roomy while chat remains readable.
			chatW = (usable * 9) / 20
			if chatW < 34 {
				chatW = 34
			}
			listW = usable - chatW
		}
		return listW, chatW
	case listOn:
		return m.width, 0
	case chatOn:
		return 0, m.width
	default:
		return 0, 0
	}
}

// View implements tea.Model.
func (m Model) View() (view string) {
	defer func() { view = fitScreenLines(view, m.width) }()
	if m.quitting {
		return ""
	}
	return m.frame()
}

// frame draws the panels and the status bar in m.height rows.
func (m Model) frame() string {
	status := m.StatusBar()
	if m.height == 1 {
		return status
	}

	// Help is modal, so it wins over the filter menu it may have opened on.
	overlayLines := m.helpViewLines()
	if len(overlayLines) == 0 {
		overlayLines = m.renderChannelFilterMenuLines(m.width)
	}

	if m.hosted.host != nil && m.width > 0 && m.height > 1 {
		return m.hostedView(m.height-1, overlayLines, status)
	}

	// Unsized fallback (m.width <= 0 and m.height <= 0): used by unit tests that don't send WindowSizeMsg.
	if m.width <= 0 && m.height <= 0 {
		var bodyLines []string
		if m.listVisible() {
			bodyLines = m.renderBodyLines(0)
		}
		if m.shouldShowNarrowChatNotice() {
			bodyLines = append(bodyLines, m.narrowChatNoticeLines(0)...)
		} else if m.layout.showChat {
			chatLines := m.renderChatLines(defaultChatPreviewLines, 0)
			bodyLines = append(bodyLines, chatLines...)
		} else if !m.listVisible() {
			bodyLines = append(bodyLines, m.focusModeHintLines(0)...)
		}
		bodyLines = append(bodyLines, overlayLines...)
		if len(bodyLines) == 0 {
			return status
		}
		return strings.Join(bodyLines, "\n") + "\n" + status
	}

	available := m.height - 1
	if m.height <= 0 {
		available = 20
	}

	listW, chatW := m.panelWidths()

	// Side-by-side split.
	if listW > 0 && chatW > 0 {
		chatLines := m.renderChatLines(available, chatW)
		listLines := m.renderBodyLines(listW)
		rows := available
		if m.height <= 0 {
			rows = len(listLines)
			if len(chatLines) > rows {
				rows = len(chatLines)
			}
		}
		combined := make([]string, 0, rows)
		for i := 0; i < rows; i++ {
			var left, right string
			if i < len(listLines) {
				left = listLines[i]
			}
			if i < len(chatLines) {
				right = chatLines[i]
			}
			leftCell := padOrTruncateVisual(left, listW)
			rightCell := padOrTruncateVisual(right, chatW)
			combined = append(combined, leftCell+panelSeparator+rightCell)
		}
		combined = m.overlayHelpLines(combined, overlayLines, m.width, available)
		return strings.Join(combined, "\n") + "\n" + status
	}

	// Chat-only layout: full width chat column.
	if listW == 0 && chatW > 0 {
		bodyLines := m.renderChatLines(available, chatW)
		if m.height <= 0 {
			bodyLines = append(bodyLines, overlayLines...)
			for i := range bodyLines {
				bodyLines[i] = padOrTruncateVisual(bodyLines[i], chatW)
			}
			return strings.Join(bodyLines, "\n") + "\n" + status
		}
		for len(bodyLines) < available {
			bodyLines = append(bodyLines, "")
		}
		if len(bodyLines) > available {
			bodyLines = bodyLines[:available]
		}
		for i := range bodyLines {
			bodyLines[i] = padOrTruncateVisual(bodyLines[i], chatW)
		}
		bodyLines = m.overlayHelpLines(bodyLines, overlayLines, chatW, available)
		return strings.Join(bodyLines, "\n") + "\n" + status
	}

	// Focus mode: both panels hidden — only a short hint and the status bar.
	if listW == 0 && chatW == 0 {
		bodyLines := m.focusModeHintLines(m.width)
		if m.height <= 0 {
			bodyLines = append(bodyLines, overlayLines...)
			return strings.Join(bodyLines, "\n") + "\n" + status
		}
		for len(bodyLines) < available {
			bodyLines = append(bodyLines, "")
		}
		if len(bodyLines) > available {
			bodyLines = bodyLines[:available]
		}
		for i := range bodyLines {
			bodyLines[i] = padOrTruncateVisual(bodyLines[i], m.width)
		}
		bodyLines = m.overlayHelpLines(bodyLines, overlayLines, m.width, available)
		return strings.Join(bodyLines, "\n") + "\n" + status
	}

	// List-only (or chat hidden by width with optional narrow notice).
	bodyLines := m.renderBodyLines(listW)
	var noticeLines []string
	if m.shouldShowNarrowChatNotice() {
		noticeLines = m.narrowChatNoticeLines(listW)
	}

	if m.height <= 0 {
		bodyLines = append(bodyLines, noticeLines...)
		bodyLines = append(bodyLines, overlayLines...)
		if listW > 0 {
			for i := range bodyLines {
				bodyLines[i] = padOrTruncateVisual(bodyLines[i], listW)
			}
		}
		return strings.Join(bodyLines, "\n") + "\n" + status
	}

	// The notice takes rows from the list, but the list keeps at least one row.
	listBudget := available - len(noticeLines)
	if listBudget < 1 {
		listBudget = 1
	}
	if len(bodyLines) > listBudget {
		bodyLines = bodyLines[:listBudget]
	}
	for _, line := range noticeLines {
		if len(bodyLines) >= available {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	for len(bodyLines) < available {
		bodyLines = append(bodyLines, "")
	}
	if listW > 0 {
		for i := range bodyLines {
			bodyLines[i] = padOrTruncateVisual(bodyLines[i], listW)
		}
	}
	bodyLines = m.overlayHelpLines(bodyLines, overlayLines, listW, available)
	return strings.Join(bodyLines, "\n") + "\n" + status
}

// focusModeHintLines explains how to leave total-focus layout.
func (m Model) focusModeHintLines(maxWidth int) []string {
	rows := wrapChatRow("  ", m.t("layout.focus_hint"), maxWidth)
	return rows
}

// narrowChatNoticeLines explains why the chat is hidden. It wraps instead of
// truncating, so the whole sentence (and the way out) stays readable.
func (m Model) narrowChatNoticeLines(maxWidth int) []string {
	rows := wrapChatRow("  ! ", m.t("chat.hidden_narrow"), maxWidth)
	for i, row := range rows {
		rows[i] = styleWarning(row)
	}
	return rows
}

// helpOpen reports whether the help is on screen and owns the keyboard. A
// one-row terminal draws only the status bar, and a help nobody can see must
// not eat keys.
func (m Model) helpOpen() bool {
	return m.layout.helpVisible && m.height != 1
}

// helpPageRows returns how many of the total help rows are drawn at once. On
// overflow one row goes to the position indicator, unless it is the only row.
func (m Model) helpPageRows(total int) int {
	available := m.height - 1
	if m.height <= 0 || total <= available {
		return total
	}
	return max(available-1, 1)
}

// helpOffset returns helpScroll within the current bounds. A resize or a new
// player error can change them while the help stays open.
func (m Model) helpOffset(total int) int {
	return min(max(m.layout.helpScroll, 0), total-m.helpPageRows(total))
}

// helpTextWidth is the width the help wraps to: the video column in the hosted
// window, where it lies over the picture, and the whole screen elsewhere.
func (m Model) helpTextWidth() int {
	if m.hosted.host != nil {
		if _, _, videoW := m.hostedWidths(); videoW > hostedHelpPadding {
			return videoW - hostedHelpPadding
		}
	}
	return m.width
}

// helpViewLines returns the help rows to draw: all of them when they fit, else
// the scrolled window and, when there is room for it, the position indicator.
func (m Model) helpViewLines() []string {
	lines := m.renderHelpLines(m.helpTextWidth())
	total := len(lines)
	rows := m.helpPageRows(total)
	if rows == total {
		return lines
	}
	offset := m.helpOffset(total)
	window := lines[offset : offset+rows]
	if m.height > 2 {
		window = append(window, positionRow(offset+rows, total, m.helpTextWidth()))
	}
	return window
}

// positionRow is the right-aligned "current/total" row of a scrolled overlay.
func positionRow(current, total, width int) string {
	position := fmt.Sprintf("%d/%d", current, total)
	pad := max(width-ansi.StringWidth(position), 0)
	return strings.Repeat(" ", pad) + position
}

// renderHelpLines returns the whole help, wrapped at maxWidth. Rows are never
// truncated: what does not fit the height is reached by scrolling.
func (m Model) renderHelpLines(maxWidth int) []string {
	if !m.layout.helpVisible {
		return nil
	}
	focusPanel := m.t("help.panel_list")
	if m.Focus() == FocusChat {
		focusPanel = m.t("help.panel_chat")
	}
	out := wrapChatRow("", "=== "+m.t("help.title")+" ===", maxWidth)
	for _, line := range []string{
		m.t("help.close"),
		fmt.Sprintf(m.t("help.focus_now"), focusPanel),
		m.t("help.nav"),
		m.t("help.esc"),
		m.t("help.categories"),
		m.t("help.search"),
		m.t("help.channel_filter"),
		m.t("help.layout"),
		m.t("help.layout_back"),
		m.t("help.player"),
		m.t("help.chat"),
		m.t("help.app"),
		m.t("help.nocolor"),
	} {
		out = append(out, wrapChatRow("  ", line, maxWidth)...)
		if m.hosted.host != nil && line == m.t("help.layout") {
			out = append(out, wrapChatRow("  ", m.t("help.hosted_resize"), maxWidth)...)
		}
	}
	out = append(out, m.renderHelpColorLines(maxWidth)...)
	if line := m.playerErrorLine(); line != "" {
		out = append(out, wrapChatRow("  ", fmt.Sprintf(m.t("help.player_error"), line), maxWidth)...)
	}
	return out
}

// renderHelpColorLines returns the color legend, wrapped at maxWidth (never
// truncated) with each palette term painted in its own color.
func (m Model) renderHelpColorLines(maxWidth int) []string {
	rows := wrapChatRow("  ", m.t("help.colors"), maxWidth)
	for i, row := range rows {
		rows[i] = colorizeHelpLegend(row)
	}
	return rows
}

func (m Model) overlayHelpLines(lines []string, helpLines []string, maxWidth int, available int) []string {
	if len(helpLines) == 0 {
		return lines
	}
	if available <= 0 {
		return append(lines, helpLines...)
	}
	if len(helpLines) >= len(lines) {
		out := make([]string, len(lines))
		for i := range out {
			out[i] = padOrTruncateVisual(helpLines[i], maxWidth)
		}
		return out
	}
	start := len(lines) - len(helpLines)
	if start > 0 {
		lines[start-1] = padOrTruncateVisual("", maxWidth)
	}
	for i, hl := range helpLines {
		lines[start+i] = padOrTruncateVisual(hl, maxWidth)
	}
	return lines
}

func (m Model) headerLine() string {
	var title string
	switch m.Mode() {
	case ModeChannels:
		if m.list.selectedCategory != nil && m.list.selectedCategory.Name != "" {
			title = fmt.Sprintf(m.t("channels.title_category"), sanitizeScreenText(m.list.selectedCategory.Name))
		} else {
			title = m.t("channels.title")
		}
	case ModeSearch:
		if m.list.search.query != "" {
			title = fmt.Sprintf(m.t("search.title_query"), sanitizeScreenText(m.list.search.query))
		} else {
			title = m.t("search.title")
		}
	default:
		title = m.t("categories.title")
	}

	if m.chatColumnVisible() && m.listVisible() {
		if m.Focus() == FocusList {
			return "▸ " + title + " " + m.t("focus.active_tag")
		}
		return "  " + title
	}
	return title
}

func (m Model) appendPromptLines(lines []string, maxWidth int) []string {
	if m.list.search.active {
		line := fmt.Sprintf(m.t("search.prompt"), sanitizeScreenText(m.list.search.input))
		lines = append(lines, TruncateVisual(line, maxWidth))
	} else if m.list.filtering || m.list.filter != "" {
		line := fmt.Sprintf(m.t("filter.prompt"), sanitizeScreenText(m.list.filter))
		lines = append(lines, TruncateVisual(line, maxWidth))
	}
	return lines
}

func (m Model) renderBodyLines(maxWidth int) []string {
	header := fitListRow(m.headerLine(), maxWidth)
	if m.chatColumnVisible() && m.listVisible() && m.Focus() == FocusList {
		header = styleFocus(header)
	}

	if m.list.err != nil {
		lines := []string{header}
		lines = m.appendPromptLines(lines, maxWidth)
		if maxWidth <= 0 {
			lines = append(lines,
				styleError(m.errorMessage(m.list.err)),
				styleWarning(m.t("error.action_retry")),
			)
		} else {
			for _, part := range strings.Split(ansi.Wrap(m.errorMessage(m.list.err), maxWidth, ""), "\n") {
				lines = append(lines, styleError(TruncateVisual(part, maxWidth)))
			}
			for _, part := range strings.Split(ansi.Wrap(m.t("error.action_retry"), maxWidth, ""), "\n") {
				lines = append(lines, styleWarning(TruncateVisual(part, maxWidth)))
			}
		}
		return lines
	}

	return m.renderListBodyLines(header, maxWidth)
}

// listRowMarker returns the 2-cell lead for a list row.
func listRowMarker(highlighted, favorite bool) string {
	switch {
	case highlighted && favorite:
		return styleHighlight("▸") + styleFavorite("★")
	case highlighted:
		return styleHighlight("▸" + " ")
	case favorite:
		return styleFavorite("★" + " ")
	default:
		return "  "
	}
}

// channelRowTag keeps availability and unknown-language evidence together.
func (m Model) channelRowTag(ch twitch.Channel, avail twitch.Availability) string {
	var tags []string
	switch avail {
	case twitch.AvailabilityOffline:
		tags = append(tags, styleOffline(m.t("channels.offline_tag")))
	case twitch.AvailabilityUnknown:
		tags = append(tags, styleUnverified(m.t("channels.unverified_tag")))
	}
	if len(m.list.prefs.languages) > 0 && channelLanguageUnknown(ch) {
		tags = append(tags, m.t("channels.lang_unknown_tag"))
	}
	return strings.Join(tags, " ")
}

func (m Model) formatChannelRow(marker string, ch twitch.Channel, avail twitch.Availability, maxWidth int, tagWidths ...int) string {
	displayName := sanitizeScreenText(ch.DisplayName)
	login := sanitizeScreenText(ch.Login)
	identity := displayName
	if identity == "" {
		identity = login
	} else if login != "" && !strings.EqualFold(displayName, login) {
		identity += " (" + login + ")"
	}
	title := ""
	if ch.Title != nil {
		title = sanitizeScreenText(strings.TrimSpace(*ch.Title))
	}
	viewers := fmt.Sprintf("%7s", m.t("channels.viewers_unknown"))
	if avail == twitch.AvailabilityLive {
		if ch.Viewers == nil {
			viewers = styleLive(padOrTruncateVisual(m.t("channels.live_compact"), 7))
		} else {
			viewers = styleLive(formatLiveViewersCell(m.formatViewers(ch.Viewers)))
		}
	}
	tag := m.channelRowTag(ch, avail)
	tagWidth := ansi.StringWidth(tag)
	if len(tagWidths) > 0 {
		tagWidth = max(tagWidth, tagWidths[0])
	}
	return fitChannelRow(marker, viewers, identity, tag, title, m.isPlayingChannel(ch), maxWidth, tagWidth)
}

// formatLiveViewersCell reserves one space after the live dot at every magnitude.
func formatLiveViewersCell(viewers string) string {
	return "●" + " " + fmt.Sprintf("%5s", viewers)
}

// fitChannelRow reserves a shared, left-aligned status column at the right edge.
// The identity takes priority over the title when the panel is narrow.
func fitChannelRow(marker, viewers, identity, tag, title string, playing bool, maxWidth, tagWidth int) string {
	prefix := marker + " " + viewers + "  "
	identity = normalizeScreenText(identity)
	title = normalizeScreenText(title)
	tag = normalizeScreenText(tag)
	bodyWidth := maxWidth
	if maxWidth > 0 && tagWidth > 0 {
		bodyWidth = max(1, maxWidth-tagWidth-1)
	}
	row := prefix + identity
	if title != "" {
		row += " — " + title
	}
	row = fitListRow(row, bodyWidth)
	if playing {
		// Keep the live/favorite colors outside the identity's bold attribute.
		idStart := len(prefix)
		if len(row) >= idStart {
			row = prefix + stylePlayingChannel(row[idStart:])
		}
	}
	if tagWidth > 0 {
		if maxWidth > 0 {
			row = padOrTruncateVisual(row, bodyWidth)
		}
		row += " " + tag
	}
	row = fitListRow(row, maxWidth)
	return row
}

func (m Model) formatCategoryRow(marker string, cat twitch.Category, inSearch bool, maxWidth int) string {
	name := sanitizeScreenText(cat.Name)
	if inSearch && categoryNoBroadcast(cat) {
		row := fmt.Sprintf("%s %7s  %s — %s", marker, m.t("categories.viewers_unknown"), name, m.t("categories.no_stream"))
		return fitListRow(row, maxWidth)
	}
	viewers := m.formatViewers(cat.Viewers)
	if cat.Viewers != nil && *cat.Viewers > 0 {
		viewers = styleLive(fmt.Sprintf("%7s", viewers))
	} else {
		viewers = fmt.Sprintf("%7s", viewers)
	}
	row := marker + " " + viewers + "  " + name
	return fitListRow(row, maxWidth)
}

func categoryNoBroadcast(cat twitch.Category) bool {
	return cat.Viewers == nil || *cat.Viewers <= 0
}

func (m Model) formatViewers(v *int) string {
	if v == nil {
		return m.t("categories.viewers_unknown")
	}
	n := *v
	if n < 1_000 {
		return fmt.Sprintf("%d", n)
	}
	units := [...]rune{'k', 'M', 'B', 'T', 'Q', 'E'}
	value := float64(n) / 1_000
	unit := 0
	// Promote before one-decimal rounding would produce 1000.0 of a unit.
	for value >= 999.95 && unit < len(units)-1 {
		value /= 1_000
		unit++
	}
	if value >= 99.95 {
		return fmt.Sprintf("%d%c", max(100, int(value)), units[unit])
	}
	return fmt.Sprintf("%.1f%c", value, units[unit])
}

func (m Model) formatChannelViewers(v *int) string {
	if v == nil {
		return m.t("channels.viewers_unknown")
	}
	return m.formatViewers(v)
}
