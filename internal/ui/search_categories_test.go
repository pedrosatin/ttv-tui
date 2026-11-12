package ui

import (
	"context"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func newSearchCategoriesModel(t *testing.T, src *fakeSource) Model {
	t.Helper()
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithSource(src), WithPageSize(10))
	m.width = 100
	m.height = 24
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ = m.Update(msg)
			m = updated.(Model)
		}
	}
	// Load initial categories so / filter has a non-empty local list.
	if cmd = m.fetchCategoriesCmd(m.list.generation, "", false); cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestCtrlSSearchShowsCategoriesBeforeChannels(t *testing.T) {
	var searchedQ string
	var searchedCatsQ string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: catalog.IntPtr(100000)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			searchedQ = query
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch1", Login: "tft_streamer", DisplayName: "TFTStreamer", Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(50)},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			searchedCatsQ = query
			return []catalog.Category{
				{ID: "528672376", Name: "Teamfight Manager 2", Viewers: catalog.IntPtr(3)},
				{ID: "513143", Name: "Teamfight Tactics", Viewers: catalog.IntPtr(4063)},
				{ID: "1049329847", Name: "Teamfight Manager", Viewers: nil},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "open1", Login: "opened", DisplayName: "Opened", Viewers: catalog.IntPtr(9)},
			}}, nil
		},
	}

	m := newSearchCategoriesModel(t, src)
	updated, cmd := m.startSearch("teamfight")
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected search cmd")
	}
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if searchedQ != "teamfight" || searchedCatsQ != "teamfight" {
		t.Fatalf("queries: channels=%q cats=%q, want teamfight", searchedQ, searchedCatsQ)
	}
	if m.Mode() != ModeSearch {
		t.Fatalf("mode = %q, want search", m.Mode())
	}
	if len(m.list.search.categories) != 3 {
		t.Fatalf("searchCategories len = %d, want 3", len(m.list.search.categories))
	}
	if m.list.search.categories[0].Name != "Teamfight Tactics" {
		t.Fatalf("first category = %q, want Teamfight Tactics (exact/viewers rank)", m.list.search.categories[0].Name)
	}

	view := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE search teamfight:\n%s", view)
	catIdx := strings.Index(view, "Categorias")
	chanIdx := strings.Index(view, "Canais")
	if catIdx < 0 || chanIdx < 0 || catIdx > chanIdx {
		t.Fatalf("expected Categorias section before Canais; cat=%d chan=%d\n%s", catIdx, chanIdx, view)
	}
	if !strings.Contains(view, "Teamfight Tactics") || !strings.Contains(view, "TFTStreamer") {
		t.Fatalf("view missing category or channel hit:\n%s", view)
	}
	if !strings.Contains(view, "sem transmissão") {
		t.Fatalf("null-viewers category must show sem transmissão:\n%s", view)
	}
	if m.HighlightedID() != "513143" {
		t.Fatalf("highlight = %q, want first category 513143", m.HighlightedID())
	}
}

func TestEnterOnSearchCategoryOpensItsChannels(t *testing.T) {
	var openedCat string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: catalog.IntPtr(1)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			return []catalog.Category{
				{ID: "513143", Name: "Teamfight Tactics", Viewers: catalog.IntPtr(4063)},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			openedCat = categoryID
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "c1", Login: "tftpro", DisplayName: "TFTPro", Viewers: catalog.IntPtr(100)},
			}}, nil
		},
	}

	m := newSearchCategoriesModel(t, src)
	updated, cmd := m.startSearch("teamfight")
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	updated, cmd = m.selectHighlighted()
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected fetchChannelsCmd after Enter on category")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if openedCat != "513143" {
		t.Fatalf("opened category = %q, want 513143", openedCat)
	}
	if m.Mode() != ModeChannels {
		t.Fatalf("mode = %q, want channels", m.Mode())
	}
	if m.SelectedCategory() == nil || m.SelectedCategory().Name != "Teamfight Tactics" {
		t.Fatalf("SelectedCategory = %+v", m.SelectedCategory())
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "TFTPro") {
		t.Fatalf("channels list missing TFTPro:\n%s", view)
	}
}

func TestSearchWithNoCategoriesStillShowsChannels(t *testing.T) {
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: catalog.IntPtr(1)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch1", Login: "onlychan", DisplayName: "OnlyChan"},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			return []catalog.Category{}, nil
		},
	}

	m := newSearchCategoriesModel(t, src)
	updated, cmd := m.startSearch("onlychan")
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "OnlyChan") {
		t.Fatalf("missing channel:\n%s", view)
	}
	if strings.Contains(view, "Categorias") {
		t.Fatalf("empty categories section must not render:\n%s", view)
	}
}

func TestLocalFilterWithoutMatchSuggestsCtrlF(t *testing.T) {
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: catalog.IntPtr(1)},
				{ID: "top2", Name: "Counter-Strike", Viewers: catalog.IntPtr(2)},
			}}, nil
		},
	}
	m := newSearchCategoriesModel(t, src)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "teamfight" {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := ansi.Strip(m.View())
	hint := "Enter ou Ctrl+F busca na Twitch: teamfight"
	if !strings.Contains(view, hint) {
		t.Fatalf("expected hint %q in view:\n%s", hint, view)
	}
	if !strings.Contains(view, m.t("filter.empty")) {
		t.Fatalf("expected filter.empty in view:\n%s", view)
	}
}
