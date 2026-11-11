package ui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// assertHighlightActs checks that the highlight sits on a visible row for
// wantLogin and that Enter and f act on that row.
func assertHighlightActs(t *testing.T, m Model, wantLogin string) {
	t.Helper()
	it, ok := m.HighlightedItem()
	if !ok || it.Channel == nil || it.Channel.Login != wantLogin {
		t.Fatalf("highlight = %q (visible=%v), want visible row %q; rows=%v",
			m.HighlightedID(), ok, wantLogin, visibleLogins(m.VisibleItems()))
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("Enter did nothing; want ChannelSelectedMsg for %q", wantLogin)
	}
	if sel, ok := cmd().(ChannelSelectedMsg); !ok || sel.Channel.Login != wantLogin {
		t.Fatalf("Enter = %+v, want ChannelSelectedMsg for %q", sel, wantLogin)
	}

	wasFavorite := false
	for _, fav := range m.Favorites() {
		if fav.Login == wantLogin {
			wasFavorite = true
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	isFavorite := false
	for _, fav := range updated.(Model).Favorites() {
		if fav.Login == wantLogin {
			isFavorite = true
		}
	}
	if isFavorite == wasFavorite {
		t.Fatalf("f did not toggle %q (favorite before=%v after=%v)", wantLogin, wasFavorite, isFavorite)
	}
}

// A persisted language filter hides a highlighted favorite only once the
// hydration reports its language. The categories arrive first, so the
// highlight was already placed on that favorite.
func TestHighlightLeavesFavoriteHiddenByHydration(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithChannelFilters([]string{"pt"}, 0, nil),
		WithFavorites([]twitch.Channel{
			{ID: "f-en", Login: "gringo", DisplayName: "Gringo"},
			{ID: "f-pt", Login: "brasileiro", DisplayName: "Brasileiro"},
		}),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(CategoriesLoadedMsg{
		Generation: m.list.generation,
		Page:       twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "jc", Name: "Just Chatting"}}},
	})
	m = updated.(Model)
	if it, ok := m.HighlightedItem(); !ok || it.Login() != "gringo" {
		t.Fatalf("before hydration highlight = %q, want gringo", m.HighlightedID())
	}

	updated, _ = m.Update(SavedAvailabilityMsg{
		Generation: m.list.hydrateGen,
		Channels: []twitch.Channel{
			{ID: "1", Login: "gringo", DisplayName: "Gringo", Language: twitch.StringPtr("en"),
				Viewers: twitch.IntPtr(900), Availability: twitch.AvailabilityLive},
			{ID: "2", Login: "brasileiro", DisplayName: "Brasileiro", Language: twitch.StringPtr("pt"),
				Viewers: twitch.IntPtr(400), Availability: twitch.AvailabilityLive},
		},
	})
	m = updated.(Model)

	assertHighlightActs(t, m, "brasileiro")
}

// With a minimum audience, the channel the player reports offline leaves the
// list. The highlight goes to its neighbor, not back to the top.
func TestHighlightMovesToNeighborWhenOfflineChannelLeavesAudienceFilter(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithPlayback(&fakePlayback{}),
		WithChannelFilters(nil, 1000, nil),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		liveChannel("a-id", "alpha", "Alpha", 20000),
		liveChannel("xqc-id", "xqc", "xQc", 13000),
		liveChannel("b-id", "beta", "Beta", 5000),
	}
	items := m.VisibleItems()
	m.setHighlightToItem(items, 1)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "xqc" {
		t.Fatalf("Enter = %+v, want xqc", sel)
	}
	updated, cmd = m.Update(sel)
	m = updated.(Model)
	m = drainCmd(t, m, cmd)

	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)

	assertHighlightActs(t, m, "beta")
}

func TestHighlightMovesToNeighborWhenRetryDropsHighlightedRecent(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	recents := make([]twitch.Channel, defaultMaxRecents)
	for i := 0; i < defaultMaxRecents; i++ {
		id := fmt.Sprintf("rec-%02d", i)
		recents[i] = twitch.Channel{ID: id, Login: id, DisplayName: id}
	}
	m := New(cat, WithRecents(recents), WithSource(&fakeSource{}))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)

	items := m.VisibleItems()
	m.setHighlightToItem(items, len(items)-1)
	if got := m.HighlightedID(); got != "rec-49" {
		t.Fatalf("setup: highlighted %q, want rec-49", got)
	}

	m.queueRecent(twitch.Channel{ID: "rec-new", Login: "rec-new", DisplayName: "New"})

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)

	updated, _ = m.Update(CategoriesLoadedMsg{Generation: m.list.generation, Err: loadFailure()})
	m = updated.(Model)

	if got := m.HighlightedID(); got != "rec-48" {
		t.Fatalf("highlight after retry failure = %q, want rec-48; rows=%v",
			got, visibleLogins(m.VisibleItems()))
	}

	// Under a load error, HighlightedItem returns no row.
	if it, ok := m.HighlightedItem(); ok {
		t.Fatalf("HighlightedItem under error = %+v, want none", it)
	}

	// Once the error is cleared, the highlight acts on the reconciled row.
	m.clearStaleStatusErrors()
	assertHighlightActs(t, m, "rec-48")
}
