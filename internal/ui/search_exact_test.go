package ui

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// gaulesSource reproduces Marina's search: ten categories, nine of them without
// a broadcast, and the exact channel behind a look-alike.
func gaulesSource(opened *string) *fakeSource {
	return &fakeSource{
		categoriesFn: func(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: catalog.IntPtr(100000)},
			}}, nil
		},
		searchFn: func(context.Context, string) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "clone", Login: "gaules_fan", DisplayName: "gaules_fan", Viewers: catalog.IntPtr(9000), Availability: catalog.AvailabilityLive},
				{ID: "exact", Login: "gaules", DisplayName: "Gaules", Viewers: catalog.IntPtr(30000), Availability: catalog.AvailabilityLive},
				{ID: "old", Login: "gaulesclips", DisplayName: "GaulesClips", Availability: catalog.AvailabilityOffline},
			}}, nil
		},
		searchCategoriesFn: func(context.Context, string) ([]catalog.Category, error) {
			cats := []catalog.Category{}
			for i := range 9 {
				cats = append(cats, catalog.Category{ID: fmt.Sprintf("dead-%d", i), Name: fmt.Sprintf("Gale Game %d", i)})
			}
			return append(cats, catalog.Category{ID: "xd", Name: "Pokémon XD: Gale of Darkness", Viewers: catalog.IntPtr(14)}), nil
		},
		channelsFn: func(_ context.Context, categoryID string, _ catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			if opened != nil {
				*opened = categoryID
			}
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "c1", Login: "inside", DisplayName: "Inside", Viewers: catalog.IntPtr(5)},
			}}, nil
		},
	}
}

func runSearch(t *testing.T, m Model, query string) Model {
	t.Helper()
	updated, cmd := m.startSearch(query)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected a search command")
	}
	m, _ = pressKeyMsg(m, cmd())
	return m
}

func cursorRow(t *testing.T, m Model) string {
	t.Helper()
	got := cursorRows(m)
	var rows []string
	for _, row := range got {
		// The focused panel title also starts with the caret.
		if !strings.Contains(row, m.t("focus.active_tag")) {
			rows = append(rows, row)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("cursor rows = %q, want 1:\n%s", rows, ansi.Strip(m.View()))
	}
	return rows[0]
}

func TestSearchExactLoginComesBeforeCategories(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gaules")

	items := m.VisibleItems()
	if items[0].Channel == nil || items[0].Login() != "gaules" {
		t.Fatalf("first item = %+v, want channel gaules", items[0])
	}
	if m.HighlightedID() != "exact" {
		t.Fatalf("highlight = %q, want exact", m.HighlightedID())
	}
	if row := cursorRow(t, m); !strings.Contains(row, "Gaules") {
		t.Fatalf("cursor row = %q, want Gaules", row)
	}
	view := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE search gaules:\n%s", view)
	exact := strings.Index(view, "── "+m.t("search.section_exact")+" ──")
	cats := strings.Index(view, "── Categorias ──")
	if exact < 0 || cats < 0 || exact > cats {
		t.Fatalf("exact section must come before categories: exact=%d cats=%d\n%s", exact, cats, view)
	}
}

func TestSearchExactChannelAppearsOnce(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gaules")
	var seen int
	for _, it := range m.VisibleItems() {
		if it.Login() == "gaules" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("gaules rows = %d, want 1", seen)
	}
	if got := len(m.VisibleItems()); got != 13 {
		t.Fatalf("items = %d, want 3 channels + 10 categories", got)
	}
}

func TestSearchExactIgnoresCaseAndSpaces(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "  GAULES ")
	if m.HighlightedID() != "exact" {
		t.Fatalf("highlight = %q, want exact", m.HighlightedID())
	}
}

func TestSearchExactDisplayNameComesFirst(t *testing.T) {
	src := gaulesSource(nil)
	src.searchFn = func(context.Context, string) (catalog.Page[catalog.Channel], error) {
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
			{ID: "other", Login: "hangeul_fan", DisplayName: "hangeul_fan", Availability: catalog.AvailabilityLive},
			{ID: "kr", Login: "koreanstreamer", DisplayName: "한글이름", Availability: catalog.AvailabilityOffline},
		}}, nil
	}
	m := runSearch(t, newSearchCategoriesModel(t, src), "한글이름")
	if items := m.VisibleItems(); items[0].Login() != "koreanstreamer" {
		t.Fatalf("first item = %+v, want koreanstreamer", items[0])
	}
	if m.HighlightedID() != "kr" {
		t.Fatalf("highlight = %q, want kr", m.HighlightedID())
	}
}

func TestEnterOnFirstSearchResultOpensExactChannel(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gaules")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.list.selectedChannel == nil || m.list.selectedChannel.Login != "gaules" {
		t.Fatalf("selected channel = %+v, want gaules", m.list.selectedChannel)
	}
	if m.list.selectedCategory != nil {
		t.Fatalf("Enter must not open a category: %+v", m.list.selectedCategory)
	}
}

func TestSearchCategoriesWithoutBroadcastComeAfterLiveOnes(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gaules")
	var cats []catalog.Category
	for _, it := range m.VisibleItems() {
		if it.Category != nil {
			cats = append(cats, *it.Category)
		}
	}
	if len(cats) != 10 || cats[0].ID != "xd" {
		t.Fatalf("categories = %+v, want the live one first", cats)
	}
	for _, cat := range cats[1:] {
		if cat.Viewers != nil {
			t.Fatalf("category with viewers after the ones without: %+v", cats)
		}
	}
}

func TestSearchByCategoryNameStillOpensCategory(t *testing.T) {
	var opened string
	src := gaulesSource(&opened)
	src.searchFn = func(context.Context, string) (catalog.Page[catalog.Channel], error) {
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
			{ID: "fan", Login: "darkness_fan", DisplayName: "darkness_fan", Availability: catalog.AvailabilityLive},
		}}, nil
	}
	m := runSearch(t, newSearchCategoriesModel(t, src), "Pokémon XD: Gale of Darkness")
	if m.HighlightedID() != "xd" {
		t.Fatalf("highlight = %q, want category xd", m.HighlightedID())
	}
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected channels fetch")
	}
	m, _ = pressKeyMsg(m, cmd())
	if opened != "xd" || m.Mode() != ModeChannels {
		t.Fatalf("opened = %q mode = %q, want xd in channels", opened, m.Mode())
	}
}

func TestSearchWithoutExactMatchKeepsCurrentOrder(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gau")

	items := m.VisibleItems()
	if items[0].Category == nil || items[0].Category.ID != "xd" {
		t.Fatalf("first item = %+v, want the category with most viewers", items[0])
	}
	if m.HighlightedID() != "xd" {
		t.Fatalf("highlight = %q, want xd", m.HighlightedID())
	}
	var logins []string
	for _, it := range items {
		if it.Channel != nil {
			logins = append(logins, it.Login())
		}
	}
	// Live before offline, relevance order among the live ones.
	if got := strings.Join(logins, ","); got != "gaules_fan,gaules,gaulesclips" {
		t.Fatalf("channel order = %s", got)
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, m.t("search.section_exact")) {
		t.Fatalf("no exact section without an exact match:\n%s", view)
	}
}

func TestSearchExactChannelBeatsExactCategoryWhichStaysNext(t *testing.T) {
	var opened string
	src := gaulesSource(&opened)
	src.searchFn = func(context.Context, string) (catalog.Page[catalog.Channel], error) {
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
			{ID: "official", Login: "valorant", DisplayName: "VALORANT", Availability: catalog.AvailabilityOffline},
		}}, nil
	}
	src.searchCategoriesFn = func(context.Context, string) ([]catalog.Category, error) {
		return []catalog.Category{
			{ID: "other", Name: "Valorant Mobile", Viewers: catalog.IntPtr(900000)},
			{ID: "val", Name: "VALORANT", Viewers: catalog.IntPtr(200000)},
		}, nil
	}
	m := runSearch(t, newSearchCategoriesModel(t, src), "valorant")
	if m.HighlightedID() != "official" {
		t.Fatalf("highlight = %q, want the exact channel", m.HighlightedID())
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.HighlightedID() != "val" {
		t.Fatalf("one j must reach the exact category, got %q", m.HighlightedID())
	}
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if opened != "val" || m.Mode() != ModeChannels {
		t.Fatalf("opened = %q mode = %q, want val in channels", opened, m.Mode())
	}
}

func TestSearchExactFavoriteGetsTheCursorWhereItRenders(t *testing.T) {
	m := newSearchCategoriesModel(t, gaulesSource(nil))
	m.list.favorites = []catalog.Channel{
		{ID: "fav", Login: "alanzoka", DisplayName: "alanzoka"},
		{ID: "exact", Login: "gaules", DisplayName: "Gaules"},
	}
	m = runSearch(t, m, "gaules")
	if m.HighlightedID() != "exact" {
		t.Fatalf("highlight = %q, want the favorite row of gaules", m.HighlightedID())
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.list.selectedChannel == nil || m.list.selectedChannel.Login != "gaules" {
		t.Fatalf("selected channel = %+v, want gaules", m.list.selectedChannel)
	}
}

func TestSearchExactHiddenByPreferencesFallsBackToCategory(t *testing.T) {
	m := newSearchCategoriesModel(t, gaulesSource(nil))
	m.list.prefs.minViewers = 50000
	m = runSearch(t, m, "gaules")
	if m.HighlightedID() != "xd" {
		t.Fatalf("highlight = %q, want first category xd", m.HighlightedID())
	}
}

func TestLocalFilterOverSearchResultsStillWorks(t *testing.T) {
	m := runSearch(t, newSearchCategoriesModel(t, gaulesSource(nil)), "gaules")

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "pokémon")
	items := m.VisibleItems()
	if len(items) != 1 || items[0].Category == nil || items[0].Category.ID != "xd" {
		t.Fatalf("filter must hide the exact row too: %+v", items)
	}
	if m.HighlightedID() != "xd" {
		t.Fatalf("highlight = %q, want xd", m.HighlightedID())
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.VisibleItems(); got[0].Login() != "gaules" {
		t.Fatalf("clearing the filter must restore the exact row first: %+v", got[0])
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "gaulesclips")
	items = m.VisibleItems()
	if len(items) != 1 || items[0].Login() != "gaulesclips" {
		t.Fatalf("filter must keep matching channels: %+v", items)
	}
}

func TestExactSectionAloneWhenItIsTheOnlyChannel(t *testing.T) {
	src := gaulesSource(nil)
	src.searchFn = func(context.Context, string) (catalog.Page[catalog.Channel], error) {
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
			{ID: "exact", Login: "gaules", DisplayName: "Gaules"},
		}}, nil
	}
	m := runSearch(t, newSearchCategoriesModel(t, src), "gaules")
	view := ansi.Strip(m.View())
	if strings.Contains(view, "── Canais ──") {
		t.Fatalf("empty channels section must not render:\n%s", view)
	}
}
