package ui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// WithKeymap selects the legacy bindings and an optional startup notice.
func WithKeymap(classic bool, notice string) Option {
	return func(m *Model) {
		m.classic = classic
		m.keys = defaultKeys()
		if classic {
			m.keys = newKeyRegistry(classicActions(), nil)
		}
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
			next, cmd := m.returnToCategories()
			m = next.(Model)
			m.layout.showList = true
			m.setFocus(FocusList)
			kind := ItemTypeFavorite
			if id == actRecents {
				kind = ItemTypeRecent
			}
			for i, item := range m.VisibleItems() {
				if item.Type == kind {
					m.moveHighlight(i - m.list.indexOfHighlighted(m.VisibleItems()))
					break
				}
			}
			return m, cmd, true
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
	next, cmd := m.returnToCategories()
	m = next.(Model)
	m.focusLiveCatalog()
	return m, cmd
}
