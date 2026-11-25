package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// quickOpenMaxResults é o teto de linhas de resultado na caixa do seletor,
// antes de respeitar o espaço disponível do terminal.
const quickOpenMaxResults = 12

// quickOpenState é o seletor rápido Ctrl+K. Um campo sobre a tela atual com
// busca aproximada em favoritos, recentes, categorias carregadas e ações.
type quickOpenState struct {
	active bool
	input  string
	cursor int
}

// quickOpenKind classifica cada resultado do seletor.
type quickOpenKind int

const (
	quickFavorite quickOpenKind = iota
	quickRecent
	quickCategory
	quickAction
)

// quickOpenItem é uma linha do seletor com tipo, texto de busca, pontuação e o
// alvo canal, categoria ou ação.
type quickOpenItem struct {
	Kind        quickOpenKind
	Label       string
	FoldedLabel string
	Platform    string
	Score       int
	Channel     *catalog.Channel
	Category    *catalog.Category
	Action      actionID
	KeyLabel    string
}

// foldForFuzzy normaliza para comparação em minúsculas e sem acentos comuns.
// A implementação mapeia diacríticos latinos sem passar por NFD.
func foldForFuzzy(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == 'á' || r == 'à' || r == 'â' || r == 'ã' || r == 'ä':
			b.WriteRune('a')
		case r == 'é' || r == 'è' || r == 'ê' || r == 'ë':
			b.WriteRune('e')
		case r == 'í' || r == 'ì' || r == 'î' || r == 'ï':
			b.WriteRune('i')
		case r == 'ó' || r == 'ò' || r == 'ô' || r == 'õ' || r == 'ö':
			b.WriteRune('o')
		case r == 'ú' || r == 'ù' || r == 'û' || r == 'ü':
			b.WriteRune('u')
		case r == 'ç':
			b.WriteRune('c')
		case r == 'ñ':
			b.WriteRune('n')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fuzzyScore mede o casamento de query em text como subsequência. O bool diz se
// houve casamento. Prefixo e início de palavra valem mais, e caracteres
// contíguos também. A penalidade por comprimento nunca anula um casamento
// válido. queryFolded já deve estar normalizado com foldForFuzzy.
func fuzzyScore(queryFolded, text string) (int, bool) {
	q := queryFolded
	t := foldForFuzzy(text)
	if q == "" {
		return 0, true
	}
	if t == "" {
		return 0, false
	}

	qi := 0
	score := 0
	prevMatch := -2
	tRunes := []rune(t)
	qRunes := []rune(q)
	for ti, tr := range tRunes {
		if qi >= len(qRunes) {
			break
		}
		if tr != qRunes[qi] {
			continue
		}
		score += 10
		if ti == 0 {
			score += 40
		} else if isWordStart(tRunes, ti) {
			score += 25
		}
		if ti == prevMatch+1 {
			score += 15
		}
		prevMatch = ti
		qi++
	}
	if qi < len(qRunes) {
		return 0, false
	}
	// Empate: textos mais curtos sobem, sem deixar o score negativo.
	penalty := len(tRunes) - len(qRunes)
	if penalty > score {
		penalty = score
	}
	if penalty > 0 {
		score -= penalty
	}
	return score, true
}

func isWordStart(runes []rune, i int) bool {
	if i <= 0 {
		return true
	}
	prev := runes[i-1]
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}

// keyQuickOpen abre o seletor ou, se já estiver aberto, o fecha.
func (m Model) keyQuickOpen() (tea.Model, tea.Cmd) {
	if m.list.quickOpen.active {
		m.list.quickOpen = quickOpenState{}
		return m, nil
	}
	m.clearStaleStatusErrors()
	m.list.quickOpen = quickOpenState{active: true}
	return m, nil
}

func (m Model) closeQuickOpen() Model {
	m.list.quickOpen = quickOpenState{}
	return m
}

func (m Model) updateQuickOpenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if input, ok := editText(m.list.quickOpen.input, msg); ok {
		m.list.quickOpen.input = input
		m.list.quickOpen.cursor = 0
		return m, nil
	}
	if step := fieldArrow(msg); step != 0 {
		items := m.quickOpenItems()
		if len(items) == 0 {
			return m, nil
		}
		m.list.quickOpen.cursor = (m.list.quickOpen.cursor + step + len(items)) % len(items)
		return m, nil
	}
	switch m.keys.actionFor(modeQuickOpen, msg) {
	case actBack, actQuickOpen:
		return m.closeQuickOpen(), nil
	case actSelect:
		return m.confirmQuickOpen()
	}
	return m, nil
}

func (m Model) confirmQuickOpen() (tea.Model, tea.Cmd) {
	items := m.quickOpenItems()
	if len(items) == 0 {
		q := strings.TrimSpace(m.list.quickOpen.input)
		m = m.closeQuickOpen()
		if q == "" {
			return m, nil
		}
		return m.revealListForQuickOpen().startSearchClosingPreview(q)
	}
	if m.list.quickOpen.cursor < 0 || m.list.quickOpen.cursor >= len(items) {
		m.list.quickOpen.cursor = 0
	}
	item := items[m.list.quickOpen.cursor]
	m = m.closeQuickOpen()
	switch item.Kind {
	case quickFavorite, quickRecent:
		if item.Channel == nil {
			return m, nil
		}
		ch := *item.Channel
		m = m.revealListForQuickOpen()
		var cmds []tea.Cmd
		if m.previewOpen() {
			if stop := m.closePreview(); stop != nil {
				cmds = append(cmds, stop)
			}
		}
		m.list.enters++
		next, cmd := m.applyChannelSelected(ChannelSelectedMsg{Channel: ch, Enter: m.list.enters})
		m = next.(Model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Sequence(cmds...)
	case quickCategory:
		if item.Category == nil {
			return m, nil
		}
		return m.revealListForQuickOpen().openQuickCategoryClosingPreview(*item.Category, item.Platform)
	case quickAction:
		return m.fireQuickAction(item.Action)
	}
	return m, nil
}

// revealListForQuickOpen mostra a lista e leva o foco a ela, como keySaved,
// também no keymap classic.
func (m Model) revealListForQuickOpen() Model {
	m.layout.showList = true
	m.setFocus(FocusList)
	return m
}

func (m Model) startSearchClosingPreview(q string) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.previewOpen() {
		if stop := m.closePreview(); stop != nil {
			cmds = append(cmds, stop)
		}
	}
	next, cmd := m.startSearch(q)
	m = next.(Model)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Sequence(cmds...)
}

func (m Model) openQuickCategoryClosingPreview(cat catalog.Category, plat string) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.previewOpen() {
		if stop := m.closePreview(); stop != nil {
			cmds = append(cmds, stop)
		}
	}
	next, cmd := m.openQuickCategory(cat, plat)
	m = next.(Model)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Sequence(cmds...)
}

// openQuickCategory abre a categoria. Se for de outra plataforma, troca o
// catálogo antes de carregar os canais.
func (m Model) openQuickCategory(cat catalog.Category, plat string) (tea.Model, tea.Cmd) {
	plat = platform.Normalize(plat)
	if plat == "" {
		plat = m.ListPlatform()
	}
	var cmds []tea.Cmd
	if plat != m.ListPlatform() {
		c := m.catalogOf(plat)
		if c.browser == nil && c.searcher == nil {
			m.warnStatus(m.t("feedback.single_platform"))
			return m, nil
		}
		m.list.tracker.cancelActive()
		m.list.generation++
		m.applyPendingRecents()
		m.list.platform = c.platform
		m.list.recentsExpanded = false
		m.browser, m.searcher = c.browser, c.searcher
		m.list.search = searchState{}
		m.list.filtering = false
		m.list.filter = ""
		m.list.prefs.menuOpen = false
		m.list.categories = nil
		m.list.channels = nil
		m.list.selectedCategory = nil
		m.list.prefs.seenLanguages = nil
		m.list.nextCursor = ""
		m.list.hasMore = false
		m.list.loadFailed = false
		m.list.focusResults = false
		m.clearStaleStatusErrors()
		m.clearHighlight()
	}
	m.setFocus(FocusList)
	selected := cat
	if m.browser == nil {
		m.list.selectedCategory = &selected
		m.list.mode = ModeChannels
		m.list.channels = nil
		m.list.filter = ""
		m.list.filtering = false
		m.list.search.active = false
		m.clearHighlight()
		m.list.focusResults = true
		m.list.nextCursor = ""
		m.list.hasMore = false
		m.clearStaleStatusErrors()
		m.list.loading = false
		return m, tea.Batch(append(cmds, func() tea.Msg {
			return CategorySelectedMsg{Category: selected}
		})...)
	}
	m.list.tracker.cancelActive()
	m.list.generation++
	m.list.search = searchState{}
	m.list.selectedCategory = &selected
	m.list.mode = ModeChannels
	m.list.channels = nil
	m.list.filter = ""
	m.list.filtering = false
	m.list.search.active = false
	m.clearHighlight()
	m.list.focusResults = true
	m.list.nextCursor = ""
	m.list.hasMore = false
	m.clearStaleStatusErrors()
	m.list.loading = true
	cmds = append(cmds, m.fetchChannelsCmd(m.list.generation, selected, "", false))
	return m, tea.Batch(cmds...)
}

// fireQuickAction executa uma ação do registro escolhida no seletor pelo mesmo
// caminho das teclas de comando, layout, player e globais. As primitivas de
// movimento e confirmação não entram no seletor.
func (m Model) fireQuickAction(id actionID) (tea.Model, tea.Cmd) {
	if m.hiddenListAction(id) {
		m.warnStatus(m.t("layout.hidden_list_key"))
		return m, nil
	}
	switch id {
	case actCycleLayout, actToggleList, actToggleChat:
		if next, cmd, ok := m.handleLayoutAction(id, ""); ok {
			return next, cmd
		}
	case actWindowList, actChatOverlay, actSwapSides, actChatTaller, actChatShorter,
		actChatMoreOpaque, actChatLessOpaque, actListWider, actListNarrower,
		actChatWider, actChatNarrower:
		if next, cmd, ok := m.handleLayoutAction("", id); ok {
			return next, cmd
		}
		return m, nil
	case actHelp, actHelpAnywhere:
		m.toggleHelp()
		return m, nil
	case actCategories:
		return m.keyCategories()
	case actStopVideo:
		return m.keyStopVideo()
	case actReconnectChat:
		return m.keyReconnectChat()
	case actFavorite:
		return m.keyFavorite()
	case actFavorites:
		return m.keySaved(actFavorites)
	case actRecents:
		return m.keySaved(actRecents)
	case actForgetRecent:
		return m.forgetRecentHighlighted()
	case actClearRecents:
		return m.askClearRecents()
	case actChannelFilter:
		return m.keyChannelFilter()
	case actSwitchPlatform:
		return m.keySwitchPlatform()
	case actCompose:
		return m.enterCompose()
	case actCopyMessage:
		return m.keyCopyMessage()
	case actFilter:
		return m.keyFilter()
	case actSearch:
		return m.keySearch()
	case actRetry:
		return m.keyRetry()
	case actPause:
		return m.keyPause()
	case actVolumeUp:
		return m.adjustVolume(5)
	case actVolumeDown:
		return m.adjustVolume(-5)
	case actMute:
		return m.keyMute()
	case actSwitchFocus:
		return m.switchPanelFocus(), nil
	}
	return m, nil
}

// quickOpenItems monta e ordena os resultados por pontuação decrescente. Empate
// resolve por tipo favorito, recente, categoria, ação e, no fim, ordem alfabética
// do rótulo normalizado.
func (m Model) quickOpenItems() []quickOpenItem {
	queryFolded := foldForFuzzy(m.list.quickOpen.input)
	var items []quickOpenItem

	favSeen := map[string]bool{}
	for _, ch := range m.list.favorites {
		label := channelQuickLabel(ch)
		score, ok := bestChannelFuzzy(queryFolded, label, ch.Login)
		if !ok {
			continue
		}
		c := ch
		key := channelQuickKey(ch)
		favSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickFavorite, Label: label, FoldedLabel: foldForFuzzy(label),
			Platform: ch.CanonicalPlatform(), Score: score, Channel: &c,
		})
	}
	for _, ch := range m.listRecents() {
		key := channelQuickKey(ch)
		if favSeen[key] {
			continue
		}
		label := channelQuickLabel(ch)
		score, ok := bestChannelFuzzy(queryFolded, label, ch.Login)
		if !ok {
			continue
		}
		c := ch
		items = append(items, quickOpenItem{
			Kind: quickRecent, Label: label, FoldedLabel: foldForFuzzy(label),
			Platform: ch.CanonicalPlatform(), Score: score, Channel: &c,
		})
	}

	catSeen := map[string]bool{}
	listPlat := m.ListPlatform()
	catsByID := make(map[string]catalog.Category, len(m.list.categories))
	for _, cat := range m.list.categories {
		catsByID[cat.ID] = cat
		label := m.categoryLabel(cat)
		score, ok := fuzzyScore(queryFolded, label)
		if !ok {
			continue
		}
		c := cat
		key := categoryQuickKey(listPlat, cat.ID)
		catSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickCategory, Label: label, FoldedLabel: foldForFuzzy(label),
			Platform: listPlat, Score: score, Category: &c,
		})
	}
	for _, saved := range m.list.favoriteCategories {
		plat := platform.Normalize(saved.Platform)
		key := categoryQuickKey(plat, saved.ID)
		if catSeen[key] {
			continue
		}
		c := catalog.Category{ID: saved.ID, Name: saved.Name}
		if plat == listPlat {
			if fresh, found := catsByID[saved.ID]; found {
				c = fresh
			}
		}
		label := m.categoryLabel(c)
		score, ok := fuzzyScore(queryFolded, label)
		if !ok {
			continue
		}
		catSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickCategory, Label: label, FoldedLabel: foldForFuzzy(label),
			Platform: plat, Score: score, Category: &c,
		})
	}

	for _, a := range m.keys.actions {
		if !m.quickOpenActionCandidate(a) {
			continue
		}
		keys := m.keys.keys[a.ID]
		if len(keys) == 0 {
			continue
		}
		label := m.t(a.text())
		score, ok := fuzzyScore(queryFolded, label)
		if !ok {
			// Também casa o nome curto da ação, como quick_open ou mute.
			short := strings.TrimPrefix(string(a.ID), "action.")
			score, ok = fuzzyScore(queryFolded, short)
			if !ok {
				continue
			}
		}
		items = append(items, quickOpenItem{
			Kind: quickAction, Label: label, FoldedLabel: foldForFuzzy(label), Score: score,
			Action: a.ID, KeyLabel: keyLabel(keys[0], m.catalog),
		})
	}

	slices.SortStableFunc(items, func(a, b quickOpenItem) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		if c := cmp.Compare(int(a.Kind), int(b.Kind)); c != 0 {
			return c
		}
		return strings.Compare(a.FoldedLabel, b.FoldedLabel)
	})
	return items
}

func bestChannelFuzzy(queryFolded, label, login string) (int, bool) {
	score, ok := fuzzyScore(queryFolded, label)
	if login == "" || strings.EqualFold(strings.TrimSpace(login), strings.TrimSpace(label)) {
		return score, ok
	}
	loginScore, loginOK := fuzzyScore(queryFolded, login)
	if !ok && !loginOK {
		return 0, false
	}
	if !ok {
		return loginScore, true
	}
	if !loginOK {
		return score, true
	}
	if loginScore > score {
		return loginScore, true
	}
	return score, true
}

func (m Model) quickOpenActionCandidate(a action) bool {
	if a.ID == actQuickOpen || a.Section == sectionHidden {
		return false
	}
	if a.windowOnly() && m.hosted.host == nil {
		return false
	}
	if a.ID == actSwitchPlatform && len(m.catalogs) < 2 {
		return false
	}
	switch a.ID {
	case actUp, actDown, actPageUp, actPageDown, actHome, actEnd, actSelect, actBack:
		return false
	}
	for _, mode := range a.Modes {
		if mode == modeNormal || mode == modeGlobal || mode == modeWindow {
			return true
		}
	}
	return false
}

func channelQuickLabel(ch catalog.Channel) string {
	if name := visibleOrEmpty(sanitizeScreenText(ch.DisplayName)); name != "" {
		return name
	}
	if login := visibleOrEmpty(sanitizeScreenText(ch.Login)); login != "" {
		return login
	}
	if id := strings.TrimSpace(ch.ID); id != "" {
		return id
	}
	return ""
}

func channelQuickKey(ch catalog.Channel) string {
	return ch.CanonicalPlatform() + "\n" + strings.ToLower(ch.Login) + "\n" + ch.ID
}

func categoryQuickKey(plat, id string) string {
	return plat + "\n" + id
}

// renderQuickOpenLines desenha o seletor como caixa de altura fixa ancorada no
// topo da área disponível. Reserva até quickOpenMaxResults linhas de resultado
// para a caixa não mudar de posição ao digitar.
func (m Model) renderQuickOpenLines(maxWidth int) []string {
	if !m.list.quickOpen.active {
		return nil
	}
	available := m.height - 1 - m.footerRows()
	if m.height <= 0 {
		available = quickOpenMaxResults + 3
	}

	items := m.quickOpenItems()
	cursor := m.list.quickOpen.cursor
	if cursor >= len(items) {
		cursor = max(0, len(items)-1)
	}

	title := TruncateVisual("=== "+m.t("quick_open.title")+" ===", maxWidth)
	prompt := TruncateVisual(fmt.Sprintf(m.t("quick_open.prompt"), sanitizeScreenText(m.list.quickOpen.input)), maxWidth)

	if len(items) == 0 {
		hint := m.t("quick_open.empty")
		if strings.TrimSpace(m.list.quickOpen.input) != "" {
			hint = m.t("quick_open.empty_search")
		}
		out := []string{title, prompt, TruncateVisual("  "+hint, maxWidth)}
		if available > 0 && len(out) > available {
			return out[:available]
		}
		return out
	}

	showPos := available > 2
	chrome := 2
	if showPos {
		chrome++
	}
	resultSlots := min(quickOpenMaxResults, max(0, available-chrome))
	if available <= 0 {
		resultSlots = quickOpenMaxResults
	}

	window := max(resultSlots, 1)
	start := max(min(cursor-window/2, len(items)-window), 0)
	end := min(start+window, len(items))

	rows := make([]string, 0, resultSlots)
	for i := start; i < end; i++ {
		marker := "  "
		if i == cursor {
			marker = "▸ "
		}
		rows = append(rows, m.formatQuickOpenRow(items[i], marker, maxWidth))
	}
	for len(rows) < resultSlots {
		rows = append(rows, "")
	}

	out := make([]string, 0, chrome+resultSlots)
	out = append(out, title, prompt)
	out = append(out, rows...)
	if showPos {
		out = append(out, positionRow(cursor+1, len(items), maxWidth))
	}
	if available > 0 && len(out) > available {
		out = out[:available]
	}
	return out
}

// formatQuickOpenRow corta o rótulo antes do sufixo da tecla ou da plataforma,
// para o corte cair no texto e nunca na tecla efetiva.
func (m Model) formatQuickOpenRow(it quickOpenItem, marker string, maxWidth int) string {
	typeLabel := m.t(quickOpenTypeKey(it.Kind))
	label := sanitizeScreenText(it.Label)
	body := typeLabel + " · " + label
	suffix := ""
	switch it.Kind {
	case quickAction:
		if it.KeyLabel != "" {
			suffix = "  " + it.KeyLabel
		}
	default:
		if plat := platform.DisplayName(it.Platform); plat != "" {
			suffix = " [" + plat + "]"
		}
	}
	room := maxWidth - ansi.StringWidth(marker) - ansi.StringWidth(suffix)
	if room < 1 {
		room = 1
	}
	return marker + TruncateVisual(body, room) + suffix
}

func quickOpenTypeKey(k quickOpenKind) string {
	switch k {
	case quickFavorite:
		return "quick_open.type.favorite"
	case quickRecent:
		return "quick_open.type.recent"
	case quickCategory:
		return "quick_open.type.category"
	default:
		return "quick_open.type.action"
	}
}
