package ui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// WithKeymap selects the legacy bindings and an optional startup notice.
func WithKeymap(classic bool, notice string) Option {
	return WithKeymapOverrides(classic, nil, notice)
}

// WithKeymapOverrides aplica a base (default ou classic), os overrides de
// [keys] já validados e o aviso de início (migração e/ou validação).
func WithKeymapOverrides(classic bool, overrides map[actionID][]string, notice string) Option {
	return func(m *Model) {
		m.classic = classic
		actions := defaultActions()
		if classic {
			actions = classicActions()
		}
		m.keys = newKeyRegistry(actions, overrides)
		if notice != "" {
			m.keymapNotice = notice
		}
	}
}

type prefixPopupMsg struct{ gen uint64 }

// prefixKeys são as teclas que começam uma sequência no mapa padrão: Ctrl+W, a
// das janelas, como no vim, e g, a de ir a um lugar da lista.
var prefixKeys = []string{"ctrl+w", "g"}

func (m Model) handlePrefixKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.classic {
		return m, nil, false
	}
	name := keyName(msg)
	if m.prefix != "" {
		seq := m.prefix + " " + prefixSecondKey(name)
		m.cancelPrefix()
		if name == "esc" {
			return m, nil, true
		}
		if id := m.keys.byMode[modeGlobal][seq]; id == actCategories {
			return handled(m.keyCategories())
		}
		if m.keys.byMode[modeNormal][seq] == actHome {
			return handled(m.keyHome())
		}
		if id := m.keys.byMode[modeNormal][seq]; id == actFavorites || id == actRecents {
			return handled(m.keySaved(id))
		}
		if next, cmd, ok := m.handleLayoutAction(m.keys.byMode[modeGlobal][seq], m.keys.byMode[modeWindow][seq]); ok {
			return next, cmd, true
		}
		// Só a ajuda atravessa o prefixo: F1 o desfaz e abre a ajuda. Ctrl+C e
		// Ctrl+Q chegam antes, em handleKeyMsg. As demais teclas acabam a
		// sequência sem efeito.
		if m.keys.actionFor(modeGlobal, msg) == actHelpAnywhere {
			return m, nil, false
		}
		return m, nil, true
	}
	if !slices.Contains(prefixKeys, name) || name == "g" && (m.typingField() || m.list.prefs.menuOpen) {
		return m, nil, false
	}
	m.prefix = name
	m.prefixGeneration++
	gen := m.prefixGeneration
	return m, m.after(250*time.Millisecond, func(time.Time) tea.Msg { return prefixPopupMsg{gen} }), true
}

// prefixSecondKey é a tecla que fecha a sequência. Como no vim, Ctrl+letra vale
// a letra: Ctrl+W Ctrl+H é Ctrl+W h, e Ctrl+W Ctrl+X, Ctrl+W x, sem encerrar o
// vídeo no caminho.
func prefixSecondKey(name string) string {
	if letter, ok := strings.CutPrefix(name, "ctrl+"); ok && len(letter) == 1 && letter[0] >= 'a' && letter[0] <= 'z' {
		return letter
	}
	return name
}

// cancelPrefix desfaz o prefixo pendente e o popup dele. O temporizador que
// ainda vai chegar traz a geração antiga e não reabre o popup.
func (m *Model) cancelPrefix() {
	if m.prefix == "" && !m.prefixVisible {
		return
	}
	m.prefix, m.prefixVisible = "", false
	m.prefixGeneration++
}

// prefixLines é o popup do prefixo: as sequências dele, uma por linha. Sem a
// janela, as que só valem nela (chat sobreposto, lados, altura e largura) ficam
// de fora.
func (m Model) prefixLines() []string {
	lines := []string{keyLabel(m.prefix, m.catalog)}
	for _, a := range m.keys.actions {
		if a.windowOnly() && m.hosted.host == nil {
			continue
		}
		for _, key := range m.keys.keys[a.ID] {
			if strings.HasPrefix(key, m.prefix+" ") {
				lines = append(lines, keyLabel(key, m.catalog)+"  "+m.t(a.text()))
			}
		}
	}
	return append(lines, m.t("keymap.cancel"))
}

// keyCategories volta à tela inicial e leva o destaque à primeira categoria
// ao vivo, abaixo das fixadas, dos favoritos e dos recentes. A recarga do
// catálogo que vem depois mantém o destaque no topo dele. Esc, que só volta um
// nível, continua devolvendo o destaque à categoria de onde se saiu.
func (m Model) keyCategories() (tea.Model, tea.Cmd) {
	if !m.classic {
		m.layout.showList = true
		m.setFocus(FocusList)
	}
	var cmds []tea.Cmd
	if m.previewOpen() {
		if stop := m.closePreview(); stop != nil {
			cmds = append(cmds, stop)
		}
	}
	next, cmd := m.returnToCategories()
	m = next.(Model)
	m.focusLiveCatalog()
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// keySaved é g f e g r: volta à tela inicial e destaca o primeiro favorito ou o
// primeiro recente. Sem nenhum na lista ativa, nada se move e a barra diz como
// ter um. Com todos escondidos pelos filtros de canal, a tela inicial abre e a
// barra diz que são os filtros. Com a prévia aberta, o destaque novo agenda a
// troca (debounce) ou a fecha se a linha de destino não for canal.
func (m Model) keySaved(id actionID) (tea.Model, tea.Cmd) {
	kind, empty, filtered := ItemTypeFavorite, "feedback.no_favorites", "feedback.favorites_filtered"
	if id == actRecents {
		kind, empty, filtered = ItemTypeRecent, "feedback.no_recents", "feedback.recents_filtered"
	}
	if !m.hasSavedOfType(kind) {
		m.warnStatus(m.t(empty))
		return m, nil
	}
	next, cmd := m.returnToCategories()
	m = next.(Model)
	m.layout.showList = true
	m.setFocus(FocusList)
	items := m.VisibleItems()
	for i, item := range items {
		if item.Type == kind {
			m.moveHighlight(i - m.list.indexOfHighlighted(items))
			m, switchCmd := m.schedulePreviewSwitch()
			return m, tea.Batch(cmd, switchCmd)
		}
	}
	m.warnStatus(m.t(filtered))
	return m, cmd
}
