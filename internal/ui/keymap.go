package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
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

func (m Model) handlePrefixKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.classic {
		return m, nil, false
	}
	name := keyName(msg)
	if m.prefix != "" {
		seq := m.prefix + " " + name
		m.prefix, m.prefixVisible = "", false
		m.prefixGeneration++
		if name == "esc" {
			return m, nil, true
		}
		if id := m.keys.byMode[modeGlobal][seq]; id == actCategories {
			return handled(m.keyCategories())
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
		return m, nil, true
	}
	if name != "ctrl+w" && (name != "g" || m.typingField() || m.list.prefs.menuOpen) {
		return m, nil, false
	}
	m.prefix = name
	m.prefixGeneration++
	gen := m.prefixGeneration
	return m, m.after(250*time.Millisecond, func(time.Time) tea.Msg { return prefixPopupMsg{gen} }), true
}

func (m Model) prefixLines() []string {
	lines := []string{keyLabel(m.prefix, m.catalog)}
	for _, a := range m.keys.actions {
		for _, key := range m.keys.keys[a.ID] {
			if strings.HasPrefix(key, m.prefix+" ") {
				lines = append(lines, keyLabel(key, m.catalog)+"  "+m.t(string(a.ID)))
			}
		}
	}
	return append(lines, m.t("keymap.cancel"))
}

func (m Model) keyCategories() (tea.Model, tea.Cmd) {
	if !m.classic {
		m.layout.showList = true
		m.setFocus(FocusList)
	}
	return m.returnToCategories()
}
