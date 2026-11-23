package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// quickOpenState é o seletor rápido (Ctrl+K): um campo sobre a tela atual com
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

// quickOpenItem é uma linha do seletor: tipo, texto de busca, pontuação e o
// alvo (canal, categoria ou ação).
type quickOpenItem struct {
	Kind     quickOpenKind
	Label    string
	Platform string
	Score    int
	Channel  *catalog.Channel
	Category *catalog.Category
	Action   actionID
	KeyLabel string
}

// foldForFuzzy normaliza para comparação: minúsculas e sem acentos (NFD + tira Mn).
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

// fuzzyScore mede o casamento de query em text como subsequência. Sem casamento
// devolve -1. Prefixo e início de palavra valem mais; caracteres contíguos
// também. Implementação sem dependência nova.
func fuzzyScore(query, text string) int {
	q := foldForFuzzy(query)
	t := foldForFuzzy(text)
	if q == "" {
		return 0
	}
	if t == "" {
		return -1
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
		return -1
	}
	// Empate: textos mais curtos, que cobriram a query com menos sobra, sobem.
	score -= len(tRunes) - len(qRunes)
	return score
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
		return m.startSearch(q)
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
		m.list.enters++
		return m.applyChannelSelected(ChannelSelectedMsg{Channel: ch, Enter: m.list.enters})
	case quickCategory:
		if item.Category == nil {
			return m, nil
		}
		return m.openQuickCategory(*item.Category, item.Platform)
	case quickAction:
		return m.fireQuickAction(item.Action)
	}
	return m, nil
}

// openQuickCategory abre a categoria; se for de outra plataforma, troca o
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

// fireQuickAction executa uma ação do registro escolhida no seletor, pelo mesmo
// caminho das teclas (comando, layout, player, movimento e globais).
func (m Model) fireQuickAction(id actionID) (tea.Model, tea.Cmd) {
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
	case actBack:
		return m.keyClear()
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
	case actUp:
		return m.keyUp()
	case actDown:
		return m.keyDown()
	case actPageUp:
		return m.keyPageUp()
	case actPageDown:
		return m.keyPageDown()
	case actHome:
		return m.keyHome()
	case actEnd:
		return m.keyGoLive()
	case actSelect:
		return m.keySelect()
	case actSwitchFocus:
		return m.switchPanelFocus(), nil
	}
	return m, nil
}

// quickOpenItems monta e ordena os resultados: pontuação decrescente; empate
// por tipo (favorito, recente, categoria, ação) e ordem de origem.
func (m Model) quickOpenItems() []quickOpenItem {
	query := m.list.quickOpen.input
	var items []quickOpenItem

	favSeen := map[string]bool{}
	for _, ch := range m.list.favorites {
		label := channelQuickLabel(ch)
		score := fuzzyScore(query, label)
		if score < 0 {
			continue
		}
		c := ch
		key := channelQuickKey(ch)
		favSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickFavorite, Label: label, Platform: ch.CanonicalPlatform(),
			Score: score, Channel: &c,
		})
	}
	for _, ch := range m.list.recents {
		key := channelQuickKey(ch)
		if favSeen[key] {
			continue
		}
		label := channelQuickLabel(ch)
		score := fuzzyScore(query, label)
		if score < 0 {
			continue
		}
		c := ch
		items = append(items, quickOpenItem{
			Kind: quickRecent, Label: label, Platform: ch.CanonicalPlatform(),
			Score: score, Channel: &c,
		})
	}

	catSeen := map[string]bool{}
	listPlat := m.ListPlatform()
	for _, cat := range m.list.categories {
		label := cat.Name
		score := fuzzyScore(query, label)
		if score < 0 {
			continue
		}
		c := cat
		key := categoryQuickKey(listPlat, cat.ID)
		catSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickCategory, Label: label, Platform: listPlat,
			Score: score, Category: &c,
		})
	}
	for _, saved := range m.list.favoriteCategories {
		plat := platform.Normalize(saved.Platform)
		key := categoryQuickKey(plat, saved.ID)
		if catSeen[key] {
			continue
		}
		label := saved.Name
		score := fuzzyScore(query, label)
		if score < 0 {
			continue
		}
		c := catalog.Category{ID: saved.ID, Name: saved.Name}
		for _, fresh := range m.list.categories {
			if plat == listPlat && fresh.ID == saved.ID {
				c = fresh
				break
			}
		}
		catSeen[key] = true
		items = append(items, quickOpenItem{
			Kind: quickCategory, Label: label, Platform: plat,
			Score: score, Category: &c,
		})
	}

	for _, a := range m.keys.actions {
		if !quickOpenActionCandidate(a) {
			continue
		}
		keys := m.keys.keys[a.ID]
		if len(keys) == 0 {
			continue
		}
		label := m.t(a.text())
		score := fuzzyScore(query, label)
		if score < 0 {
			// Também casa o nome curto da ação (quick_open, mute…).
			short := strings.TrimPrefix(string(a.ID), "action.")
			score = fuzzyScore(query, short)
			if score < 0 {
				continue
			}
		}
		items = append(items, quickOpenItem{
			Kind: quickAction, Label: label, Score: score,
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
		return strings.Compare(foldForFuzzy(a.Label), foldForFuzzy(b.Label))
	})
	return items
}

func quickOpenActionCandidate(a action) bool {
	if a.ID == actQuickOpen || a.Section == sectionHidden {
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
	if name := strings.TrimSpace(ch.DisplayName); name != "" {
		return name
	}
	if login := strings.TrimSpace(ch.Login); login != "" {
		return login
	}
	return strings.TrimSpace(ch.ID)
}

func channelQuickKey(ch catalog.Channel) string {
	return ch.CanonicalPlatform() + "\n" + strings.ToLower(ch.Login) + "\n" + ch.ID
}

func categoryQuickKey(plat, id string) string {
	return plat + "\n" + id
}

// renderQuickOpenLines desenha o seletor como overlay, no estilo do menu de filtros.
func (m Model) renderQuickOpenLines(maxWidth int) []string {
	if !m.list.quickOpen.active {
		return nil
	}
	items := m.quickOpenItems()
	cursor := m.list.quickOpen.cursor
	if cursor >= len(items) {
		cursor = max(0, len(items)-1)
	}

	prompt := fmt.Sprintf(m.t("quick_open.prompt"), sanitizeScreenText(m.list.quickOpen.input))
	head := []string{
		TruncateVisual("=== "+m.t("quick_open.title")+" ===", maxWidth),
		TruncateVisual(prompt, maxWidth),
	}
	if len(items) == 0 {
		hint := m.t("quick_open.empty")
		if strings.TrimSpace(m.list.quickOpen.input) != "" {
			hint = m.t("quick_open.empty_search")
		}
		head = append(head, TruncateVisual("  "+hint, maxWidth))
		return head
	}

	rows := make([]string, 0, len(items))
	for i, it := range items {
		marker := "  "
		if i == cursor {
			marker = "▸ "
		}
		rows = append(rows, TruncateVisual(marker+m.quickOpenRowText(it), maxWidth))
	}

	available := m.height - 1 - m.footerRows()
	if m.height <= 0 || len(head)+len(rows) <= available {
		return append(head, rows...)
	}
	budget := available
	if m.height > 2 {
		budget--
	}
	pinned := min(len(head), (budget-1)/2)
	window := max(1, budget-pinned)
	start := max(min(cursor-window/2, len(rows)-window), 0)
	end := min(start+window, len(rows))
	out := make([]string, 0, available)
	out = append(out, head[:pinned]...)
	out = append(out, rows[start:end]...)
	if m.height > 2 {
		out = append(out, positionRow(cursor+1, len(items), maxWidth))
	}
	return out
}

func (m Model) quickOpenRowText(it quickOpenItem) string {
	typeLabel := m.t(quickOpenTypeKey(it.Kind))
	label := sanitizeScreenText(it.Label)
	line := typeLabel + " · " + label
	switch it.Kind {
	case quickAction:
		if it.KeyLabel != "" {
			return line + "  " + it.KeyLabel
		}
		return line
	default:
		plat := platform.DisplayName(it.Platform)
		if plat == "" {
			return line
		}
		return line + " [" + plat + "]"
	}
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
