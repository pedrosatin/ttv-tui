package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// listRow is one drawn line of the list: a section header or an item.
type listRow struct {
	isHeader bool
	itemID   string
	text     string
}

// renderListBodyLines draws the list under header: the prompt, the rows that
// fit around the highlighted one, and the loading line.
func (m Model) renderListBodyLines(header string, maxWidth int) []string {
	items := m.VisibleItems()
	isSearch := m.Mode() == ModeSearch
	// An unavailable catalog keeps the saved channels on screen and says so
	// under them.
	unavailable := m.catalogUnavailableLines(maxWidth)

	if len(items) == 0 {
		lines := m.appendPromptLines([]string{header}, maxWidth)
		if m.list.loading {
			return append(lines, TruncateVisual(m.listLoadingText(), maxWidth))
		}
		if len(unavailable) > 0 {
			return append(lines, unavailable...)
		}
		return m.appendListEmptyLines(lines, maxWidth)
	}

	rows := m.listRows(items, isSearch, maxWidth)

	lines := make([]string, 0, len(rows)+5)
	lines = append(lines, header)
	lines = m.appendPromptLines(lines, maxWidth)
	lines = m.appendFilterHint(lines, maxWidth)

	start, end := 0, len(rows)
	if m.height > 0 {
		start, end = m.list.rowWindow(rows, m.listRowBudget(len(lines)+len(unavailable), isSearch, maxWidth))
	}
	for _, r := range rows[start:end] {
		lines = append(lines, r.text)
	}
	lines = append(lines, unavailable...)
	return m.appendListLoadingMore(lines, maxWidth)
}

// appendFilterHint diz, enquanto se digita no filtro, o que Enter e Ctrl+F
// fazem: Enter abre o item destacado, que está à vista logo abaixo, e Ctrl+F
// leva o texto à busca da plataforma.
func (m Model) appendFilterHint(lines []string, maxWidth int) []string {
	if !m.list.filtering {
		return lines
	}
	hint := fmt.Sprintf(m.t("filter.typing_hint"), platform.DisplayName(m.ListPlatform()))
	return append(lines, lipgloss.NewStyle().Faint(true).Render(TruncateVisual(hint, maxWidth)))
}

// listLoadingText is the line shown while the first page of the list loads.
func (m Model) listLoadingText() string {
	switch m.Mode() {
	case ModeSearch:
		return m.t("search.loading")
	case ModeChannels:
		return m.t("channels.loading")
	default:
		return m.t("categories.loading")
	}
}

// appendListEmptyLines explains an empty list and names the next step.
func (m Model) appendListEmptyLines(lines []string, maxWidth int) []string {
	switch {
	case m.filterHasNoMatches():
		// Enter or Ctrl+F (Ctrl+S alias) takes the filter text as the query.
		lines = append(lines,
			TruncateVisual(m.t("filter.empty"), maxWidth),
			TruncateVisual(fmt.Sprintf(m.t("filter.search_hint"), sanitizeScreenText(strings.TrimSpace(m.list.filter))), maxWidth),
		)
	case m.preferenceFilterHidesChannels():
		summary := m.list.prefs.summary()
		lines = append(lines,
			TruncateVisual(fmt.Sprintf(m.t("channel_filter.empty"), summary), maxWidth),
			TruncateVisual(m.t("channel_filter.empty_hint"), maxWidth),
		)
	default:
		lines = append(lines, TruncateVisual(m.listEmptyText(), maxWidth))
	}
	return append(lines, TruncateVisual(m.t("list.empty_action"), maxWidth))
}

// listEmptyText is the one-line reason for an empty list.
func (m Model) listEmptyText() string {
	if m.list.loadFailed {
		return m.t("list.not_loaded")
	}
	if m.list.hasData() && m.list.filter != "" {
		return m.t("filter.empty")
	}
	switch m.Mode() {
	case ModeSearch:
		return m.t("search.empty")
	case ModeChannels:
		return m.t("channels.empty")
	default:
		return m.t("categories.empty")
	}
}

// hasData reports whether anything was loaded or saved, shown or not.
func (l listState) hasData() bool {
	return len(l.categories) > 0 || len(l.channels) > 0 || len(l.search.categories) > 0 || len(l.favorites) > 0 || len(l.recents) > 0
}

// listRows formats every visible item, with a header where the section changes.
func (m Model) listRows(items []ListItem, isSearch bool, maxWidth int) []listRow {
	showSectionHeaders := listHasSections(items, isSearch)
	tagWidth := m.listTagWidth(items)
	rows := make([]listRow, 0, len(items))
	var currentSection ListItemType
	for _, it := range items {
		if showSectionHeaders && it.Type != currentSection {
			currentSection = it.Type
			if title, ok := m.listSectionTitle(it.Type, isSearch); ok {
				rows = append(rows, listSectionHeader(title, maxWidth))
			}
		}
		rows = append(rows, listRow{
			itemID: it.ID(),
			text:   m.formatListItemRow(it, isSearch, maxWidth, tagWidth),
		})
	}
	return rows
}

// listHasSections reports whether the list mixes groups that need a header.
func listHasSections(items []ListItem, isSearch bool) bool {
	for _, it := range items {
		switch it.Type {
		case ItemTypeFavorite, ItemTypeRecent:
			return true
		case ItemTypeCategory, ItemTypeChannel, ItemTypeExactChannel:
			if isSearch {
				return true
			}
		}
	}
	return false
}

// listTagWidth is the widest status tag, so the tag column lines up.
func (m Model) listTagWidth(items []ListItem) int {
	tagWidth := 0
	for _, it := range items {
		if it.Channel != nil {
			tagWidth = max(tagWidth, ansi.StringWidth(m.channelRowTag(*it.Channel, it.Availability)))
		}
	}
	return tagWidth
}

// listSectionTitle names the section an item type opens. An unknown type opens
// a section without a header.
func (m Model) listSectionTitle(itemType ListItemType, isSearch bool) (string, bool) {
	switch itemType {
	case ItemTypeFavorite:
		return m.t("section.favorites"), true
	case ItemTypeRecent:
		return m.t("section.recents"), true
	case ItemTypeCategory:
		if isSearch {
			return m.t("search.section_categories"), true
		}
		return m.t("categories.title"), true
	case ItemTypeExactChannel:
		return m.t("search.section_exact"), true
	case ItemTypeChannel:
		if isSearch {
			return m.t("search.section_channels"), true
		}
		if m.list.selectedCategory != nil && m.list.selectedCategory.Name != "" {
			return fmt.Sprintf(m.t("channels.title_category"), sanitizeScreenText(m.list.selectedCategory.Name)), true
		}
		return m.t("channels.title"), true
	}
	return "", false
}

func listSectionHeader(title string, maxWidth int) listRow {
	text := fitListRow(fmt.Sprintf("── %s ──", title), maxWidth)
	return listRow{isHeader: true, text: lipgloss.NewStyle().Faint(true).Render(text)}
}

// formatListItemRow draws one item, reversed when it is the highlighted one.
func (m Model) formatListItemRow(it ListItem, isSearch bool, maxWidth, tagWidth int) string {
	highlighted := it.ID() == m.list.highlightedID
	marker := listRowMarker(highlighted, it.Type == ItemTypeFavorite || it.Favorite)

	var row string
	switch it.Type {
	case ItemTypeMoreRecents:
		row = fitListRow(fmt.Sprintf("%s %7s  %s", marker, "", fmt.Sprintf(m.t("recents.more"), it.Hidden)), maxWidth)
	case ItemTypeCategory:
		row = m.formatCategoryRow(marker, *it.Category, isSearch, maxWidth)
	default: // ItemTypeChannel, ItemTypeExactChannel, ItemTypeFavorite, ItemTypeRecent
		row = m.formatChannelRow(marker, *it.Channel, it.Availability, maxWidth, tagWidth)
	}
	if !highlighted {
		return row
	}
	if maxWidth > 0 {
		row = padOrTruncateVisual(row, maxWidth)
	}
	// One reverse run keeps nested SGR resets from breaking selection.
	return styleSelectedRow(ansi.Strip(row))
}

// listRowBudget is how many rows fit once the used lines, the status bar, the
// loading line and the narrow-chat notice have their room. Never below one.
func (m Model) listRowBudget(used int, isSearch bool, maxWidth int) int {
	budget := m.height - 1 - used
	if m.list.loading && !isSearch {
		budget--
	}
	if m.shouldShowNarrowChatNotice() && m.hosted.host == nil {
		budget -= len(m.narrowChatNoticeLines(maxWidth))
	}
	return max(budget, 1)
}

// rowWindow picks the slice of rows to draw, centred on the highlighted
// item and clamped to both ends.
func (l listState) rowWindow(rows []listRow, budget int) (start, end int) {
	if len(rows) <= budget {
		return 0, len(rows)
	}
	hiRow := 0
	for idx, r := range rows {
		if !r.isHeader && r.itemID == l.highlightedID {
			hiRow = idx
			break
		}
	}
	start = max(hiRow-budget/2, 0)
	end = start + budget
	if end > len(rows) {
		end = len(rows)
		start = max(end-budget, 0)
	}
	return start, end
}

// appendListLoadingMore adds the line shown while another page loads. Search
// has no further pages.
func (m Model) appendListLoadingMore(lines []string, maxWidth int) []string {
	if !m.list.loading {
		return lines
	}
	switch m.Mode() {
	case ModeChannels:
		lines = append(lines, TruncateVisual(m.t("channels.loading_more"), maxWidth))
	case ModeCategories:
		lines = append(lines, TruncateVisual(m.t("categories.loading_more"), maxWidth))
	}
	return lines
}
