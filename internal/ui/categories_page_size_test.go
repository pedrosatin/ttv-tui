package ui

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// Ticket 11: initial categories load uses the API maximum (100). TFT sits
// past the old ~60 cut, so /team must find it locally without a global search.
func TestDefaultCategoryPageSizeIsAPIMax(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat)
	if m.list.pageSize != twitch.MaxGamesFirst {
		t.Fatalf("default pageSize = %d, want %d (games(first:) API max)", m.list.pageSize, twitch.MaxGamesFirst)
	}
	if defaultCategoryPageSize != twitch.MaxGamesFirst {
		t.Fatalf("defaultCategoryPageSize = %d, want %d", defaultCategoryPageSize, twitch.MaxGamesFirst)
	}
}

func TestCategoriesLoadRequestsLimit100(t *testing.T) {
	var gotLimit int
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			gotLimit = page.Limit
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "1", Name: "Just Chatting", Viewers: catalog.IntPtr(1)},
			}}, nil
		},
	}
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithSource(src))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init must fetch categories")
	}
	m, _ = pressKeyMsg(m, cmd())
	if gotLimit != twitch.MaxGamesFirst {
		t.Fatalf("Categories Limit = %d, want %d", gotLimit, twitch.MaxGamesFirst)
	}
	if len(m.list.categories) != 1 {
		t.Fatalf("categories loaded = %d, want 1", len(m.list.categories))
	}
}

func TestSlashTeamFindsTeamfightTacticsInTop100(t *testing.T) {
	items := make([]catalog.Category, 0, twitch.MaxGamesFirst)
	for i := 0; i < 70; i++ {
		items = append(items, catalog.Category{
			ID:      fmt.Sprintf("pad-%d", i),
			Name:    fmt.Sprintf("Pad Category %d", i),
			Viewers: catalog.IntPtr(200000 - i*1000),
		})
	}
	// Past the historical ~60 cut: TFT must load with first:100.
	items = append(items, catalog.Category{
		ID:      "513143",
		Name:    "Teamfight Tactics",
		Viewers: catalog.IntPtr(5000),
	})
	for i := len(items); i < twitch.MaxGamesFirst; i++ {
		items = append(items, catalog.Category{
			ID:      fmt.Sprintf("tail-%d", i),
			Name:    fmt.Sprintf("Tail Category %d", i),
			Viewers: catalog.IntPtr(1000 - (i - 70)),
		})
	}

	var searched int
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			if page.Limit != twitch.MaxGamesFirst {
				t.Fatalf("Limit = %d, want %d", page.Limit, twitch.MaxGamesFirst)
			}
			return catalog.Page[catalog.Category]{Items: items}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			searched++
			return catalog.Page[catalog.Channel]{}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			searched++
			return nil, nil
		},
	}

	m := New(mustCatalog(t, i18n.LangPTBR), WithSource(src))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = pressKeyMsg(m, m.Init()())
	if len(m.list.categories) != twitch.MaxGamesFirst {
		t.Fatalf("loaded %d categories, want %d", len(m.list.categories), twitch.MaxGamesFirst)
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "team")
	visible := m.VisibleItems()
	if len(visible) == 0 {
		t.Fatal("/team matched nothing; TFT should be in the local top 100")
	}
	found := false
	for _, it := range visible {
		if it.Category != nil && it.Category.Name == "Teamfight Tactics" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("/team visible items missing Teamfight Tactics: %+v", visible)
	}
	view := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE /team local filter:\n%s", view)
	if !strings.Contains(view, "Teamfight Tactics") {
		t.Fatalf("view missing Teamfight Tactics:\n%s", view)
	}
	if searched != 0 {
		t.Fatalf("local /team must not trigger global search, searched=%d", searched)
	}
}

func TestEnterWithoutFilterMatchStartsGlobalSearch(t *testing.T) {
	var searched []string
	var searchedCats []string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "cat-cs", Name: "Counter-Strike", Viewers: catalog.IntPtr(250000)},
				{ID: "cat-jc", Name: "Just Chatting", Viewers: catalog.IntPtr(410000)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			searched = append(searched, query)
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch-tf2", Login: "tf2live", DisplayName: "TF2Live"},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			searchedCats = append(searchedCats, query)
			return []catalog.Category{
				{ID: "tf2", Name: "Team Fortress 2", Viewers: catalog.IntPtr(187)},
			}, nil
		},
	}

	m := New(mustCatalog(t, i18n.LangPTBR), WithSource(src))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = pressKeyMsg(m, m.Init()())

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "team")
	if !m.filterHasNoMatches() {
		t.Fatalf("expected no local match for %q", m.Filter())
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Mode() != ModeSearch || m.SearchQuery() != "team" || cmd == nil {
		t.Fatalf("Enter without match must search: mode=%q query=%q cmd=%v", m.Mode(), m.SearchQuery(), cmd != nil)
	}
	if m.Filtering() || m.Filter() != "" {
		t.Fatalf("search must consume filter: filtering=%v filter=%q", m.Filtering(), m.Filter())
	}
	m, _ = pressKeyMsg(m, cmd())
	if len(searched) != 1 || searched[0] != "team" {
		t.Fatalf("channel search = %v, want [team]", searched)
	}
	if len(searchedCats) != 1 || searchedCats[0] != "team" {
		t.Fatalf("category search = %v, want [team]", searchedCats)
	}
	view := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE /team + Enter search:\n%s", view)
	if !strings.Contains(view, "Team Fortress 2") || !strings.Contains(view, "TF2Live") {
		t.Fatalf("search results missing TF2 category/channel:\n%s", view)
	}
}

func TestEnterWithFilterMatchOpensHighlightedItem(t *testing.T) {
	var opened string
	var searched int
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "513143", Name: "Teamfight Tactics", Viewers: catalog.IntPtr(5000)},
				{ID: "cat-jc", Name: "Just Chatting", Viewers: catalog.IntPtr(410000)},
			}}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			opened = categoryID
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "c1", Login: "tftpro", DisplayName: "TFTPro", Viewers: catalog.IntPtr(100)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			searched++
			return catalog.Page[catalog.Channel]{}, nil
		},
	}

	m := New(mustCatalog(t, i18n.LangPTBR), WithSource(src))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = pressKeyMsg(m, m.Init()())

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "team")
	if m.filterHasNoMatches() {
		t.Fatal("expected local match for team → Teamfight Tactics")
	}
	if m.HighlightedID() != "513143" {
		t.Fatalf("highlight = %q, want 513143", m.HighlightedID())
	}

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter with match must open the highlighted category")
	}
	if searched != 0 {
		t.Fatalf("Enter with match must not start search, searched=%d", searched)
	}
	m, _ = pressKeyMsg(m, cmd())
	if opened != "513143" {
		t.Fatalf("opened = %q, want 513143", opened)
	}
	if m.Mode() != ModeChannels {
		t.Fatalf("mode = %q, want channels", m.Mode())
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "TFTPro") {
		t.Fatalf("opened category channels missing TFTPro:\n%s", view)
	}
}
