package ui

import (
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// WithFavoriteCategories carrega as categorias fixadas do state.json; a audiência vem do catálogo.
func WithFavoriteCategories(cats []store.Category) Option {
	return func(m *Model) { m.list.favoriteCategories = append([]store.Category(nil), cats...) }
}

func (m Model) categoryPinned(id string) bool {
	for _, c := range m.list.favoriteCategories {
		if platform.Normalize(c.Platform) == m.ListPlatform() && c.ID == id {
			return true
		}
	}
	return false
}

func (m Model) appendPinnedCategories(items []ListItem, p itemPass) []ListItem {
	for _, saved := range m.list.favoriteCategories {
		if platform.Normalize(saved.Platform) != m.ListPlatform() {
			continue
		}
		cat := catalog.Category{ID: saved.ID, Name: saved.Name}
		for _, fresh := range m.list.categories {
			if fresh.ID == saved.ID {
				cat = fresh
				break
			}
		}
		if !p.matchesFilter(cat.Name, "", "") {
			continue
		}
		items = append(items, ListItem{Type: ItemTypePinnedCategory, Category: &cat, Favorite: true})
	}
	return items
}

func (m Model) toggleCategory(cat catalog.Category) (tea.Model, tea.Cmd) {
	key := "categories.pinned"
	saved := store.Category{Platform: m.ListPlatform(), ID: cat.ID, Name: cat.Name}
	index := slices.IndexFunc(m.list.favoriteCategories, saved.Matches)
	if index >= 0 {
		m.list.favoriteCategories = slices.Concat(m.list.favoriteCategories[:index], m.list.favoriteCategories[index+1:])
		key = "categories.unpinned"
	} else {
		m.list.favoriteCategories = append(m.list.favoriteCategories, saved)
	}
	m.clearStatusNotice()
	if m.Mode() == ModeCategories {
		// A categoria muda de seção sem levar o cursor junto. A linha que ocupa
		// a posição anterior permite seguir pelo catálogo com a próxima seta.
		m.setHighlightToItem(m.VisibleItems(), m.list.highlightIndex)
	} else {
		m.reconcileHighlight()
	}
	if m.store == nil {
		m.notices.status = fmt.Sprintf(m.t(key), sanitizeScreenText(cat.Name))
		return m, nil
	}
	st, s := m.list.toStoreState(), m.store
	cmd := m.storeSaves.cmd(func() tea.Msg {
		return storeSavedMsg{Err: s.Save(st), Notice: key, Name: cat.Name}
	})
	return m, cmd
}
