package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func filterClearFixture(t *testing.T) Model {
	t.Helper()
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{
				Items: []twitch.Channel{
					{ID: "ch-peere", Login: "peere", DisplayName: "Peere", Viewers: twitch.IntPtr(100)},
					{ID: "ch-alan", Login: "alanzoka", DisplayName: "alanzoka", Viewers: twitch.IntPtr(200)},
					{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(300)},
				},
			}, nil
		},
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "cat-1", Name: "Just Chatting"}},
			}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{
				Items: []twitch.Channel{
					{ID: "ch-peere", Login: "peere", DisplayName: "Peere"},
				},
			}, nil
		},
	}
	m := New(cat,
		WithSource(src),
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "favorite_one", DisplayName: "Favorite One"},
		}),
	)
	m.mode = ModeChannels
	m.selectedCategory = &twitch.Category{ID: "cat-1", Name: "Just Chatting"}
	m.loading = true
	m.generation++
	loadCmd := m.fetchChannelsCmd(m.generation, *m.selectedCategory, "", false)
	updated, _ := m.Update(loadCmd())
	m = updated.(Model)
	if len(m.Channels()) != 3 {
		t.Fatalf("fixture channels = %d, want 3", len(m.Channels()))
	}
	return m
}

func TestOpenChannelClearsFilterAndShowsFavorites(t *testing.T) {
	m := filterClearFixture(t)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	if m.Filter() != "peere" || !m.Filtering() {
		t.Fatalf("filter state: Filtering=%v Filter=%q", m.Filtering(), m.Filter())
	}
	items := m.VisibleItems()
	if len(items) != 1 || items[0].Login() != "peere" {
		t.Fatalf("filtered items = %+v, want only peere", items)
	}
	for _, it := range items {
		if it.Type == ItemTypeFavorite {
			t.Fatal("favorite must be hidden while filter is active")
		}
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected ChannelSelectedMsg cmd")
	}
	msg := cmd()
	sel, ok := msg.(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "peere" {
		t.Fatalf("got %+v, want ChannelSelectedMsg for peere", msg)
	}
	if m.Filtering() || m.Filter() != "" {
		t.Fatalf("after open: Filtering=%v Filter=%q, want cleared", m.Filtering(), m.Filter())
	}

	visible := m.VisibleItems()
	var sawFav, sawPeere, sawAlan bool
	for _, it := range visible {
		switch {
		case it.Type == ItemTypeFavorite && it.Login() == "favorite_one":
			sawFav = true
		case it.Login() == "peere":
			sawPeere = true
		case it.Login() == "alanzoka":
			sawAlan = true
		}
	}
	if !sawFav || !sawPeere || !sawAlan {
		t.Fatalf("full list missing items: fav=%v peere=%v alan=%v items=%v", sawFav, sawPeere, sawAlan, visibleLogins(visible))
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "Filter:") {
		t.Fatalf("view still shows filter prompt after opening channel:\n%s", view)
	}
	if !strings.Contains(view, "Favorite One") || !strings.Contains(view, "alanzoka") {
		t.Fatalf("view missing restored list rows:\n%s", view)
	}
}

func TestEscClearsFilterWithoutLeavingChannels(t *testing.T) {
	m := filterClearFixture(t)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeChannels || m.Filter() != "" || m.Filtering() {
		t.Fatalf("after Esc: mode=%q filter=%q filtering=%v", m.Mode(), m.Filter(), m.Filtering())
	}
	if !hasFavoriteLogin(m.VisibleItems(), "favorite_one") {
		t.Fatal("favorite not visible after Esc cleared the filter")
	}
}

func TestCtrlRClearsFilterAndReturnsToCategories(t *testing.T) {
	m := filterClearFixture(t)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if m.Filter() != "" || m.Filtering() {
		t.Fatalf("Ctrl+R left filter=%q filtering=%v", m.Filter(), m.Filtering())
	}
	if m.Mode() != ModeCategories {
		t.Fatalf("mode=%q, want categories", m.Mode())
	}
	if cmd != nil {
		updated, _ := m.Update(cmd())
		m = updated.(Model)
	}
	if m.Filter() != "" {
		t.Fatalf("filter survived category reload: %q", m.Filter())
	}
}

func TestRefreshKeepsFilter(t *testing.T) {
	m := filterClearFixture(t)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	// Leave the filter prompt so 'r' is Retry, not a typed rune.
	m.filtering = false
	if m.Filter() != "peere" {
		t.Fatalf("precondition filter=%q", m.Filter())
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if m.Filter() != "peere" {
		t.Fatalf("refresh cleared filter: %q", m.Filter())
	}
	if cmd != nil {
		updated, _ := m.Update(cmd())
		m = updated.(Model)
	}
	if m.Filter() != "peere" {
		t.Fatalf("filter after refresh load: %q, want peere", m.Filter())
	}
	items := m.VisibleItems()
	if len(items) != 1 || items[0].Login() != "peere" {
		t.Fatalf("refresh must keep filtered context, got %v", visibleLogins(items))
	}
}

func TestCtrlSUsesFilterTextExplicitly(t *testing.T) {
	m := filterClearFixture(t)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.Mode() != ModeSearch || m.SearchQuery() != "peere" || cmd == nil {
		t.Fatalf("Ctrl+S: mode=%q query=%q cmd=%v", m.Mode(), m.SearchQuery(), cmd != nil)
	}
	if m.Filter() != "" || m.Filtering() {
		t.Fatalf("search must consume the filter: filter=%q filtering=%v", m.Filter(), m.Filtering())
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.Filter() != "" {
		t.Fatalf("filter after search load: %q", m.Filter())
	}
}

func TestOpenChannelPreventsSilentFilterReuseOnNextSearch(t *testing.T) {
	m := filterClearFixture(t)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "peere")
	m, selCmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if selCmd == nil {
		t.Fatal("expected select cmd")
	}
	_ = selCmd()
	if m.Filter() != "" {
		t.Fatalf("filter still set after open: %q", m.Filter())
	}

	// Next Ctrl+S must open a blank search prompt, not reuse "peere".
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !m.Searching() || m.searchInput != "" || cmd != nil {
		t.Fatalf("after open, Ctrl+S must prompt empty: searching=%v input=%q cmd=%v", m.Searching(), m.searchInput, cmd != nil)
	}
	if m.Mode() == ModeSearch {
		t.Fatal("Ctrl+S must not auto-submit a stale filter after opening a channel")
	}
}

func visibleLogins(items []ListItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Login()
		if out[i] == "" && it.Category != nil {
			out[i] = it.Category.Name
		}
	}
	return out
}

func hasFavoriteLogin(items []ListItem, login string) bool {
	for _, it := range items {
		if it.Type == ItemTypeFavorite && strings.EqualFold(it.Login(), login) {
			return true
		}
	}
	return false
}
