package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

type fakeStore struct {
	savedState store.State
	saveCalls  int
}

func (fs *fakeStore) Save(state store.State) error {
	fs.savedState = state
	fs.saveCalls++
	return nil
}

func TestFavoritesToggling_ReflectedInListAndStore(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	fs := &fakeStore{}

	m := New(catEN,
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "streamer_fav", DisplayName: "Favorite Streamer"},
		}),
	)

	// Add channels in ModeChannels
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		{ID: "ch-1", Login: "streamer_normal", DisplayName: "Normal Streamer", Viewers: twitch.IntPtr(500)},
	}
	m.reconcileHighlight()

	// Initially: 1 favorite ("streamer_fav"), 1 channel ("streamer_normal")
	items := m.VisibleItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 visible items, got %d", len(items))
	}
	if items[0].Type != ItemTypeFavorite || items[0].Login() != "streamer_fav" {
		t.Fatalf("expected first item to be favorite 'streamer_fav', got %+v", items[0])
	}
	if items[1].Type != ItemTypeChannel || items[1].Login() != "streamer_normal" {
		t.Fatalf("expected second item to be channel 'streamer_normal', got %+v", items[1])
	}

	// Move highlight down to normal streamer
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "ch-1" {
		t.Fatalf("expected highlight on ch-1, got %q", m.HighlightedID())
	}

	// Press 'f' to favorite streamer_normal
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if cmd != nil {
		_ = cmd()
	}

	if fs.saveCalls != 1 {
		t.Fatalf("expected store save called once, got %d", fs.saveCalls)
	}
	favs := m.Favorites()
	if len(favs) != 2 {
		t.Fatalf("expected 2 favorites after adding, got %d", len(favs))
	}
	if favs[1].Login != "streamer_normal" {
		t.Fatalf("expected streamer_normal in favorites, got %+v", favs[1])
	}

	// Now unfavorite streamer_fav
	// Move highlight to top (streamer_fav)
	m.moveHighlight(-2)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(Model)
	if cmd != nil {
		_ = cmd()
	}

	if fs.saveCalls != 2 {
		t.Fatalf("expected store save called twice, got %d", fs.saveCalls)
	}
	favs = m.Favorites()
	if len(favs) != 1 || favs[0].Login != "streamer_normal" {
		t.Fatalf("expected only streamer_normal in favorites, got %+v", favs)
	}
}

func TestListOrder_FavoritesRecentsContent(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "streamer_fav1"},
			{ID: "fav-2", Login: "streamer_fav2"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-1", Login: "streamer_rec1"},
			{ID: "rec-2", Login: "streamer_rec2"},
		}),
	)

	m.list.mode = ModeCategories
	m.list.categories = []twitch.Category{
		{ID: "cat-1", Name: "Category 1"},
		{ID: "cat-2", Name: "Category 2"},
	}

	items := m.VisibleItems()
	if len(items) != 6 {
		t.Fatalf("expected 6 visible items, got %d", len(items))
	}

	expectedTypes := []ListItemType{
		ItemTypeFavorite,
		ItemTypeFavorite,
		ItemTypeRecent,
		ItemTypeRecent,
		ItemTypeCategory,
		ItemTypeCategory,
	}

	expectedIDs := []string{
		"fav-1",
		"fav-2",
		"rec-1",
		"rec-2",
		"cat-1",
		"cat-2",
	}

	for i := range items {
		if items[i].Type != expectedTypes[i] {
			t.Errorf("item[%d] type = %q, want %q", i, items[i].Type, expectedTypes[i])
		}
		if items[i].ID() != expectedIDs[i] {
			t.Errorf("item[%d] ID = %q, want %q", i, items[i].ID(), expectedIDs[i])
		}
	}
}

func TestNoDuplicatesAcrossSections(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	// "shared_streamer" is in Favorites AND in Recents AND in loaded category channels
	// "recent_shared" is in Recents AND in loaded category channels
	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "ch-shared", Login: "shared_streamer", DisplayName: "Shared Streamer"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "ch-shared", Login: "shared_streamer", DisplayName: "Shared Streamer"},
			{ID: "ch-recent", Login: "recent_shared", DisplayName: "Recent Shared"},
		}),
	)

	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		{ID: "ch-shared", Login: "shared_streamer", DisplayName: "Shared Streamer", Viewers: twitch.IntPtr(1000)},
		{ID: "ch-recent", Login: "recent_shared", DisplayName: "Recent Shared", Viewers: twitch.IntPtr(500)},
		{ID: "ch-unique", Login: "unique_streamer", DisplayName: "Unique Streamer", Viewers: twitch.IntPtr(200)},
	}

	items := m.VisibleItems()
	// Should have:
	// 1: shared_streamer (as Favorite)
	// 2: recent_shared (as Recent)
	// 3: unique_streamer (as Channel)
	if len(items) != 3 {
		t.Fatalf("expected 3 items without duplicates, got %d", len(items))
	}

	if items[0].Type != ItemTypeFavorite || items[0].Login() != "shared_streamer" {
		t.Errorf("expected items[0] to be Favorite 'shared_streamer', got %+v", items[0])
	}
	if items[1].Type != ItemTypeRecent || items[1].Login() != "recent_shared" {
		t.Errorf("expected items[1] to be Recent 'recent_shared', got %+v", items[1])
	}
	if items[2].Type != ItemTypeChannel || items[2].Login() != "unique_streamer" {
		t.Errorf("expected items[2] to be Channel 'unique_streamer', got %+v", items[2])
	}
}

func TestRenamedRecentIsSeenByID(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	// The store kept the favorite under its old login; the recent has the same
	// ID with the new one. No listing is loaded to resolve either.
	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "ch-1", Login: "old_login", DisplayName: "Old Login"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "ch-1", Login: "new_login", DisplayName: "New Login"},
		}),
	)

	items := m.VisibleItems()
	if len(items) != 1 {
		t.Fatalf("expected the recent to be skipped by ID, got %d items", len(items))
	}
	if items[0].Type != ItemTypeFavorite || items[0].Login() != "old_login" {
		t.Errorf("expected items[0] to be Favorite 'old_login', got %+v", items[0])
	}
}

func TestFilteredRecentDoesNotHideListedChannel(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	m := New(catEN,
		WithRecents([]twitch.Channel{
			{ID: "ch-1", Login: "quiet", DisplayName: "Quiet"},
		}),
	)

	// A search hit without availability evidence does not replace the stored
	// recent, so the text filter sees the recent without a title.
	title := "Speedrun any%"
	m.list.mode = ModeSearch
	m.list.channels = []twitch.Channel{
		{ID: "ch-1", Login: "quiet", DisplayName: "Quiet", Title: &title},
	}
	m.list.filter = "speedrun"

	items := m.VisibleItems()
	if len(items) != 1 {
		t.Fatalf("expected only the listed channel, got %d items", len(items))
	}
	if items[0].Type != ItemTypeChannel || items[0].Login() != "quiet" {
		t.Errorf("expected items[0] to be Channel 'quiet', got %+v", items[0])
	}

	m.list.filter = "zzz"
	if items := m.VisibleItems(); items != nil {
		t.Errorf("expected a nil list when nothing matches, got %+v", items)
	}
}

func TestEnterSemanticsByItemType(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	fs := &fakeStore{}

	m := New(catEN,
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "streamer_fav", DisplayName: "Fav"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-1", Login: "streamer_rec1", DisplayName: "Rec1"},
			{ID: "rec-2", Login: "streamer_rec2", DisplayName: "Rec2"},
		}),
	)
	m.list.categories = []twitch.Category{
		{ID: "cat-1", Name: "Category 1"},
	}
	m.reconcileHighlight()

	// 1. Enter on Favorite: selects channel; visible recents stay put until next load
	orderBefore := visibleLogins(m.VisibleItems())
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected command from Enter on favorite")
	}
	msg := cmd()
	selMsg, ok := msg.(ChannelSelectedMsg)
	if !ok || selMsg.Channel.Login != "streamer_fav" {
		t.Fatalf("expected ChannelSelectedMsg for streamer_fav, got %+v", msg)
	}
	if got := visibleLogins(m.VisibleItems()); !equalStringSlices(got, orderBefore) {
		t.Fatalf("visible order changed on select: before=%v after=%v", orderBefore, got)
	}
	updated, cmd = m.Update(selMsg)
	m = updated.(Model)
	if cmd != nil {
		_ = cmd()
	}
	if fs.saveCalls != 1 {
		t.Fatalf("expected store save once after channel open, got %d", fs.saveCalls)
	}
	if len(fs.savedState.Recents) == 0 || fs.savedState.Recents[0].Login != "streamer_fav" {
		t.Fatalf("store must record streamer_fav as newest recent, got %+v", fs.savedState.Recents)
	}
	if m.Recents()[0].Login != "streamer_rec1" {
		t.Fatalf("visible recents must stay frozen until next load, got %q", m.Recents()[0].Login)
	}

	// 2. Enter on Category: drills down to ModeChannels without recording recent
	// Move highlight down to Category 1 (it is 4th item: fav-1, rec-1, rec-2, cat-1)
	m.list.highlightedID = "cat-1"
	recentsBefore := len(m.Recents())
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.SelectedCategory() == nil || m.SelectedCategory().ID != "cat-1" {
		t.Fatalf("expected SelectedCategory = cat-1, got %+v", m.SelectedCategory())
	}
	if len(m.Recents()) != recentsBefore {
		t.Fatalf("Enter on Category must not record recent, count changed from %d to %d", recentsBefore, len(m.Recents()))
	}
}

func TestSavedChannelWithoutViewersIsUnverifiedNotOffline(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	// fav-off has no viewers and is not in the loaded listing: no evidence.
	// fav-on is in the loaded category listing with viewers: live.
	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "fav-off", Login: "offline_guy", DisplayName: "Offline Guy", Viewers: nil},
			{ID: "fav-on", Login: "online_guy", DisplayName: "Online Guy", Viewers: nil},
		}),
	)

	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		{ID: "fav-on", Login: "online_guy", DisplayName: "Online Guy", Viewers: twitch.IntPtr(1500), Title: twitch.StringPtr("Playing Game")},
	}

	items := m.VisibleItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 visible items, got %d", len(items))
	}

	if items[0].Availability != twitch.AvailabilityUnknown || items[0].Offline() {
		t.Errorf("items[0] (no evidence) availability = %v, want unknown", items[0].Availability)
	}
	if items[1].Availability != twitch.AvailabilityLive {
		t.Errorf("items[1] (in listing) availability = %v, want live", items[1].Availability)
	}

	plain := ansi.Strip(m.View())
	if strings.Contains(plain, "[offline]") {
		t.Fatalf("view must not claim [offline] without evidence:\n%s", plain)
	}
	if !strings.Contains(strings.Join(strings.Fields(plain), " "), "Offline Guy (offline_guy) [unverified]") {
		t.Fatalf("expected unverified tag for channel without evidence:\n%s", plain)
	}
	if !strings.Contains(plain, "1.5k") {
		t.Fatalf("expected view to contain live viewer count '1.5k':\n%s", plain)
	}
}

func TestOfflineTagRequiresExplicitEvidence(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN,
		WithRecents([]twitch.Channel{
			{ID: "r-off", Login: "gaulestv", DisplayName: "gaulestv"},
			{ID: "r-unk", Login: "xqc", DisplayName: "xQc"},
		}),
	)

	plain := ansi.Strip(m.View())
	if strings.Contains(plain, "[offline]") {
		t.Fatalf("no evidence yet, [offline] must not appear:\n%s", plain)
	}

	// A search proves gaulestv is not broadcasting (stream:null upstream).
	m.list.mode = ModeSearch
	m.list.search.query = "gaules"
	updated, _ := m.Update(SearchLoadedMsg{
		Generation: m.list.generation,
		Page: twitch.Page[twitch.Channel]{Items: []twitch.Channel{
			{ID: "r-off", Login: "gaulestv", DisplayName: "gaulestv", Availability: twitch.AvailabilityOffline},
		}},
	})
	m = updated.(Model)

	byLogin := map[string]ListItem{}
	for _, it := range m.VisibleItems() {
		byLogin[it.Login()] = it
	}
	if !byLogin["gaulestv"].Offline() {
		t.Fatalf("gaulestv with explicit evidence should be offline, got %v", byLogin["gaulestv"].Availability)
	}
	if byLogin["xqc"].Availability != twitch.AvailabilityUnknown {
		t.Fatalf("xqc without evidence should stay unknown, got %v", byLogin["xqc"].Availability)
	}

	plain = ansi.Strip(m.View())
	if !strings.Contains(strings.Join(strings.Fields(plain), " "), "gaulestv [offline]") {
		t.Fatalf("expected [offline] for channel with evidence:\n%s", plain)
	}
	if strings.Contains(strings.Join(strings.Fields(plain), " "), "xQc [offline]") {
		t.Fatalf("xqc must not be shown offline without evidence:\n%s", plain)
	}
}

func TestPlayingChannelIsLiveRegardlessOfStore(t *testing.T) {
	catPT, _ := i18n.New(i18n.LangPTBR)
	m := New(catPT,
		WithRecents([]twitch.Channel{
			// Store only carries identity; worst case it even says offline.
			{ID: "r-1", Login: "eslcs", DisplayName: "ESLCS", Availability: twitch.AvailabilityOffline},
		}),
	)
	m.player.login = "ESLCS"

	for _, st := range []PlayerState{PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak} {
		m.player.state = st
		items := m.VisibleItems()
		if len(items) != 1 || items[0].Availability != twitch.AvailabilityLive {
			t.Fatalf("state %q: playing channel availability = %+v, want live", st, items)
		}
		plain := ansi.Strip(m.View())
		if strings.Contains(plain, "[offline]") {
			t.Fatalf("state %q: playing channel shown offline:\n%s", st, plain)
		}
		if !strings.Contains(plain, "[vivo]") || !strings.Contains(plain, "ESLCS") {
			t.Fatalf("state %q: playing channel must read as live:\n%s", st, plain)
		}
	}

	// Once the player is gone the stored evidence is back in charge.
	m.player.state = PlayerStateEnded
	if items := m.VisibleItems(); !items[0].Offline() {
		t.Fatalf("after playback ends availability = %v, want stored offline", items[0].Availability)
	}
}

func TestLoadedCategoryRefreshesSavedAvailability(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN,
		WithFavorites([]twitch.Channel{{ID: "f-1", Login: "gaules", DisplayName: "Gaules"}}),
		WithRecents([]twitch.Channel{
			{ID: "r-1", Login: "strogo", DisplayName: "StRoGo"},
			{ID: "r-2", Login: "elsewhere", DisplayName: "Elsewhere"},
		}),
	)
	cat := twitch.Category{ID: "cs", Name: "Counter-Strike"}
	m.list.mode = ModeChannels
	m.list.selectedCategory = &cat

	updated, _ := m.Update(ChannelsLoadedMsg{
		Generation: m.list.generation,
		Category:   cat,
		Page: twitch.Page[twitch.Channel]{Items: []twitch.Channel{
			{ID: "f-1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(12900), Title: twitch.StringPtr("Major"), Availability: twitch.AvailabilityLive},
			{ID: "r-1", Login: "strogo", DisplayName: "StRoGo", Viewers: twitch.IntPtr(30400), Availability: twitch.AvailabilityLive},
		}},
	})
	m = updated.(Model)

	favs := m.Favorites()
	if favs[0].Availability != twitch.AvailabilityLive || favs[0].Viewers == nil || *favs[0].Viewers != 12900 {
		t.Fatalf("favorite not refreshed from listing: %+v", favs[0])
	}
	recs := m.Recents()
	if recs[0].Availability != twitch.AvailabilityLive {
		t.Fatalf("recent strogo not refreshed: %+v", recs[0])
	}
	if recs[1].Availability != twitch.AvailabilityUnknown {
		t.Fatalf("channel absent from listing must stay unknown, got %v", recs[1].Availability)
	}

	// Leaving the category keeps the evidence for the saved channels.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "12.9k  Gaules — Major") {
		t.Fatalf("favorite should keep live status after leaving category:\n%s", plain)
	}
	if !strings.Contains(plain, "30.4k  StRoGo") {
		t.Fatalf("recent should keep live status after leaving category:\n%s", plain)
	}
	if !strings.Contains(strings.Join(strings.Fields(plain), " "), "Elsewhere [unverified]") || strings.Contains(plain, "[offline]") {
		t.Fatalf("channel outside the listing must be unverified, never offline:\n%s", plain)
	}

	// Persistence keeps identity only: no stale availability on disk.
	st := m.list.toStoreState()
	if got := st.Favorites[0].ToTwitch().Availability; got != twitch.AvailabilityUnknown {
		t.Fatalf("store must not persist availability, got %v", got)
	}
}

func TestReturnToCategoriesDropsListing(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN,
		WithFavorites([]twitch.Channel{{ID: "f-1", Login: "gaules", DisplayName: "Gaules", Language: twitch.StringPtr("pt")}}),
	)
	cat := twitch.Category{ID: "cs", Name: "Counter-Strike"}
	m.list.mode = ModeChannels
	m.list.selectedCategory = &cat
	updated, _ := m.Update(ChannelsLoadedMsg{
		Generation: m.list.generation,
		Category:   cat,
		Page: twitch.Page[twitch.Channel]{Items: []twitch.Channel{
			{ID: "f-1", Login: "gaules", DisplayName: "Gaules", Language: twitch.StringPtr("pt"), Viewers: twitch.IntPtr(12900), Availability: twitch.AvailabilityLive},
			{ID: "c-2", Login: "eslcs", DisplayName: "ESLCS", Language: twitch.StringPtr("en"), Viewers: twitch.IntPtr(30400), Availability: twitch.AvailabilityLive},
		}},
	})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Mode() != ModeCategories {
		t.Fatalf("Esc should return to categories, mode = %v", m.Mode())
	}

	// The hydration after Esc says the favorite went offline.
	updated, _ = m.Update(SavedAvailabilityMsg{
		Generation: m.list.hydrateGen,
		Channels:   []twitch.Channel{{ID: "f-1", Login: "gaules", DisplayName: "Gaules", Availability: twitch.AvailabilityOffline}},
	})
	m = updated.(Model)
	items := m.VisibleItems()
	if len(items) != 1 || !items[0].Offline() {
		t.Errorf("favorite must show the newest availability (offline), got %+v", items)
	}

	// The language menu counts only what is on screen: the favorite.
	langs := m.list.languagesInCurrentList()
	if len(langs) != 1 || langs[0].Code != "pt" || langs[0].Count != 1 {
		t.Errorf("language counts outside the screen: %+v", langs)
	}
}

func TestRetryFallingToCategoriesDropsListing(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN, WithSource(&fakeSource{}))
	m.list.mode = ModeSearch
	m.list.channels = []twitch.Channel{{ID: "c-1", Login: "eslcs", Availability: twitch.AvailabilityLive}}

	updated, _ := m.retryCurrentMode()
	m = updated.(Model)
	if m.Mode() != ModeCategories {
		t.Fatalf("retry without query should fall to categories, mode = %v", m.Mode())
	}
	if len(m.Channels()) != 0 {
		t.Fatalf("listing must be dropped on the way to categories, got %+v", m.Channels())
	}
}

func TestAvailabilityLabelsFitIn120And80Columns(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		cat, _ := i18n.New(lang)
		m := New(cat,
			WithFavorites([]twitch.Channel{{ID: "f-1", Login: "gaules", DisplayName: "Gaules"}}),
			WithRecents([]twitch.Channel{
				{ID: "r-1", Login: "eslcs", DisplayName: "ESLCS"},
				{ID: "r-2", Login: "gaulestv", DisplayName: "gaulestv", Availability: twitch.AvailabilityOffline},
			}),
		)
		m.chat.panel.channel = "eslcs"
		m.player.login = "eslcs"
		m.player.state = PlayerStatePlaying
		unverified := cat.T("channels.unverified_tag")
		live := cat.T("channels.live_compact")

		for _, width := range []int{120, 80} {
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			mw := updated.(Model)
			plain := ansi.Strip(mw.View())
			for i, line := range strings.Split(plain, "\n") {
				if w := ansi.StringWidth(line); w > width {
					t.Fatalf("%s %d cols: line %d too wide (%d): %q", lang, width, i, w, line)
				}
			}
			// Status tags stay intact at 80 columns; the identity/title truncates first.
			for _, want := range []string{
				unverified,
				live,
				"ESLCS",
				"[offline]",
			} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%s %d cols: missing %q:\n%s", lang, width, want, plain)
				}
			}
			if strings.Contains(plain, unverified+"…") || strings.Contains(plain, "[offlin…") {
				t.Fatalf("%s %d cols: status tag truncated:\n%s", lang, width, plain)
			}
		}
	}
}

func TestSectionsRenderIn120And79Columns(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(12000)},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-1", Login: "alanzoka", DisplayName: "alanzoka", Viewers: nil},
		}),
	)
	m.list.categories = []twitch.Category{
		{ID: "cat-1", Name: "Counter-Strike", Viewers: twitch.IntPtr(50000)},
	}

	// 1. Render in 120 columns
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m120 := updated.(Model)
	plain120 := ansi.Strip(m120.View())

	if !strings.Contains(plain120, "Favorites") {
		t.Fatalf("expected 120-col view to contain 'Favorites' section header:\n%s", plain120)
	}
	if !strings.Contains(plain120, "Recent") {
		t.Fatalf("expected 120-col view to contain 'Recent' section header:\n%s", plain120)
	}
	if !strings.Contains(plain120, "Live Categories") {
		t.Fatalf("expected 120-col view to contain 'Live Categories' section header:\n%s", plain120)
	}

	for i, line := range strings.Split(plain120, "\n") {
		if w := ansi.StringWidth(line); w > 120 {
			t.Fatalf("120-col line %d exceeded width: %d > 120: %q", i, w, line)
		}
	}

	// 2. Render in 79 columns
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 79, Height: 20})
	m79 := updated.(Model)
	plain79 := ansi.Strip(m79.View())

	if !strings.Contains(plain79, "Favorites") {
		t.Fatalf("expected 79-col view to contain 'Favorites' section header:\n%s", plain79)
	}
	if !strings.Contains(plain79, "Recent") {
		t.Fatalf("expected 79-col view to contain 'Recent' section header:\n%s", plain79)
	}
	for i, line := range strings.Split(plain79, "\n") {
		if w := ansi.StringWidth(line); w > 79 {
			t.Fatalf("79-col line %d exceeded width: %d > 79: %q", i, w, line)
		}
	}
}

func TestCorruptStateWarnsOnceInStatusBar(t *testing.T) {
	catPT, _ := i18n.New(i18n.LangPTBR)

	notice := catPT.T("store.corrupt_notice")
	m := New(catPT, WithCorruptNotice(notice))

	if m.CorruptNotice() != notice {
		t.Fatalf("expected CorruptNotice() = %q, got %q", notice, m.CorruptNotice())
	}

	plain := ansi.Strip(m.StatusBar())
	if !strings.Contains(plain, notice) {
		t.Fatalf("expected status bar to show corrupt notice:\n%s", plain)
	}

	// Pressing any key clears the notice
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)

	if m.CorruptNotice() != "" {
		t.Fatalf("expected CorruptNotice cleared after keypress, got %q", m.CorruptNotice())
	}
	plainAfter := ansi.Strip(m.StatusBar())
	if strings.Contains(plainAfter, notice) {
		t.Fatalf("expected status bar to not contain notice after keypress:\n%s", plainAfter)
	}
}

func TestSearchFavoritesRecentsAndLiveLabelsConsistent(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New: %v", err)
		}
		favCh := twitch.Channel{ID: "f-1", Login: "favchannel", DisplayName: "FavChannel"}
		recCh := twitch.Channel{ID: "r-1", Login: "recchannel", DisplayName: "RecChannel"}
		liveCh := twitch.Channel{
			ID:           "s-1",
			Login:        "livechannel",
			DisplayName:  "LiveChannel",
			Viewers:      twitch.IntPtr(1500),
			Title:        twitch.StringPtr("Live Title"),
			Availability: twitch.AvailabilityLive,
		}

		m := New(cat,
			WithFavorites([]twitch.Channel{favCh}),
			WithRecents([]twitch.Channel{recCh}),
		)

		// 1. In Search Mode:
		m.list.mode = ModeSearch
		m.list.search.query = "channel"
		m.list.channels = []twitch.Channel{liveCh}
		m.reconcileHighlight()

		plainSearch := ansi.Strip(m.View())
		lines := strings.Split(plainSearch, "\n")

		findLine := func(all []string, login string) string {
			for _, line := range all {
				if strings.Contains(strings.ToLower(line), login) {
					return line
				}
			}
			return ""
		}

		favLine := findLine(lines, "favchannel")
		recLine := findLine(lines, "recchannel")
		liveLine := findLine(lines, "livechannel")

		if favLine == "" || recLine == "" || liveLine == "" {
			t.Fatalf("%s: missing lines in search view:\n%s", lang, plainSearch)
		}

		unverifiedTag := cat.T("channels.unverified_tag")
		viewersUnknown := cat.T("channels.viewers_unknown")

		// Favorite and recent must show the unknown viewers placeholder and unverified availability tag
		if !strings.Contains(favLine, viewersUnknown) || !strings.Contains(favLine, unverifiedTag) {
			t.Fatalf("%s: favLine missing viewers or availability tag: %q", lang, favLine)
		}
		if !strings.Contains(recLine, viewersUnknown) || !strings.Contains(recLine, unverifiedTag) {
			t.Fatalf("%s: recLine missing viewers or availability tag: %q", lang, recLine)
		}
		if !strings.Contains(liveLine, "1.5k") || !strings.Contains(liveLine, "Live Title") {
			t.Fatalf("%s: liveLine missing viewers or title: %q", lang, liveLine)
		}

		// All three lines share the same marker, viewers and identity columns.
		lineRe := regexp.MustCompile(`^[ ▸★]{1,2}\s+(?:●\s+\S+|—)\s{2}\S+.*$`)
		for name, line := range map[string]string{"favorite": favLine, "recent": recLine, "live": liveLine} {
			if !lineRe.MatchString(strings.TrimRight(line, " ")) {
				t.Fatalf("%s: %s line does not match consistent row format: %q", lang, name, line)
			}
		}

		// 2. The same channel has the same line (except highlight marker) in search and category modes
		mCat := New(cat,
			WithFavorites([]twitch.Channel{favCh}),
			WithRecents([]twitch.Channel{recCh}),
		)
		mCat.list.mode = ModeCategories
		mCat.list.categories = []twitch.Category{{ID: "c-1", Name: "Games"}}
		mCat.list.highlightedID = "c-1"

		plainCat := ansi.Strip(mCat.View())
		catLines := strings.Split(plainCat, "\n")

		// In search, highlight is on search result, so fav and rec have "  " marker
		m.list.highlightedID = "s-1"
		plainSearchNoHighlight := ansi.Strip(m.View())
		searchLines := strings.Split(plainSearchNoHighlight, "\n")

		catFav := findLine(catLines, "favchannel")
		searchFav := findLine(searchLines, "favchannel")
		if catFav != searchFav {
			t.Fatalf("%s: favorite row differs between category and search:\ncategory: %q\nsearch:   %q", lang, catFav, searchFav)
		}

		catRec := findLine(catLines, "recchannel")
		searchRec := findLine(searchLines, "recchannel")
		if catRec != searchRec {
			t.Fatalf("%s: recent row differs between category and search:\ncategory: %q\nsearch:   %q", lang, catRec, searchRec)
		}

		// Also check an offline channel and a live channel with viewers
		offCh := twitch.Channel{ID: "off-1", Login: "offchannel", DisplayName: "OffChannel", Availability: twitch.AvailabilityOffline}
		liveFav := twitch.Channel{ID: "favlive-1", Login: "favlive", DisplayName: "FavLive", Viewers: twitch.IntPtr(5200), Title: twitch.StringPtr("Playing Game"), Availability: twitch.AvailabilityLive}

		mDualCat := New(cat, WithFavorites([]twitch.Channel{liveFav}), WithRecents([]twitch.Channel{offCh}))
		mDualCat.list.mode = ModeCategories
		mDualCat.list.categories = []twitch.Category{{ID: "c-1", Name: "Games"}}
		mDualCat.list.highlightedID = "c-1"

		mDualSearch := New(cat, WithFavorites([]twitch.Channel{liveFav}), WithRecents([]twitch.Channel{offCh}))
		mDualSearch.list.mode = ModeSearch
		mDualSearch.list.search.query = "channel"
		mDualSearch.list.channels = []twitch.Channel{liveCh}
		mDualSearch.list.highlightedID = "s-1"

		plainDualCat := ansi.Strip(mDualCat.View())
		plainDualSearch := ansi.Strip(mDualSearch.View())

		if findLine(strings.Split(plainDualCat, "\n"), "offchannel") != findLine(strings.Split(plainDualSearch, "\n"), "offchannel") {
			t.Fatalf("%s: offline channel row differs between category and search", lang)
		}
		if findLine(strings.Split(plainDualCat, "\n"), "favlive") != findLine(strings.Split(plainDualSearch, "\n"), "favlive") {
			t.Fatalf("%s: live favorite row differs between category and search", lang)
		}

		// 3. 80 and 120 columns without overflowing width
		for _, width := range []int{80, 120} {
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			mw := updated.(Model)
			plain := ansi.Strip(mw.View())
			for i, l := range strings.Split(plain, "\n") {
				if w := ansi.StringWidth(l); w > width {
					t.Fatalf("%s %d cols: line %d exceeded width (%d > %d): %q", lang, width, i, w, width, l)
				}
			}
		}
	}
}

func TestHydrateSavedAvailabilityOnInit(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	var calls int32
	var gotLogins []string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "jc", Name: "Just Chatting"}},
			}, nil
		},
		channelsByLoginsFn: func(ctx context.Context, logins []string) ([]twitch.Channel, error) {
			atomic.AddInt32(&calls, 1)
			gotLogins = append([]string(nil), logins...)
			return []twitch.Channel{
				{
					ID:           "1",
					Login:        "gaules",
					DisplayName:  "Gaules",
					Viewers:      twitch.IntPtr(12900),
					Title:        twitch.StringPtr("Major"),
					Availability: twitch.AvailabilityLive,
				},
				{
					ID:           "2",
					Login:        "xqc",
					DisplayName:  "xQc",
					Availability: twitch.AvailabilityOffline,
				},
			}, nil
		},
	}

	m := New(cat,
		WithSource(src),
		WithFavorites([]twitch.Channel{
			{ID: "f-1", Login: "gaules", DisplayName: "Gaules"},
			{ID: "f-2", Login: "elsewhere", DisplayName: "Elsewhere"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "r-1", Login: "xqc", DisplayName: "xQc"},
			{ID: "r-2", Login: "gaules", DisplayName: "Gaules"},
		}),
	)

	updated, cmd := m.Update(m.Init()())
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("ChannelsByLogins calls = %d, want 1", calls)
	}
	if len(gotLogins) != 3 {
		t.Fatalf("logins = %v, want 3 unique (gaules, elsewhere, xqc)", gotLogins)
	}

	favs := m.Favorites()
	if favs[0].Availability != twitch.AvailabilityLive || favs[0].Viewers == nil || *favs[0].Viewers != 12900 {
		t.Fatalf("favorite gaules not hydrated: %+v", favs[0])
	}
	if favs[0].Title == nil || *favs[0].Title != "Major" {
		t.Fatalf("favorite gaules title not hydrated: %+v", favs[0])
	}
	if favs[1].Availability != twitch.AvailabilityUnknown {
		t.Fatalf("elsewhere absent from batch must stay unknown, got %v", favs[1].Availability)
	}

	recs := m.Recents()
	if recs[0].Availability != twitch.AvailabilityOffline || recs[0].Viewers != nil {
		t.Fatalf("recent xqc not hydrated offline: %+v", recs[0])
	}
	if recs[1].Availability != twitch.AvailabilityLive {
		t.Fatalf("recent gaules not hydrated live: %+v", recs[1])
	}

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "12.9k") || !strings.Contains(plain, "Major") {
		t.Fatalf("view missing hydrated live favorite:\n%s", plain)
	}
	if !strings.Contains(plain, "[offline]") {
		t.Fatalf("view missing offline recent:\n%s", plain)
	}
	if !strings.Contains(plain, "[unverified]") {
		t.Fatalf("elsewhere must stay unverified:\n%s", plain)
	}
}

func TestHydrateEmptySavedDoesNotRequest(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	var calls int32
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "jc", Name: "Just Chatting"}},
			}, nil
		},
		channelsByLoginsFn: func(ctx context.Context, logins []string) ([]twitch.Channel, error) {
			atomic.AddInt32(&calls, 1)
			return nil, nil
		},
	}

	m := New(cat, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("ChannelsByLogins calls = %d, want 0 for empty saved set", calls)
	}
	if len(m.Categories()) != 1 {
		t.Fatalf("categories not loaded: %+v", m.Categories())
	}
}

func TestHydrateFailureKeepsUnverified(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "jc", Name: "Just Chatting"}},
			}, nil
		},
		channelsByLoginsFn: func(ctx context.Context, logins []string) ([]twitch.Channel, error) {
			return nil, errors.New("upstream down")
		},
	}

	m := New(cat,
		WithSource(src),
		WithFavorites([]twitch.Channel{{ID: "f-1", Login: "gaules", DisplayName: "Gaules"}}),
		WithRecents([]twitch.Channel{{ID: "r-1", Login: "xqc", DisplayName: "xQc"}}),
	)

	updated, follow := m.Update(m.Init()())
	m = updated.(Model)
	if follow != nil {
		updated, _ = m.Update(follow())
		m = updated.(Model)
	}

	if m.Err() != nil {
		t.Fatalf("hydrate failure must not set model err, got %v", m.Err())
	}
	if m.Favorites()[0].Availability != twitch.AvailabilityUnknown {
		t.Fatalf("favorite changed on failure: %+v", m.Favorites()[0])
	}
	if m.Recents()[0].Availability != twitch.AvailabilityUnknown {
		t.Fatalf("recent changed on failure: %+v", m.Recents()[0])
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "[unverified]") {
		t.Fatalf("expected unverified after hydrate failure:\n%s", plain)
	}
	if strings.Contains(plain, "upstream down") {
		t.Fatalf("hydrate failure must not surface aggressive error text:\n%s", plain)
	}
}

func TestCaptureSearchScreenPTY(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	favCh := twitch.Channel{ID: "f-1", Login: "gaules", DisplayName: "Gaules"}
	recCh := twitch.Channel{ID: "r-1", Login: "alanzoka", DisplayName: "alanzoka"}
	liveCh := twitch.Channel{
		ID:           "s-1",
		Login:        "cellbit",
		DisplayName:  "Cellbit",
		Viewers:      twitch.IntPtr(15420),
		Title:        twitch.StringPtr("Enigmas e RPG ao vivo"),
		Availability: twitch.AvailabilityLive,
	}

	m := New(cat,
		WithFavorites([]twitch.Channel{favCh}),
		WithRecents([]twitch.Channel{recCh}),
	)

	m.list.mode = ModeSearch
	m.list.search.query = "cellbit"
	m.list.channels = []twitch.Channel{liveCh}
	m.reconcileHighlight()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)

	out := m.View()
	t.Logf("\n%s\n", out)
	fmt.Printf("\n--- PTY CAPTURE START ---\n%s\n--- PTY CAPTURE END ---\n", out)
}

func TestRemovingFavoriteKeepsEarlierModelSlice(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	fs := &fakeStore{}
	m := New(catEN,
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "1", Login: "alpha"},
			{ID: "2", Login: "bravo"},
			{ID: "3", Login: "charlie"},
		}),
	)
	earlier := m
	kept := m.list.favorites

	if m.ToggleFavorite(twitch.Channel{ID: "2", Login: "bravo"}) {
		t.Fatal("ToggleFavorite added bravo instead of removing it")
	}

	logins := func(chs []twitch.Channel) string {
		out := make([]string, len(chs))
		for i, ch := range chs {
			out[i] = ch.Login
		}
		return strings.Join(out, ",")
	}
	if got := logins(kept); got != "alpha,bravo,charlie" {
		t.Errorf("slice kept before removal = %s, want alpha,bravo,charlie", got)
	}
	if got := logins(earlier.Favorites()); got != "alpha,bravo,charlie" {
		t.Errorf("earlier model favorites = %s, want alpha,bravo,charlie", got)
	}
	if got := logins(m.Favorites()); got != "alpha,charlie" {
		t.Errorf("favorites after removal = %s, want alpha,charlie", got)
	}

	m.saveStoreCmd()()
	saved := make([]string, len(fs.savedState.Favorites))
	for i, ch := range fs.savedState.Favorites {
		saved[i] = ch.Login
	}
	if got := strings.Join(saved, ","); got != "alpha,charlie" {
		t.Errorf("persisted favorites = %s, want alpha,charlie", got)
	}
}
