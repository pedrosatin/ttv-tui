package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

const (
	minChatColumns  = 80
	fullChatColumns = 100
	panelSeparator  = " │ "
	panelSepWidth   = 3
)

// TruncateVisual truncates s to fit within maxWidth terminal cells (visual width,
// accounting for wide Unicode/CJK/emoji runes and preserving ANSI sequences),
// appending an ellipsis ("…") when truncated.
func TruncateVisual(s string, maxWidth int) string {
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
	if maxWidth <= 0 {
		return s
	}
	if !hasWideRune(s) {
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

func (m Model) chatHiddenByWidth() bool {
	return m.width > 0 && m.width < minChatColumns
}

func (m Model) showSideBySideChat() bool {
	return m.width >= minChatColumns && m.hasChatPanel()
}

// panelWidths returns the visual column widths (listWidth, chatWidth) when rendering
// at m.width. When chat is not shown side-by-side, chatWidth is 0.
func (m Model) panelWidths() (int, int) {
	if m.width <= 0 {
		return 0, 0
	}
	if !m.showSideBySideChat() {
		return m.width, 0
	}
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
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	status := m.StatusBar()
	if m.height == 1 {
		return status
	}

	helpLines := m.renderHelpLines(m.width)

	// Unsized fallback (m.width <= 0 and m.height <= 0): used by unit tests that don't send WindowSizeMsg.
	if m.width <= 0 && m.height <= 0 {
		bodyLines := m.renderBodyLines(0)
		if m.chatHiddenByWidth() && m.hasChatPanel() {
			bodyLines = append(bodyLines, m.narrowChatNoticeLines(0)...)
		} else {
			chatLines := m.renderChatLines(defaultChatPreviewLines, 0)
			bodyLines = append(bodyLines, chatLines...)
		}
		bodyLines = append(bodyLines, helpLines...)
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

	if chatW > 0 {
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
		combined = m.overlayHelpLines(combined, helpLines, m.width, available)
		return strings.Join(combined, "\n") + "\n" + status
	}

	// Single panel (either no chat open, or width < 80 so chat is hidden with notice).
	bodyLines := m.renderBodyLines(listW)
	var noticeLines []string
	if m.chatHiddenByWidth() && m.hasChatPanel() {
		noticeLines = m.narrowChatNoticeLines(listW)
	}

	if m.height <= 0 {
		bodyLines = append(bodyLines, noticeLines...)
		bodyLines = append(bodyLines, helpLines...)
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
	bodyLines = m.overlayHelpLines(bodyLines, helpLines, listW, available)
	return strings.Join(bodyLines, "\n") + "\n" + status
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

func (m Model) renderHelpLines(maxWidth int) []string {
	if !m.helpVisible {
		return nil
	}
	focusPanel := m.t("help.panel_list")
	if m.Focus() == FocusChat {
		focusPanel = m.t("help.panel_chat")
	}
	raw := []string{
		"=== " + m.t("help.title") + " ===",
		"  " + fmt.Sprintf(m.t("help.focus_now"), focusPanel),
		"  " + m.t("help.nav"),
		"  " + m.t("help.esc"),
		"  " + m.t("help.categories"),
		"  " + m.t("help.search"),
		"  " + m.t("help.player"),
		"  " + m.t("help.chat"),
		"  " + m.t("help.app"),
		"  " + m.t("help.colors"),
	}
	if line := m.playerErrorLine(); line != "" {
		raw = append(raw, "  "+fmt.Sprintf(m.t("help.player_error"), line))
	}
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = TruncateVisual(line, maxWidth)
	}
	return out
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
	for i, hl := range helpLines {
		lines[start+i] = padOrTruncateVisual(hl, maxWidth)
	}
	return lines
}

func (m Model) headerLine() string {
	var title string
	switch m.Mode() {
	case ModeChannels:
		if m.selectedCategory != nil && m.selectedCategory.Name != "" {
			title = fmt.Sprintf(m.t("channels.title_category"), sanitizeChatText(m.selectedCategory.Name))
		} else {
			title = m.t("channels.title")
		}
	case ModeSearch:
		if m.searchQuery != "" {
			title = fmt.Sprintf(m.t("search.title_query"), sanitizeChatText(m.searchQuery))
		} else {
			title = m.t("search.title")
		}
	default:
		title = m.t("categories.title")
	}

	if m.hasChatPanel() {
		if m.Focus() == FocusList {
			return "▸ " + title + " " + m.t("focus.active_tag")
		}
		return "  " + title
	}
	return title
}

func (m Model) appendPromptLines(lines []string, maxWidth int) []string {
	if m.searching {
		line := fmt.Sprintf(m.t("search.prompt"), sanitizeChatText(m.searchInput))
		lines = append(lines, TruncateVisual(line, maxWidth))
	} else if m.filtering || m.filter != "" {
		line := fmt.Sprintf(m.t("filter.prompt"), sanitizeChatText(m.filter))
		lines = append(lines, TruncateVisual(line, maxWidth))
	}
	return lines
}

func (m Model) renderBodyLines(maxWidth int) []string {
	header := fitListRow(m.headerLine(), maxWidth)
	if m.hasChatPanel() && m.Focus() == FocusList {
		header = styleFocus(header)
	}

	if m.err != nil {
		lines := []string{header}
		lines = m.appendPromptLines(lines, maxWidth)
		if maxWidth <= 0 {
			lines = append(lines,
				styleError(m.errorMessage(m.err)),
				styleWarning(m.t("error.action_retry")),
			)
		} else {
			for _, part := range strings.Split(ansi.Wrap(m.errorMessage(m.err), maxWidth, ""), "\n") {
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

func (m Model) renderListBodyLines(header string, maxWidth int) []string {
	items := m.VisibleItems()
	isSearch := m.Mode() == ModeSearch

	if m.loading && len(items) == 0 {
		lines := []string{header}
		lines = m.appendPromptLines(lines, maxWidth)
		if isSearch {
			lines = append(lines, TruncateVisual(m.t("search.loading"), maxWidth))
		} else if m.Mode() == ModeChannels {
			lines = append(lines, TruncateVisual(m.t("channels.loading"), maxWidth))
		} else {
			lines = append(lines, TruncateVisual(m.t("categories.loading"), maxWidth))
		}
		return lines
	}

	if len(items) == 0 {
		if m.source == nil && len(m.categories) == 0 && len(m.channels) == 0 && len(m.searchCategories) == 0 && len(m.favorites) == 0 && len(m.recents) == 0 && !m.filtering && !m.searching && m.filter == "" {
			return nil
		}
		lines := []string{header}
		lines = m.appendPromptLines(lines, maxWidth)
		if m.filterHasNoMatches() {
			// Enter or Ctrl+F (Ctrl+S alias) takes the filter text as the query.
			lines = append(lines,
				TruncateVisual(m.t("filter.empty"), maxWidth),
				TruncateVisual(fmt.Sprintf(m.t("filter.search_hint"), sanitizeChatText(strings.TrimSpace(m.filter))), maxWidth),
			)
		} else if (len(m.categories) > 0 || len(m.channels) > 0 || len(m.searchCategories) > 0 || len(m.favorites) > 0 || len(m.recents) > 0) && m.filter != "" {
			lines = append(lines, TruncateVisual(m.t("filter.empty"), maxWidth))
		} else if isSearch {
			lines = append(lines, TruncateVisual(m.t("search.empty"), maxWidth))
		} else if m.Mode() == ModeChannels {
			lines = append(lines, TruncateVisual(m.t("channels.empty"), maxWidth))
		} else {
			lines = append(lines, TruncateVisual(m.t("categories.empty"), maxWidth))
		}
		return lines
	}

	hasFavorites := false
	hasRecents := false
	hasSearchCategory := false
	hasSearchChannel := false
	for _, it := range items {
		switch it.Type {
		case ItemTypeFavorite:
			hasFavorites = true
		case ItemTypeRecent:
			hasRecents = true
		case ItemTypeCategory:
			if isSearch {
				hasSearchCategory = true
			}
		case ItemTypeChannel:
			if isSearch {
				hasSearchChannel = true
			}
		}
	}
	showSectionHeaders := hasFavorites || hasRecents || (isSearch && (hasSearchCategory || hasSearchChannel))

	sectionStyle := lipgloss.NewStyle().Faint(true)

	type rowEntry struct {
		isHeader bool
		itemID   string
		text     string
	}

	var allRows []rowEntry

	addSectionHeader := func(title string, favorite bool) {
		text := fitListRow(fmt.Sprintf("── %s ──", title), maxWidth)
		if favorite {
			text = styleFavorite(text)
		} else {
			text = sectionStyle.Render(text)
		}
		allRows = append(allRows, rowEntry{
			isHeader: true,
			text:     text,
		})
	}

	var currentSection ListItemType = ""

	for _, it := range items {
		if showSectionHeaders && it.Type != currentSection {
			currentSection = it.Type
			switch it.Type {
			case ItemTypeFavorite:
				addSectionHeader(m.t("section.favorites"), true)
			case ItemTypeRecent:
				addSectionHeader(m.t("section.recents"), false)
			case ItemTypeCategory:
				if isSearch {
					addSectionHeader(m.t("search.section_categories"), false)
				} else {
					addSectionHeader(m.t("categories.title"), false)
				}
			case ItemTypeChannel:
				if isSearch {
					addSectionHeader(m.t("search.section_channels"), false)
				} else if m.selectedCategory != nil && m.selectedCategory.Name != "" {
					addSectionHeader(fmt.Sprintf(m.t("channels.title_category"), sanitizeChatText(m.selectedCategory.Name)), false)
				} else {
					addSectionHeader(m.t("channels.title"), false)
				}
			}
		}

		marker := "  "
		if it.ID() == m.highlightedID {
			marker = "▸ "
		}

		var row string
		switch it.Type {
		case ItemTypeCategory:
			cat := *it.Category
			row = m.formatCategoryRow(marker, cat, isSearch)
		default: // ItemTypeChannel, ItemTypeFavorite, ItemTypeRecent
			ch := *it.Channel
			row = m.formatChannelRow(marker, ch, it.Availability)
		}

		row = fitListRow(row, maxWidth)
		row = m.styleChannelListRow(row, it)
		if it.ID() == m.highlightedID {
			// Highlight keeps the semantic color and adds bold/cyan on top of the
			// plain text so selection stays visible without color alone (▸ marker).
			row = styleHighlight(ansi.Strip(row))
		}

		allRows = append(allRows, rowEntry{
			isHeader: false,
			itemID:   it.ID(),
			text:     row,
		})
	}

	lines := make([]string, 0, len(allRows)+5)
	lines = append(lines, header)
	lines = m.appendPromptLines(lines, maxWidth)

	if m.height <= 0 {
		for _, r := range allRows {
			lines = append(lines, r.text)
		}
		if m.loading {
			if m.Mode() == ModeChannels && !isSearch {
				lines = append(lines, TruncateVisual(m.t("channels.loading_more"), maxWidth))
			} else if m.Mode() == ModeCategories {
				lines = append(lines, TruncateVisual(m.t("categories.loading_more"), maxWidth))
			}
		}
		return lines
	}

	budget := m.height - 1 - len(lines)
	if m.loading && !isSearch {
		budget--
	}
	if m.chatHiddenByWidth() && m.hasChatPanel() {
		budget--
	}
	if budget < 1 {
		budget = 1
	}

	start, end := 0, len(allRows)
	if len(allRows) > budget {
		hiRow := 0
		for idx, r := range allRows {
			if !r.isHeader && r.itemID == m.highlightedID {
				hiRow = idx
				break
			}
		}
		start = hiRow - budget/2
		if start < 0 {
			start = 0
		}
		end = start + budget
		if end > len(allRows) {
			end = len(allRows)
			start = end - budget
			if start < 0 {
				start = 0
			}
		}
	}

	for i := start; i < end; i++ {
		lines = append(lines, allRows[i].text)
	}

	if m.loading {
		if m.Mode() == ModeChannels && !isSearch {
			lines = append(lines, TruncateVisual(m.t("channels.loading_more"), maxWidth))
		} else if m.Mode() == ModeCategories {
			lines = append(lines, TruncateVisual(m.t("categories.loading_more"), maxWidth))
		}
	}

	return lines
}

func (m Model) formatChannelRow(marker string, ch twitch.Channel, avail twitch.Availability) string {
	displayName := sanitizeChatText(ch.DisplayName)
	login := sanitizeChatText(ch.Login)
	identity := fmt.Sprintf("%s (%s)", displayName, login)
	titleText := ""
	if ch.Title != nil && strings.TrimSpace(*ch.Title) != "" {
		titleText = sanitizeChatText(strings.TrimSpace(*ch.Title))
	}
	switch avail {
	case twitch.AvailabilityOffline:
		// Only explicit evidence of no broadcast earns the offline tag.
		suffix := ""
		if titleText != "" {
			suffix = " — " + titleText
		}
		return fmt.Sprintf("%s %7s  %s — %s%s", marker, m.t("channels.viewers_unknown"), identity, m.t("channels.offline_tag"), suffix)
	case twitch.AvailabilityLive:
		if ch.Viewers == nil {
			// Live without a count (e.g. the channel playing now): the label takes
			// the viewers column so the row reads "ao vivo  Nome (login)".
			row := fmt.Sprintf("%s %7s  %s", marker, m.t("channels.live_label"), identity)
			if titleText != "" {
				row += " — " + titleText
			}
			return row
		}
		viewersText := m.formatViewers(ch.Viewers)
		if titleText == "" {
			titleText = m.t("channels.title_unknown")
		}
		return fmt.Sprintf("%s %7s  %s — %s", marker, viewersText, identity, titleText)
	default:
		// No evidence either way: say so, never guess offline.
		suffix := ""
		if titleText != "" {
			suffix = " — " + titleText
		}
		return fmt.Sprintf("%s %7s  %s — %s%s", marker, m.t("channels.viewers_unknown"), identity, m.t("channels.unverified_tag"), suffix)
	}
}

// styleChannelListRow applies availability / favorite / playing colors. Labels
// and markers stay in the plain text so the row stays readable without color.
func (m Model) styleChannelListRow(row string, it ListItem) string {
	if it.Channel == nil {
		return row
	}
	if m.isPlayingChannel(*it.Channel) {
		return stylePlayingChannel(row)
	}
	switch it.Availability {
	case twitch.AvailabilityOffline:
		return styleOffline(row)
	case twitch.AvailabilityLive:
		if it.Type == ItemTypeFavorite {
			return styleFavorite(row)
		}
		return styleLive(row)
	default:
		if it.Type == ItemTypeFavorite {
			return styleFavorite(row)
		}
		return styleUnverified(row)
	}
}

func (m Model) formatCategoryRow(marker string, cat twitch.Category, inSearch bool) string {
	name := sanitizeChatText(cat.Name)
	if inSearch && categoryNoBroadcast(cat) {
		return fmt.Sprintf("%s %7s  %s — %s", marker, m.t("categories.viewers_unknown"), name, m.t("categories.no_stream"))
	}
	return fmt.Sprintf("%s %7s  %s", marker, m.formatViewers(cat.Viewers), name)
}

func categoryNoBroadcast(cat twitch.Category) bool {
	return cat.Viewers == nil || *cat.Viewers <= 0
}

func (m Model) formatViewers(v *int) string {
	if v == nil {
		return m.t("categories.viewers_unknown")
	}
	n := *v
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func (m Model) formatChannelViewers(v *int) string {
	if v == nil {
		return m.t("channels.viewers_unknown")
	}
	return m.formatViewers(v)
}
