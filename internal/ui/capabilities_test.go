package ui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

type dummySearcher struct {
	searched bool
}

func (s *dummySearcher) Search(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
	s.searched = true
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{{ID: "res-1", Login: "result"}}}, nil
}

type dummyBrowser struct {
	categoriesCalled bool
}

func (b *dummyBrowser) Categories(ctx context.Context, req catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	b.categoriesCalled = true
	return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "cat-1", Name: "Category 1"}}}, nil
}

func (b *dummyBrowser) Channels(ctx context.Context, categoryID string, req catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{}, nil
}

func (b *dummyBrowser) SearchCategories(ctx context.Context, query string) ([]catalog.Category, error) {
	return nil, nil
}

type dummyResolver struct {
	called bool
}

func (r *dummyResolver) ChannelsByIDs(ctx context.Context, ids []string) ([]catalog.Channel, error) {
	r.called = true
	return nil, nil
}

func TestCatalogWithoutBrowserDoesNotShowCategories(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	searcher := &dummySearcher{}
	resolver := &dummyResolver{}

	// UI initialized with Searcher and Resolver, but without Browser capability.
	m := New(cat, WithSearcher(searcher), WithResolver(resolver))

	// Init should not issue any category fetch commands.
	cmd := m.Init()
	if cmd != nil {
		// Draining commands should not call any browser methods.
		m = drainCmd(t, m, cmd)
	}

	if len(m.Categories()) != 0 {
		t.Fatalf("expected 0 categories when Browser is missing, got %d", len(m.Categories()))
	}
	if len(m.VisibleCategories()) != 0 {
		t.Fatalf("expected 0 visible categories, got %d", len(m.VisibleCategories()))
	}

	for _, it := range m.VisibleItems() {
		if it.Type == ItemTypeCategory {
			t.Fatalf("expected no category items in list without Browser, got %+v", it)
		}
	}
}

func TestCatalogWithoutSearcherDoesNotSearchChannels(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	browser := &dummyBrowser{}
	resolver := &dummyResolver{}

	// UI initialized with Browser and Resolver, but without Searcher capability.
	m := New(cat, WithBrowser(browser), WithResolver(resolver))

	// Open search and submit query
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "testquery")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m = drainCmd(t, m, cmd)
	}

	// Mode should not have any searched channels since searcher is nil
	if len(m.Channels()) != 0 {
		t.Fatalf("expected 0 channels when Searcher is missing, got %d", len(m.Channels()))
	}
}

func TestUIStoreRoundTripPreservesNonTwitchPlatformAndURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := store.New(path)

	kickFav := store.Channel{
		Platform:    store.PlatformKick,
		ID:          "kick:xqc",
		Login:       "xqc",
		DisplayName: "xqc",
		URL:         "https://kick.com/xqc",
	}

	// 1. Save initial state with a Kick channel in favorites to disk.
	if err := s.Save(store.State{Favorites: []store.Channel{kickFav}}); err != nil {
		t.Fatalf("s.Save initial: %v", err)
	}

	// 2. Load state and start UI.
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("s.Load: %v", err)
	}
	cat, _ := i18n.New(i18n.LangEN)
	m := New(cat, WithStore(s), WithFavorites(store.ToChannels(loaded.Favorites)))

	// Check UI loaded the favorite with correct platform and URL.
	favs := m.Favorites()
	if len(favs) != 1 || favs[0].Platform != store.PlatformKick || favs[0].URL != "https://kick.com/xqc" {
		t.Fatalf("UI Favorites: %+v, want kick channel with URL", favs)
	}

	// 3. Add a new Twitch channel in ModeChannels and toggle favorite to trigger store save.
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{
		{Platform: store.PlatformTwitch, ID: "tw-1", Login: "streamer_tw", DisplayName: "Streamer TW"},
	}
	m.reconcileHighlight()

	// Highlight the channel and press 'f' to favorite it.
	m.list.highlightedID = "tw-1"
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = drainCmd(t, m, cmd)

	// 4. Reload from disk directly through store.LoadFrom.
	onDisk, err := store.LoadFrom(path)
	if err != nil {
		t.Fatalf("store.LoadFrom: %v", err)
	}

	if len(onDisk.Favorites) != 2 {
		t.Fatalf("expected 2 favorites on disk, got %d: %+v", len(onDisk.Favorites), onDisk.Favorites)
	}

	// Verify Kick channel's platform was NOT converted to Twitch and its URL was retained.
	var foundKick bool
	for _, f := range onDisk.Favorites {
		if f.Login == "xqc" {
			foundKick = true
			if f.Platform != store.PlatformKick {
				t.Fatalf("Kick channel platform was corrupted to %q, want %q", f.Platform, store.PlatformKick)
			}
			if f.URL != "https://kick.com/xqc" {
				t.Fatalf("Kick channel URL was corrupted to %q, want https://kick.com/xqc", f.URL)
			}
		}
	}
	if !foundKick {
		t.Fatalf("Kick channel was lost from favorites on disk: %+v", onDisk.Favorites)
	}
}

func TestWithoutBrowserReturningToCategoriesStillHydratesSavedChannels(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	resolver := &dummyResolver{}
	saved := []catalog.Channel{{ID: "kick:xqc", Login: "xqc", Platform: "kick", URL: "https://kick.com/xqc"}}
	m := New(cat, WithPlatformResolver("kick", resolver), WithFavorites(saved))

	for name, step := range map[string]func(Model) (tea.Model, tea.Cmd){
		"returnToCategories": Model.returnToCategories,
		"retryCurrentMode":   Model.retryCurrentMode,
	} {
		resolver.called = false
		_, cmd := step(m)
		if cmd == nil {
			t.Fatalf("%s without a Browser returned no command: saved channels would stay unverified", name)
		}
		cmd()
		if !resolver.called {
			t.Fatalf("%s without a Browser did not ask the Resolver again", name)
		}
	}
}
