package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// listRow is one drawn line of the list: a section header or an item.
type listRow struct {
	isHeader  bool
	itemID    string
	itemIndex int
	text      string
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

	rows := m.listRowPositions(items, isSearch, maxWidth)

	lines := make([]string, 0, len(rows)+5)
	lines = append(lines, header)
	lines = m.appendPromptLines(lines, maxWidth)
	lines = m.appendFilterHint(lines, maxWidth)

	start, end := 0, len(rows)
	if m.height > 0 {
		start, end = m.list.rowWindow(rows, m.listRowBudget(len(lines)+len(unavailable), isSearch, maxWidth))
	}
	tagWidth := m.listTagWidth(items, maxWidth)
	distinct := m.listDistinctNames(items, rows[start:end], maxWidth, tagWidth)
	for _, r := range rows[start:end] {
		if !r.isHeader {
			r.text = m.formatListItemRow(items[r.itemIndex], isSearch, maxWidth, tagWidth, distinct[items[r.itemIndex].ID()])
		}
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
	if m.Mode() == ModeChannels && m.list.hasMore && m.list.prefs.active() {
		lines = append(lines, TruncateVisual(m.t("channel_filter.more_hint"), maxWidth))
	}
	return append(lines, TruncateVisual(m.t("list.empty_action"), maxWidth))
}

// listEmptyText is the one-line reason for an empty list.
func (m Model) listEmptyText() string {
	if m.list.loadFailed {
		return m.t("list.not_loaded")
	}
	if m.hasData() && m.list.filter != "" {
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

// hasData reports whether anything was loaded, or saved for the active list, shown or not.
func (m Model) hasData() bool {
	return len(m.list.categories) > 0 || len(m.list.channels) > 0 || len(m.list.search.categories) > 0 || m.hasSavedOnList()
}

// listRowPositions builds the scroll geometry without formatting off-screen items.
func (m Model) listRowPositions(items []ListItem, isSearch bool, maxWidth int) []listRow {
	showSectionHeaders := listHasSections(items, isSearch)
	rows := make([]listRow, 0, len(items))
	var currentSection ListItemType
	for i, it := range items {
		if showSectionHeaders && it.Type != currentSection {
			currentSection = it.Type
			if title, ok := m.listSectionTitle(it.Type, isSearch); ok {
				rows = append(rows, listSectionHeader(title, maxWidth))
			}
		}
		rows = append(rows, listRow{itemID: it.ID(), itemIndex: i})
	}
	return rows
}

// listHasSections reports whether the list mixes groups that need a header.
func listHasSections(items []ListItem, isSearch bool) bool {
	for _, it := range items {
		switch it.Type {
		case ItemTypePinnedCategory, ItemTypeFavorite, ItemTypeRecent:
			return true
		case ItemTypeCategory, ItemTypeChannel, ItemTypeExactChannel, ItemTypeYouTubeOffer:
			if isSearch {
				return true
			}
		}
	}
	return false
}

// listTagWidth is the widest status tag, so the tag column lines up.
func (m Model) listTagWidth(items []ListItem, maxWidth int) int {
	tagWidth := 0
	for _, it := range items {
		if it.Channel != nil && it.Type != ItemTypeYouTubeOffer {
			tagWidth = max(tagWidth, ansi.StringWidth(m.channelRowTag(*it.Channel, it.Availability, maxWidth)))
		}
	}
	return tagWidth
}

// listItemCollisionLabel is the name compared for truncation collisions.
func (m Model) listItemCollisionLabel(it ListItem) (string, bool) {
	switch it.Type {
	case ItemTypeCategory, ItemTypePinnedCategory:
		if it.Category == nil || it.Category.Name == "" {
			return "", false
		}
		return sanitizeScreenText(it.Category.Name), true
	case ItemTypeChannel, ItemTypeExactChannel, ItemTypeFavorite, ItemTypeRecent:
		if it.Channel == nil {
			return "", false
		}
		label := m.channelIdentity(*it.Channel)
		if label == "" {
			return "", false
		}
		return label, true
	}
	return "", false
}

// listLabelBodyWidth is the width available to prefix+name before the tag column.
func listLabelBodyWidth(maxWidth, tagWidth int) int {
	if maxWidth <= 0 {
		return 0
	}
	if tagWidth > 0 {
		return max(1, maxWidth-tagWidth-1)
	}
	return maxWidth
}

// listDistinctNames marca os IDs cujos nomes, cortados só no fim, ficam iguais a
// outro nome diferente na janela visível. Essas linhas usam corte no meio.
func (m Model) listDistinctNames(items []ListItem, visible []listRow, maxWidth, tagWidth int) map[string]bool {
	type hit struct {
		id    string
		label string
	}
	byTrunc := map[string][]hit{}
	bodyWidth := listLabelBodyWidth(maxWidth, tagWidth)
	for _, r := range visible {
		if r.isHeader || r.itemIndex < 0 || r.itemIndex >= len(items) {
			continue
		}
		it := items[r.itemIndex]
		label, ok := m.listItemCollisionLabel(it)
		if !ok {
			continue
		}
		budget := listLabelBudget(bodyWidth, strings.Repeat(" ", listRowPrefixCells), label)
		if it.Category != nil {
			budget = listLabelBudget(maxWidth, strings.Repeat(" ", listRowPrefixCells), label)
		}
		trunc := endTruncatedListLabel(label, budget)
		byTrunc[trunc] = append(byTrunc[trunc], hit{id: it.ID(), label: label})
	}
	out := map[string]bool{}
	for _, group := range byTrunc {
		if len(group) < 2 {
			continue
		}
		first := group[0].label
		same := true
		for _, h := range group[1:] {
			if h.label != first {
				same = false
				break
			}
		}
		if same {
			continue
		}
		for _, h := range group {
			out[h.id] = true
		}
	}
	return out
}

// listSectionTitle names the section an item type opens. An unknown type opens
// a section without a header.
func (m Model) listSectionTitle(itemType ListItemType, isSearch bool) (string, bool) {
	switch itemType {
	case ItemTypePinnedCategory:
		return m.t("section.pinned_categories"), true
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
	case ItemTypeYouTubeOffer:
		return m.t("search.section_other_platform"), true
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
func (m Model) formatListItemRow(it ListItem, isSearch bool, maxWidth, tagWidth int, distinct bool) string {
	highlighted := it.ID() == m.list.highlightedID
	marker := listRowMarker(highlighted, it.Type == ItemTypeFavorite || it.Favorite)

	var row string
	switch it.Type {
	case ItemTypeMoreRecents:
		row = fitListRow(fmt.Sprintf("%s %7s  %s", marker, "", fmt.Sprintf(m.t("recents.more"), it.Hidden)), maxWidth)
	case ItemTypeFewerRecents:
		row = fitListRow(fmt.Sprintf("%s %7s  %s", marker, "", fmt.Sprintf(m.t("recents.fewer"), it.Hidden)), maxWidth)
	case ItemTypeYouTubeOffer:
		offer := fmt.Sprintf(m.t("search.youtube_offer"), sanitizeScreenText(it.Channel.Login))
		row = fitListRow(fmt.Sprintf("%s %7s  %s", marker, "", offer), maxWidth)
	case ItemTypeCategory, ItemTypePinnedCategory:
		if it.Type == ItemTypePinnedCategory && it.Category.Viewers == nil {
			tag := m.t("channels.unverified_tag")
			row = fitChannelRow(marker, fmt.Sprintf("%7s", m.t("categories.viewers_unknown")), sanitizeScreenText(it.Category.Name), tag, "", false, maxWidth, ansi.StringWidth(tag), distinct)
		} else {
			row = m.formatCategoryRow(marker, *it.Category, isSearch, maxWidth, distinct)
		}
	default: // ItemTypeChannel, ItemTypeExactChannel, ItemTypeFavorite, ItemTypeRecent
		row = m.formatChannelRow(marker, *it.Channel, it.Availability, maxWidth, distinct, tagWidth)
	}
	if !highlighted {
		return row
	}
	if maxWidth > 0 {
		row = padOrTruncateVisual(row, maxWidth)
	}
	// One run keeps nested SGR resets from breaking selection.
	if m.listVisible() && m.chatColumnVisible() && m.Focus() == FocusChat {
		return styleSelectedRowDim(ansi.Strip(row))
	}
	return styleSelectedRow(ansi.Strip(row))
}

// listRowBudget is how many rows fit once the used lines, the status bar, the
// loading line and the narrow-chat notice have their room. Never below one.
func (m Model) listRowBudget(used int, isSearch bool, maxWidth int) int {
	budget := m.height - 1 - m.footerRows() - used
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

// liveCatalogBelow diz se, na tela inicial, a primeira categoria ao vivo ficou
// abaixo da janela da lista: as fixadas, os favoritos e os recentes a
// empurraram para fora da tela. A janela é a mesma de renderListBodyLines.
func (m Model) liveCatalogBelow() bool {
	if m.Mode() != ModeCategories || m.height <= 0 || !m.listVisible() || m.list.hardErr() {
		return false
	}
	items := m.VisibleItems()
	first := firstLiveCategory(items)
	if first < 0 {
		return false
	}
	listW, _ := m.panelWidths()
	rows := m.listRowPositions(items, false, listW)
	used := len(m.appendFilterHint(m.appendPromptLines([]string{""}, listW), listW)) + len(m.catalogUnavailableLines(listW))
	_, end := m.list.rowWindow(rows, m.listRowBudget(used, false, listW))
	firstID := items[first].ID()
	return !slices.ContainsFunc(rows[:end], func(r listRow) bool { return !r.isHeader && r.itemID == firstID })
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
