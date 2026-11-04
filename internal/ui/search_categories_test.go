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
	if cmd = m.fetchCategoriesCmd(m.generation, "", false); cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestCtrlSSearchShowsCategoriesBeforeChannels(t *testing.T) {
	var searchedQ string
	var searchedCatsQ string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: twitch.IntPtr(100000)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			searchedQ = query
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "ch1", Login: "tft_streamer", DisplayName: "TFTStreamer", Availability: twitch.AvailabilityLive, Viewers: twitch.IntPtr(50)},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]twitch.Category, error) {
			searchedCatsQ = query
			return []twitch.Category{
				{ID: "528672376", Name: "Teamfight Manager 2", Viewers: twitch.IntPtr(3)},
				{ID: "513143", Name: "Teamfight Tactics", Viewers: twitch.IntPtr(4063)},
				{ID: "1049329847", Name: "Teamfight Manager", Viewers: nil},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "open1", Login: "opened", DisplayName: "Opened", Viewers: twitch.IntPtr(9)},
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
	if len(m.searchCategories) != 3 {
		t.Fatalf("searchCategories len = %d, want 3", len(m.searchCategories))
	}
	if m.searchCategories[0].Name != "Teamfight Tactics" {
		t.Fatalf("first category = %q, want Teamfight Tactics (exact/viewers rank)", m.searchCategories[0].Name)
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
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: twitch.IntPtr(1)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]twitch.Category, error) {
			return []twitch.Category{
				{ID: "513143", Name: "Teamfight Tactics", Viewers: twitch.IntPtr(4063)},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			openedCat = categoryID
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "c1", Login: "tftpro", DisplayName: "TFTPro", Viewers: twitch.IntPtr(100)},
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
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: twitch.IntPtr(1)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "ch1", Login: "onlychan", DisplayName: "OnlyChan"},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]twitch.Category, error) {
			return []twitch.Category{}, nil
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
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{
				{ID: "top1", Name: "Just Chatting", Viewers: twitch.IntPtr(1)},
				{ID: "top2", Name: "Counter-Strike", Viewers: twitch.IntPtr(2)},
			}}, nil
		},
	}
	m := newSearchCategoriesModel(t, src)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "teamfight" {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := ansi.Strip(m.View())
	hint := "Ctrl+F busca na Twitch: teamfight"
	if !strings.Contains(view, hint) {
		t.Fatalf("expected hint %q in view:\n%s", hint, view)
	}
}
