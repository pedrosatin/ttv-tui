package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A ajuda sai do registro de ações (actions.go) e das primitivas de edição
// (field_keys.go): cada ação dá uma linha na seção dela e uma em cada uso de
// Also, com as teclas efetivas. A tela tem o título fixo no topo, as seções
// rolando embaixo dele e, quando não cabe tudo, uma linha que diz em que seção e
// em que linha se está.

// helpContextOrder é a ordem das situações dentro de uma seção. As ações só da
// janela vêm logo depois das gerais: dentro dela, onde não levam marca, elas
// continuam as linhas sem título que as antecedem.
var helpContextOrder = []helpContext{
	ctxAny, ctxWindowOnly, ctxHelp, ctxSearchPrompt, ctxFilter, ctxMenu, ctxChatPanel, ctxCompose, ctxField,
}

// helpEntry é uma linha da ajuda antes de quebrar na largura.
type helpEntry struct {
	use helpUse
	// fieldKeys e actionKeys são os rótulos das teclas. Os das primitivas vêm
	// antes, para ↑/↓ preceder PgUp e PgDn no filtro.
	fieldKeys, actionKeys []string
	text                  string
	// also marca a linha que nasceu de um uso de Also: ela vem depois das
	// linhas principais da mesma situação.
	also bool
	// legend pinta os termos de cor da legenda.
	legend bool
}

func (e helpEntry) keys() string {
	return strings.Join(append(slices.Clone(e.fieldKeys), e.actionKeys...), " ")
}

// helpDoc é a ajuda inteira numa largura: o título, o corpo e a linha do corpo
// onde começa cada seção, na ordem de helpSections.
type helpDoc struct {
	title  []string
	body   []string
	starts []int
}

// helpPage diz como a ajuda ocupa a altura: quantas linhas do título ficam
// fixas, quantas do corpo aparecem e se há a linha de posição.
type helpPage struct {
	sticky, rows int
	indicator    bool
}

// helpOpen reports whether the help is on screen and owns the keyboard. A
// one-row terminal draws only the status bar, and a help nobody can see must
// not eat keys.
func (m Model) helpOpen() bool {
	return m.layout.helpVisible && m.height != 1
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

// focusHelpSection é a seção em que a ajuda abre: a do que o foco está usando.
// Com o player em erro, a barra oferece a ajuda para os detalhes, e ela abre nas
// configurações, onde fica o último erro, salvo com o foco num campo de texto.
func (m Model) focusHelpSection() helpSection {
	switch {
	case m.list.prefs.menuOpen, m.listVisible() && (m.list.search.active || m.list.filtering):
		return sectionSearch
	case m.chat.composing && m.Focus() == FocusChat:
		return sectionChat
	case m.player.state == PlayerStateError && m.player.errorLine() != "":
		return sectionSettings
	case m.Focus() == FocusChat:
		return sectionChat
	}
	return sectionNav
}

// toggleHelp opens the help at the section of the focus, or closes it.
func (m *Model) toggleHelp() {
	m.layout.helpVisible = !m.layout.helpVisible
	m.layout.helpSection = m.focusHelpSection()
	m.layout.helpScroll = 0
}

// helpEntries são as linhas da ajuda, na ordem do registro, com as linhas de
// mesmo texto na mesma seção e situação juntas numa só.
func (m Model) helpEntries() []helpEntry {
	var out []helpEntry
	at := map[helpUse]int{}
	add := func(use helpUse, label string, field, also bool) {
		if label == "" {
			return
		}
		i, ok := at[use]
		if !ok {
			i = len(out)
			at[use] = i
			out = append(out, helpEntry{use: use, text: m.t(use.Text), also: also})
		}
		if field {
			out[i].fieldKeys = append(out[i].fieldKeys, label)
		} else if !slices.Contains(out[i].actionKeys, label) {
			out[i].actionKeys = append(out[i].actionKeys, label)
		}
	}
	for _, a := range m.keys.actions {
		label := m.actionKeyLabel(a.ID)
		for i, use := range a.helpUses() {
			if a.windowOnly() {
				use.Context = ctxWindowOnly
			}
			// A tecla da plataforma só tem para onde ir com duas, e a linha diz quais.
			if a.ID == actSwitchPlatform && len(m.catalogs) < 2 {
				continue
			}
			add(use, label, false, i > 0)
		}
	}
	for _, f := range fieldKeys {
		labels := make([]string, len(f.Keys))
		for i, k := range f.Keys {
			labels[i] = keyLabel(k, m.catalog)
		}
		for _, use := range f.Uses {
			add(use, joinKeyLabels(labels), true, true)
		}
	}
	for i := range out {
		if out[i].use.Text == string(actSwitchPlatform) {
			out[i].text = m.helpPlatformLine()
		}
	}
	return append(out, m.settingsEntries()...)
}

// actionKeyLabel são as teclas efetivas da ação, como a tela as mostra.
func (m Model) actionKeyLabel(id actionID) string {
	var labels []string
	for _, k := range m.keys.keys[id] {
		labels = append(labels, keyLabel(k, m.catalog))
	}
	return joinKeyLabels(labels)
}

// settingsEntries é a seção Configurações: idioma, cor e o último erro do
// player, que não são teclas.
func (m Model) settingsEntries() []helpEntry {
	info := func(text string) helpEntry {
		return helpEntry{use: helpUse{Section: sectionSettings}, text: text}
	}
	var out []helpEntry
	if m.catalog != nil {
		out = append(out, info(fmt.Sprintf(m.t("help.language"), m.catalog.Lang())))
	}
	out = append(out,
		info(m.t("help.nocolor")),
		helpEntry{use: helpUse{Section: sectionSettings}, text: m.t("help.colors"), legend: true},
	)
	if line := m.player.errorLine(); line != "" {
		out = append(out, info(fmt.Sprintf(m.t("help.player_error"), line)))
		// Same priority as the status bar: streamlink first, then mpv.
		if hint := m.manualInstallHint(); (m.player.failure == PlayerFailureStreamlinkMissing || m.player.downloadFailed) && hint != "" {
			out = append(out, info(hint))
		} else if hint := m.manualInstallMPVHint(); m.player.failure == PlayerFailureMPVMissing && hint != "" {
			out = append(out, info(hint))
		}
	}
	return out
}

// helpKeyColumn é a coluna em que o texto das linhas começa: logo depois das
// teclas mais largas, contando o recuo, até um quinto da tela. Teclas que passam
// disso ficam sozinhas na linha, com o texto embaixo.
func helpKeyColumn(entries []helpEntry, width int) int {
	limit := 24
	if width > 0 {
		limit = min(max(width/5, 8), 18) + 6
	}
	col := 0
	for _, e := range entries {
		if end := len(helpIndent(e.use.Context)) + ansi.StringWidth(e.keys()) + 2; end <= limit {
			col = max(col, end)
		}
	}
	return col
}

// helpIndent é o recuo da linha: as de uma situação ficam sob o título dela.
func helpIndent(c helpContext) string {
	if c == ctxAny {
		return "  "
	}
	return "    "
}

// helpDocument monta a ajuda na largura maxWidth. Nenhuma linha é cortada: o que
// não cabe na altura se alcança rolando.
func (m Model) helpDocument(maxWidth int) helpDoc {
	if !m.layout.helpVisible {
		return helpDoc{}
	}
	entries := m.helpEntries()
	textCol := helpKeyColumn(entries, maxWidth)
	doc := helpDoc{title: wrapChatRow("", "=== "+m.t("help.title")+" ===", maxWidth)}
	for _, section := range helpSections {
		doc.starts = append(doc.starts, len(doc.body))
		doc.body = append(doc.body, wrapChatRow("", styleFocus("── "+m.helpSectionName(section)+" ──"), maxWidth)...)
		for _, ctx := range helpContextOrder {
			var rows []string
			for _, also := range []bool{false, true} {
				for _, e := range entries {
					if e.use.Section == section && e.use.Context == ctx && e.also == also {
						rows = append(rows, helpEntryRows(e, textCol, maxWidth)...)
					}
				}
			}
			if label := m.helpContextLabel(ctx); label != "" && len(rows) > 0 {
				doc.body = append(doc.body, wrapChatRow("  ", label, maxWidth)...)
			}
			doc.body = append(doc.body, rows...)
		}
	}
	return doc
}

// helpEntryRows quebra uma linha da ajuda: as teclas no recuo dela e o texto na
// coluna textCol, com as linhas seguintes alinhadas ao texto.
func helpEntryRows(e helpEntry, textCol, maxWidth int) []string {
	keys, indent := e.keys(), helpIndent(e.use.Context)
	var rows []string
	switch lead := indent + keys + "  "; {
	case keys == "":
		rows = wrapChatRow(indent, e.text, maxWidth)
	case ansi.StringWidth(lead) > textCol:
		rows = append(wrapChatRow(indent, keys, maxWidth),
			wrapChatRow(strings.Repeat(" ", textCol), e.text, maxWidth)...)
	default:
		rows = wrapChatRow(padToWidth(lead, textCol), e.text, maxWidth)
	}
	if e.legend {
		for i, row := range rows {
			rows[i] = colorizeHelpLegend(row)
		}
	}
	return rows
}

func (m Model) helpSectionName(s helpSection) string {
	switch s {
	case sectionGlobal:
		return m.t("help.section.global")
	case sectionNav:
		return m.t("help.section.nav")
	case sectionSearch:
		return m.t("help.section.search")
	case sectionChat:
		return m.t("help.section.chat")
	case sectionVideo:
		return m.t("help.section.video")
	case sectionWindow:
		return m.t("help.section.window")
	default:
		return m.t("help.section.settings")
	}
}

// helpContextLabel é o título da situação dentro da seção. As ações só da janela
// levam a marca fora dela; dentro, nenhuma.
func (m Model) helpContextLabel(c helpContext) string {
	switch c {
	case ctxHelp:
		return m.t("help.ctx.help")
	case ctxSearchPrompt:
		return m.t("help.ctx.search_prompt")
	case ctxFilter:
		return m.t("help.ctx.filter")
	case ctxMenu:
		return m.t("help.ctx.menu")
	case ctxChatPanel:
		return m.t("help.ctx.chat_panel")
	case ctxCompose:
		return m.t("help.ctx.compose")
	case ctxField:
		return m.t("help.ctx.field")
	case ctxWindowOnly:
		if m.hosted.host == nil {
			return m.t("help.ctx.window_only")
		}
	}
	return ""
}

// renderHelpLines returns the whole help, wrapped at maxWidth: the title and
// every section.
func (m Model) renderHelpLines(maxWidth int) []string {
	doc := m.helpDocument(maxWidth)
	return append(slices.Clone(doc.title), doc.body...)
}

// helpPageFor diz quanto da ajuda cabe na altura. Sem caber tudo, o título fica
// fixo no topo e a última linha diz onde se está, se sobrar corpo para as duas.
func (m Model) helpPageFor(doc helpDoc) helpPage {
	available := m.height - 1
	if m.height <= 0 || len(doc.title)+len(doc.body) <= available {
		return helpPage{sticky: len(doc.title), rows: len(doc.body)}
	}
	page := helpPage{indicator: m.height > 2}
	rest := available
	if page.indicator {
		rest--
	}
	if rest-len(doc.title) >= 1 {
		page.sticky = len(doc.title)
	}
	page.rows = max(rest-page.sticky, 1)
	return page
}

// helpSectionIndex é a posição da seção em helpSections.
func helpSectionIndex(s helpSection) int {
	return max(slices.Index(helpSections, s), 0)
}

// helpOffset é a primeira linha do corpo na tela: o começo da seção atual mais o
// que se rolou dentro dela, nos limites da altura. Guardar a seção, e não a
// linha, mantém o lugar quando a largura muda a quebra das linhas.
func (m Model) helpOffset(doc helpDoc, page helpPage) int {
	if len(doc.starts) == 0 {
		return 0
	}
	offset := doc.starts[helpSectionIndex(m.layout.helpSection)] + m.layout.helpScroll
	return min(max(offset, 0), max(len(doc.body)-page.rows, 0))
}

// helpSectionAt é a seção da linha do corpo row.
func helpSectionAt(doc helpDoc, row int) helpSection {
	at := 0
	for i, start := range doc.starts {
		if start <= row {
			at = i
		}
	}
	return helpSections[at]
}

// helpViewLines returns the help rows to draw: all of them when they fit, else
// the fixed title, the scrolled part of the body and, when there is room for
// it, the row that says where one is.
func (m Model) helpViewLines() []string {
	width := m.helpTextWidth()
	doc := m.helpDocument(width)
	if len(doc.title)+len(doc.body) == 0 {
		return nil
	}
	page := m.helpPageFor(doc)
	offset := m.helpOffset(doc, page)
	out := append(slices.Clone(doc.title[:page.sticky]), doc.body[offset:offset+page.rows]...)
	if page.indicator {
		out = append(out, m.helpPositionRow(offset+page.rows, len(doc.body), width))
	}
	return out
}

// helpPositionRow diz a seção atual e a linha: "Chat · seção 4 de 7 · Tab/→
// próxima" à esquerda e "33/80" à direita. Sem largura, a dica sai primeiro e a
// seção depois.
func (m Model) helpPositionRow(current, total, width int) string {
	pos := fmt.Sprintf("%d/%d", current, total)
	at := helpSectionIndex(m.layout.helpSection)
	where := fmt.Sprintf(m.t("help.position"), m.helpSectionName(helpSections[at]), at+1, len(helpSections))
	for _, left := range []string{where + statusSep + m.t("help.position_hint"), where} {
		if pad := width - ansi.StringWidth(left) - ansi.StringWidth(pos); pad >= 1 {
			return left + strings.Repeat(" ", pad) + pos
		}
	}
	return positionRow(current, total, width)
}

// positionRow is the right-aligned "current/total" row of a scrolled overlay.
func positionRow(current, total, width int) string {
	position := fmt.Sprintf("%d/%d", current, total)
	pad := max(width-ansi.StringWidth(position), 0)
	return strings.Repeat(" ", pad) + position
}

// updateHelpKey consumes every key while the help is open: it closes, scrolls,
// jumps between sections, or does nothing. Nothing here may touch the state
// behind the help.
func (m Model) updateHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	doc := m.helpDocument(m.helpTextWidth())
	page := m.helpPageFor(doc)
	step := max(page.rows-1, 1)
	offset := m.helpOffset(doc, page)
	at := helpSectionIndex(m.layout.helpSection)
	switch m.keys.actionFor(modeHelp, msg) {
	case actHelp, actHelpAnywhere, actBack, actCloseHelp:
		m.toggleHelp()
	case actHelpNext:
		m.jumpHelp(helpSections[(at+1)%len(helpSections)])
	case actHelpPrev:
		m.jumpHelp(helpSections[(at+len(helpSections)-1)%len(helpSections)])
	case actHome:
		m.jumpHelp(helpSections[0])
	case actEnd:
		m.jumpHelp(helpSections[len(helpSections)-1])
		m.layout.helpScroll = len(doc.body)
	case actUp:
		m.scrollHelp(doc, page, offset, offset-1)
	case actDown:
		m.scrollHelp(doc, page, offset, offset+1)
	case actPageUp:
		m.scrollHelp(doc, page, offset, offset-step)
	case actPageDown:
		m.scrollHelp(doc, page, offset, offset+step)
	}
	return m, nil
}

// jumpHelp leva a ajuda ao começo da seção.
func (m *Model) jumpHelp(s helpSection) {
	m.layout.helpSection, m.layout.helpScroll = s, 0
}

// scrollHelp move a ajuda para a linha to, nos limites, e passa a contar a
// posição a partir da seção que fica no topo. Uma tecla que não move nada não
// muda a seção que a linha de posição mostra.
func (m *Model) scrollHelp(doc helpDoc, page helpPage, from, to int) {
	to = min(max(to, 0), max(len(doc.body)-page.rows, 0))
	if to == from || len(doc.starts) == 0 {
		return
	}
	s := helpSectionAt(doc, to)
	m.layout.helpSection = s
	m.layout.helpScroll = to - doc.starts[helpSectionIndex(s)]
}
