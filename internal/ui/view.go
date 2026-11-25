package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/rivo/uniseg"
)

const (
	// platformMarkWidth limita a marca de plataforma de um salvo sem catálogo.
	platformMarkWidth = 16
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

// TruncateMiddleVisual corta s no meio para caber em maxWidth células. Mantém o
// começo e o fim e põe "…" entre eles. Serve para nomes que, cortados só no fim,
// ficariam iguais na lista.
func TruncateMiddleVisual(s string, maxWidth int) string {
	return truncateMiddleVisual(s, maxWidth, "…")
}

type graphemeCell struct {
	text  string
	width int
}

func graphemeCells(s string) []graphemeCell {
	out := make([]graphemeCell, 0, len(s))
	state := -1
	rest := s
	for len(rest) > 0 {
		cluster, next, _, nextState := uniseg.FirstGraphemeClusterInString(rest, state)
		out = append(out, graphemeCell{text: cluster, width: ansi.StringWidth(cluster)})
		rest = next
		state = nextState
	}
	return out
}

func truncateMiddleVisual(s string, maxWidth int, ell string) string {
	s = normalizeScreenText(s)
	if maxWidth <= 0 {
		return s
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	ellW := ansi.StringWidth(ell)
	// Sem espaço para a reticência, devolve só o começo do nome. Nunca string vazia
	// quando ainda há orçamento de células.
	if maxWidth <= ellW {
		return truncateToBudgetNonEmpty(s, maxWidth)
	}
	room := maxWidth - ellW
	cells := graphemeCells(s)
	if len(cells) == 0 {
		return truncateToBudgetNonEmpty(s, maxWidth)
	}

	// Cerca de 2/3 do espaço no começo e 1/3 no fim, para o sufixo que distingue
	// títulos com o mesmo prefixo continuar legível.
	endBudget := room / 3
	if endBudget < 1 {
		endBudget = 1
	}
	headBudget := room - endBudget
	if headBudget < 1 {
		headBudget = 1
		endBudget = room - headBudget
	}

	wide := false
	for _, c := range cells {
		if c.width >= 2 {
			wide = true
			break
		}
	}
	// Em larguras 4 a 7 um CJK não cabe no terço final de 1 célula. A sobra vai
	// para o fim enquanto ainda couber um grafema no começo e outro no fim.
	if wide && endBudget < 2 && room >= 4 {
		endBudget = 2
		headBudget = room - endBudget
	}
	if wide && headBudget < 2 && room >= 4 {
		headBudget = 2
		endBudget = room - headBudget
	}

	endStart := len(cells)
	endW := 0
	for endStart > 0 && endW < endBudget {
		cw := cells[endStart-1].width
		if endW+cw > endBudget {
			break
		}
		endStart--
		endW += cw
	}
	headEnd := 0
	headW := 0
	for headEnd < endStart && headW < headBudget {
		cw := cells[headEnd].width
		if headW+cw > headBudget {
			break
		}
		headW += cw
		headEnd++
	}

	// Se o fim ficou vazio e ainda cabem um grafema no começo e outro no fim,
	// cede espaço do começo para o último cluster.
	if endStart == len(cells) && len(cells) >= 2 {
		last := cells[len(cells)-1]
		if last.width <= room && cells[0].width+last.width <= room {
			endStart = len(cells) - 1
			endW = last.width
			remain := room - endW
			headEnd = 0
			headW = 0
			for headEnd < endStart && headW+cells[headEnd].width <= remain {
				headW += cells[headEnd].width
				headEnd++
			}
		}
	}
	if headEnd == 0 && endStart > 0 && cells[0].width <= room {
		headEnd = 1
		headW = cells[0].width
		remain := room - headW
		endStart = len(cells)
		endW = 0
		for endStart > headEnd && endW+cells[endStart-1].width <= remain {
			endStart--
			endW += cells[endStart].width
		}
	}

	var b strings.Builder
	b.Grow(maxWidth * 4)
	for i := 0; i < headEnd; i++ {
		b.WriteString(cells[i].text)
	}
	b.WriteString(ell)
	for i := endStart; i < len(cells); i++ {
		b.WriteString(cells[i].text)
	}
	out := b.String()
	if out == ell || out == "" || ansi.StringWidth(out) > maxWidth {
		return truncateToBudgetNonEmpty(s, maxWidth)
	}
	return out
}

// truncateToBudgetNonEmpty corta no fim sem reticência e evita nome vazio quando
// o primeiro grafema é mais largo que o orçamento.
func truncateToBudgetNonEmpty(s string, budget int) string {
	if budget <= 0 {
		return ""
	}
	out := ansi.Truncate(s, budget, "")
	if out != "" {
		return out
	}
	return ansi.Truncate("…", budget, "")
}

// listRowPrefixCells é a largura fixa do marcador (2) + espaço + espectadores (7)
// + dois espaços antes do nome.
const listRowPrefixCells = 12

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
	wide := hasWideRune(s) && !exactCellWidths
	if wide && maxWidth > 1 {
		maxWidth--
	}
	if !wide {
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
	if m.hosted.host != nil && m.hosted.chatOverlay {
		return m.listVisible() && m.width > 0 && m.width < hostedMinList+hostedSepWidth+hostedMinChat+hostedMinVideo
	}
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
func (m Model) View() string {
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
	if m.footerRows() > 0 {
		status = m.Footer() + "\n" + status
	}

	// Help is modal, so it wins over the filter menu it may have opened on.
	overlayLines := m.helpViewLines()
	if m.keymapNotice != "" {
		overlayLines = strings.Split(ansi.Wrap(sanitizeScreenText(m.keymapNotice), max(20, min(m.width-4, 76)), ""), "\n")
	}
	if m.prefixVisible {
		overlayLines = m.prefixLines()
	}
	if len(overlayLines) == 0 {
		overlayLines = m.renderQuickOpenLines(m.width)
	}
	if len(overlayLines) == 0 {
		overlayLines = m.renderChannelFilterMenuLines(m.width)
	}

	if m.hosted.host != nil && m.width > 0 && m.height > 1 {
		return m.hostedView(m.height-1-m.footerRows(), overlayLines, status)
	}
	if m.width <= 0 && m.height <= 0 {
		return m.unsizedFrame(overlayLines, status)
	}

	available := m.height - 1 - m.footerRows()
	if m.height <= 0 {
		available = 20
	}

	listW, chatW := m.panelWidths()
	var rows []string
	switch {
	case listW > 0 && chatW > 0:
		rows = m.splitRows(listW, chatW, available, overlayLines)
	case chatW > 0:
		rows = m.fitRows(m.renderChatLines(available, chatW), overlayLines, chatW, available)
	case listW == 0:
		rows = m.focusRows(available, overlayLines)
	default:
		rows = m.listOnlyRows(listW, available, overlayLines)
	}
	return strings.Join(rows, "\n") + "\n" + status
}

// unsizedFrame draws without a terminal size, as unit tests that send no
// WindowSizeMsg see it: every panel in full, one under the other.
func (m Model) unsizedFrame(overlayLines []string, status string) string {
	var bodyLines []string
	if m.listVisible() {
		bodyLines = m.renderBodyLines(0)
	}
	if m.layout.showChat {
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

// fitRows turns the lines of a single panel into the rows above the status
// bar: exactly available of them, each width cells wide, with the overlay over
// the last ones. Without a height nothing is cut and the overlay goes below.
func (m Model) fitRows(lines, overlayLines []string, width, available int) []string {
	if m.height <= 0 {
		return padRows(append(lines, overlayLines...), width)
	}
	for len(lines) < available {
		lines = append(lines, "")
	}
	if len(lines) > available {
		lines = lines[:available]
	}
	return m.overlayHelpLines(padRows(lines, width), overlayLines, width, available)
}

// padRows pads or truncates every row to width, in place. A width of zero
// leaves the rows as they are.
func padRows(rows []string, width int) []string {
	for i := range rows {
		rows[i] = fitScreenLines(padOrTruncateVisual(rows[i], width), width)
	}
	return rows
}

// splitRows draws the list and the chat side by side.
func (m Model) splitRows(listW, chatW, available int, overlayLines []string) []string {
	chatLines := m.renderChatLines(available, chatW)
	listLines := m.renderBodyLines(listW)
	rows := available
	if m.height <= 0 {
		rows = max(len(listLines), len(chatLines))
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
		// Each panel is cut to its own width first; the wide-rune guard then only
		// trims trailing space from the composed row, so the separator keeps its
		// column and the chat text is never cut for an emoji in the list.
		row := padOrTruncateVisual(left, listW) + panelSeparator + padOrTruncateVisual(right, chatW)
		combined = append(combined, fitScreenLines(row, listW+ansi.StringWidth(panelSeparator)+chatW))
	}
	if m.height <= 0 {
		return padRows(append(combined, overlayLines...), m.width)
	}
	return m.overlayHelpLines(combined, overlayLines, m.width, available)
}

// focusRows draws focus mode: both panels hidden, only a short hint above the
// status bar.
func (m Model) focusRows(available int, overlayLines []string) []string {
	hint := m.focusModeHintLines(m.width)
	if m.height <= 0 {
		// The hint is already wrapped to the width, and nothing pads it here.
		return append(hint, overlayLines...)
	}
	return m.fitRows(hint, overlayLines, m.width, available)
}

// listOnlyRows draws the list alone, with the narrow-chat notice under it when the
// width is what hides the chat.
func (m Model) listOnlyRows(listW, available int, overlayLines []string) []string {
	bodyLines := m.renderBodyLines(listW)
	var noticeLines []string
	if m.shouldShowNarrowChatNotice() {
		noticeLines = m.narrowChatNoticeLines(listW)
	}
	if m.height <= 0 {
		return m.fitRows(append(bodyLines, noticeLines...), overlayLines, listW, available)
	}

	// The notice takes rows from the list, but the list keeps at least one row.
	listBudget := max(available-len(noticeLines), 1)
	if len(bodyLines) > listBudget {
		bodyLines = bodyLines[:listBudget]
	}
	for _, line := range noticeLines {
		if len(bodyLines) >= available {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return m.fitRows(bodyLines, overlayLines, listW, available)
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
			out[i] = fitScreenLines(padOrTruncateVisual(helpLines[i], maxWidth), maxWidth)
		}
		return out
	}
	// O seletor rápido fica ancorado no topo da área; os outros overlays, no
	// fim, inclusive a ajuda, o aviso e o prefixo abertos sobre o seletor.
	if m.quickOpenOverlayShown() {
		for i, hl := range helpLines {
			lines[i] = fitScreenLines(padOrTruncateVisual(hl, maxWidth), maxWidth)
		}
		if end := len(helpLines); end < len(lines) {
			lines[end] = padOrTruncateVisual("", maxWidth)
		}
		return lines
	}
	start := len(lines) - len(helpLines)
	if start > 0 {
		lines[start-1] = padOrTruncateVisual("", maxWidth)
	}
	for i, hl := range helpLines {
		lines[start+i] = fitScreenLines(padOrTruncateVisual(hl, maxWidth), maxWidth)
	}
	return lines
}

func (m Model) headerLine(maxWidth int) string {
	var title string
	switch m.Mode() {
	case ModeChannels:
		if m.list.selectedCategory != nil {
			title = fmt.Sprintf(m.t("channels.title_category"), m.categoryLabel(*m.list.selectedCategory))
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
	if name := m.listPlatformName(); name != "" {
		// The platform leads, so a long category name is what gets cut.
		title = name + statusSep + title
	}

	if m.chatColumnVisible() && m.listVisible() {
		if m.Focus() == FocusList {
			return withFocusTag("▸ "+title, m.t("focus.active_tag"), maxWidth)
		}
		return "  " + title
	}
	return title
}

// withFocusTag adds the focus tag to a panel title only when it fits whole: a
// cut "[fo…" says nothing, and the ▸ and the focus bar already mark the panel.
// Cutting the title instead would hide the category or room name.
func withFocusTag(head, tag string, maxWidth int) string {
	if maxWidth > 0 && ansi.StringWidth(head)+1+ansi.StringWidth(tag) > maxWidth {
		return head
	}
	return head + " " + tag
}

// padToWidth fills s with spaces up to width, so a style spans the column.
func padToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	return padOrTruncateVisual(s, width)
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
	header := fitListRow(m.headerLine(maxWidth), maxWidth)
	if m.chatColumnVisible() && m.listVisible() && m.Focus() == FocusList {
		header = styleFocusBar(padToWidth(header, maxWidth))
	}

	if m.list.hardErr() {
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

// channelStatusTag returns the availability tag shown on the right of a row.
func (m Model) channelStatusTag(ch catalog.Channel, avail catalog.Availability) string {
	switch avail {
	case catalog.AvailabilityOffline:
		return styleOffline(m.t("channels.offline_tag"))
	case catalog.AvailabilityUnknown:
		return styleUnverified(m.t("channels.unverified_tag"))
	}
	return ""
}

// langUnknownTagFits diz se o nome inteiro ainda cabe na linha junto do marcador
// [idioma?] e da tag de disponibilidade. maxWidth <= 0, frame sem tamanho,
// mostra o marcador sempre. A decisão é por linha, pelo nome daquela linha.
func langUnknownTagFits(maxWidth, statusTagWidth, nameWidth int, langTag string) bool {
	if maxWidth <= 0 {
		return true
	}
	langW := ansi.StringWidth(normalizeScreenText(langTag))
	tagW := langW
	if statusTagWidth > 0 {
		tagW = statusTagWidth + 1 + langW
	}
	needed := listRowPrefixCells + nameWidth
	if tagW > 0 {
		needed += 1 + tagW
	}
	return needed <= maxWidth
}

// channelRowTag keeps availability and unknown-language evidence together.
// O marcador de idioma some só na linha em que o nome inteiro não cabe junto
// dele. Offline e não verificado continuam reservando coluna.
func (m Model) channelRowTag(ch catalog.Channel, avail catalog.Availability, maxWidth int) string {
	lang := ""
	if len(m.list.prefs.languages) > 0 {
		lang = m.t("channels.lang_unknown_tag")
	}
	return m.channelRowTagSized(ch, avail, maxWidth, lang)
}

// channelRowTagSized is channelRowTag with the language marker already resolved,
// so listTagWidth can compute it once outside its loop.
func (m Model) channelRowTagSized(ch catalog.Channel, avail catalog.Availability, maxWidth int, langTag string) string {
	status := m.channelStatusTag(ch, avail)
	if langTag == "" || !channelLanguageUnknown(ch) {
		return status
	}
	nameW := ansi.StringWidth(m.channelIdentity(ch))
	statusW := ansi.StringWidth(normalizeScreenText(status))
	if !langUnknownTagFits(maxWidth, statusW, nameW, langTag) {
		return status
	}
	if status == "" {
		return langTag
	}
	return status + " " + langTag
}

// channelShortLabel is the channel name for notices: display name, or login, or
// "(unnamed)" when both show nothing on screen.
func (m Model) channelShortLabel(ch catalog.Channel) string {
	if name := visibleOrEmpty(sanitizeScreenText(ch.DisplayName)); name != "" {
		return name
	}
	if name := visibleOrEmpty(sanitizeScreenText(ch.Login)); name != "" {
		return name
	}
	return m.t("list.unnamed")
}

// channelIdentity is the name side of a channel row: display name, optional
// login and optional platform mark for a saved channel without a catalog.
func (m Model) channelIdentity(ch catalog.Channel) string {
	displayName := visibleOrEmpty(sanitizeScreenText(ch.DisplayName))
	login := visibleOrEmpty(sanitizeScreenText(ch.Login))
	identity := displayName
	if identity == "" {
		identity = login
	} else if login != "" && !strings.EqualFold(displayName, login) {
		identity += " (" + login + ")"
	}
	if identity == "" {
		identity = m.t("list.unnamed")
	}
	c := m.catalogOf(ch.CanonicalPlatform())
	if c.browser == nil && c.searcher == nil && ch.CanonicalPlatform() != m.ListPlatform() {
		// O teto deixa espaço para o nome mesmo com host longo em 80 colunas.
		mark := TruncateVisual(sanitizeScreenText(platform.DisplayName(ch.CanonicalPlatform())), platformMarkWidth)
		identity = "[" + mark + "] " + identity
	}
	return identity
}

func (m Model) formatChannelRow(marker string, ch catalog.Channel, avail catalog.Availability, maxWidth int, distinct bool, tagWidthAndBudget ...int) string {
	identity := m.channelIdentity(ch)
	title := ""
	if ch.Title != nil {
		title = visibleOrEmpty(sanitizeScreenText(strings.TrimSpace(*ch.Title)))
	}
	viewers := fmt.Sprintf("%7s", m.t("channels.viewers_unknown"))
	if avail == catalog.AvailabilityLive {
		if ch.Viewers == nil {
			viewers = styleLive(padOrTruncateVisual(m.t("channels.live_compact"), 7))
		} else {
			viewers = styleLive(formatLiveViewersCell(m.formatViewers(ch.Viewers)))
		}
	}
	tag := m.channelRowTag(ch, avail, maxWidth)
	tagWidth := ansi.StringWidth(tag)
	nameBudget := 0
	if len(tagWidthAndBudget) > 0 {
		tagWidth = max(tagWidth, tagWidthAndBudget[0])
	}
	if len(tagWidthAndBudget) > 1 {
		nameBudget = tagWidthAndBudget[1]
	}
	return fitChannelRow(marker, viewers, identity, tag, title, m.player.isPlaying(ch), maxWidth, tagWidth, distinct, nameBudget)
}

// formatLiveViewersCell reserves one space after the live dot at every magnitude.
func formatLiveViewersCell(viewers string) string {
	return "●" + " " + fmt.Sprintf("%5s", viewers)
}

// fitChannelRow reserves a shared, left-aligned status column at the right edge.
// The identity takes priority over the title when the panel is narrow. Uma
// linha sem marca própria usa a largura inteira. A coluna das marcas fica vazia
// nela e não corta o nome. Com distinct, a identidade que colidiria com outra
// linha é cortada no meio para mostrar o fim do nome, e o título cede espaço
// para esse sufixo permanecer.
func fitChannelRow(marker, viewers, identity, tag, title string, playing bool, maxWidth, tagWidth int, distinct bool, nameBudget int) string {
	prefix := marker + " " + viewers + "  "
	identity = normalizeScreenText(identity)
	title = normalizeScreenText(title)
	tag = normalizeScreenText(tag)
	if tag == "" {
		tagWidth = 0
	}
	bodyWidth := maxWidth
	if maxWidth > 0 && tagWidth > 0 {
		bodyWidth = max(1, maxWidth-tagWidth-1)
	}
	if nameBudget <= 0 {
		nameBudget = listLabelBudget(bodyWidth, listRowPrefixCells, identity)
	}
	if distinct {
		identity = distinctListLabel(identity, nameBudget)
	}
	row := prefix + identity
	if title != "" {
		sep := " " + listRowEmDash + " "
		if distinct {
			// O título cede células. O sufixo da identidade distinto permanece.
			title = fitChannelTitle(identity, title, sep, bodyWidth)
			if title != "" {
				if hasWideRune(identity+title) && !exactCellWidths {
					sep = " - "
				}
				row += sep + title
			}
		} else {
			row += sep + title
		}
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

// fitChannelTitle corta o título para caber depois da identidade já distinta,
// na mesma largura que fitListRow usaria no corpo da linha.
func fitChannelTitle(identity, title, sep string, bodyWidth int) string {
	if bodyWidth <= 0 {
		return title
	}
	fitW := bodyWidth
	wide := (hasWideRune(identity) || hasWideRune(title)) && !exactCellWidths
	if wide && fitW > 1 {
		fitW--
		sep = strings.ReplaceAll(sep, listRowEmDash, "-")
		title = strings.ReplaceAll(title, listRowEmDash, "-")
	}
	used := listRowPrefixCells + ansi.StringWidth(identity) + ansi.StringWidth(normalizeScreenText(sep))
	remain := fitW - used
	if remain < 1 {
		return ""
	}
	if ansi.StringWidth(title) <= remain {
		return title
	}
	ell := "…"
	if wide {
		ell = "..."
	}
	if remain <= ansi.StringWidth(ell) {
		return ansi.Truncate(title, remain, "")
	}
	return ansi.Truncate(title, remain, ell)
}

// categoryLabel is the category name as the list shows it. A name that shows
// nothing, like one made only of U+3164, becomes "(unnamed)".
func (m Model) categoryLabel(cat catalog.Category) string {
	if name := visibleOrEmpty(sanitizeScreenText(cat.Name)); name != "" {
		return name
	}
	return m.t("list.unnamed")
}

// visibleOrEmpty returns s, or "" when s is only spaces.
func visibleOrEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

func (m Model) formatCategoryRow(marker string, cat catalog.Category, inSearch bool, maxWidth int, distinct bool, nameBudget int) string {
	name := m.categoryLabel(cat)
	if inSearch && categoryNoBroadcast(cat) {
		viewers := fmt.Sprintf("%7s", m.t("categories.viewers_unknown"))
		prefix := marker + " " + viewers + "  "
		suffix := " " + listRowEmDash + " " + m.t("categories.no_stream")
		if distinct {
			budget := nameBudget
			if budget <= 0 {
				budget = listLabelBudget(maxWidth, listRowPrefixCells, name)
				if maxWidth > 0 {
					budget = max(budget-ansi.StringWidth(suffix), 1)
				}
			}
			name = distinctListLabel(name, budget)
		}
		return fitListRow(prefix+name+suffix, maxWidth)
	}
	viewers := m.formatViewers(cat.Viewers)
	if cat.Viewers != nil && *cat.Viewers > 0 {
		viewers = styleLive(fmt.Sprintf("%7s", viewers))
	} else {
		viewers = fmt.Sprintf("%7s", viewers)
	}
	prefix := marker + " " + viewers + "  "
	if distinct {
		budget := nameBudget
		if budget <= 0 {
			budget = listLabelBudget(maxWidth, listRowPrefixCells, name)
		}
		name = distinctListLabel(name, budget)
	}
	return fitListRow(prefix+name, maxWidth)
}

// listLabelBudget is how many cells the name may use before the row is cut.
// prefixWidth is the already measured lead, so callers avoid strings.Repeat.
func listLabelBudget(maxWidth, prefixWidth int, label string) int {
	if maxWidth <= 0 {
		return ansi.StringWidth(normalizeScreenText(label))
	}
	budget := maxWidth - prefixWidth
	if hasWideRune(label) && !exactCellWidths {
		budget--
	}
	return max(budget, 1)
}

// distinctListLabel corta o nome no meio com a mesma reticência que fitListRow
// usaria nessa linha, Unicode ou ASCII em runas largas.
func distinctListLabel(label string, budget int) string {
	label = normalizeScreenText(label)
	if budget <= 0 || ansi.StringWidth(label) <= budget {
		return label
	}
	ell := "…"
	if hasWideRune(label) && !exactCellWidths {
		ell = "..."
		label = strings.ReplaceAll(label, listRowEmDash, "-")
	}
	return truncateMiddleVisual(label, budget, ell)
}

// endTruncatedListLabel é o nome como fitListRow o deixaria ao cortar só o fim.
// A detecção de colisão compara esse resultado entre as linhas visíveis.
func endTruncatedListLabel(label string, budget int) string {
	label = normalizeScreenText(label)
	if budget <= 0 || ansi.StringWidth(label) <= budget {
		return label
	}
	if hasWideRune(label) && !exactCellWidths {
		label = strings.ReplaceAll(label, listRowEmDash, "-")
		ell := "..."
		if budget <= ansi.StringWidth(ell) {
			return truncateToBudgetNonEmpty(label, budget)
		}
		out := ansi.Truncate(label, budget, ell)
		if out == "" {
			return truncateToBudgetNonEmpty(label, budget)
		}
		return out
	}
	if budget <= ansi.StringWidth("…") {
		return truncateToBudgetNonEmpty(label, budget)
	}
	return TruncateVisual(label, budget)
}

func categoryNoBroadcast(cat catalog.Category) bool {
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

// quickOpenOverlayShown diz se o overlay desenhado agora é o seletor rápido, e
// não a ajuda, o aviso de teclas ou o prefixo, que têm prioridade em frame.
func (m Model) quickOpenOverlayShown() bool {
	return m.list.quickOpen.active && !m.helpOpen() && m.keymapNotice == "" && !m.prefixVisible
}
