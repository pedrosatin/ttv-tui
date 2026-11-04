package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// openChannel drives Enter on the highlighted row through ChannelSelectedMsg,
// matching the real Bubble Tea path after selectHighlighted.
func openChannel(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected ChannelSelectedMsg cmd from Enter")
	}
	msg := cmd()
	sel, ok := msg.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("got %T, want ChannelSelectedMsg", msg)
	}
	updated, cmd = m.Update(sel)
	m = updated.(Model)
	if cmd != nil {
		_ = cmd()
	}
	return m
}

func TestOpenChannelDoesNotReorderVisibleList(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	fs := &fakeStore{}

	m := New(catEN,
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "fav_one", DisplayName: "Fav One"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-a", Login: "alice", DisplayName: "Alice"},
			{ID: "rec-b", Login: "bob", DisplayName: "Bob"},
			{ID: "rec-c", Login: "carol", DisplayName: "Carol"},
		}),
	)
	m.mode = ModeChannels
	m.channels = []twitch.Channel{
		{ID: "ch-x", Login: "xqc", DisplayName: "xQc", Viewers: twitch.IntPtr(100)},
	}
	m.highlightedID = "rec-c"
	m.reconcileHighlight()

	before := visibleLogins(m.VisibleItems())
	wantBefore := []string{"fav_one", "alice", "bob", "carol", "xqc"}
	if !equalStringSlices(before, wantBefore) {
		t.Fatalf("setup order = %v, want %v", before, wantBefore)
	}

	m = openChannel(t, m)

	after := visibleLogins(m.VisibleItems())
	if !equalStringSlices(after, before) {
		t.Fatalf("opening carol reordered the list: before=%v after=%v", before, after)
	}
	if m.HighlightedID() != "rec-c" {
		t.Fatalf("highlight = %q, want rec-c (same channel, same place)", m.HighlightedID())
	}
	if fs.saveCalls != 1 {
		t.Fatalf("store save calls = %d, want 1", fs.saveCalls)
	}
	if got := fs.savedState.Recents; len(got) < 1 || got[0].Login != "carol" {
		t.Fatalf("store recents = %+v, want carol first", got)
	}
	if m.Recents()[0].Login != "alice" {
		t.Fatalf("visible Recents()[0] = %q, want alice until next load", m.Recents()[0].Login)
	}
}

func TestEnterRightAfterOpenDoesNotReopenSameChannelByShift(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	m := New(catEN,
		WithRecents([]twitch.Channel{
			{ID: "rec-a", Login: "alice", DisplayName: "Alice"},
			{ID: "rec-b", Login: "bob", DisplayName: "Bob"},
			{ID: "rec-c", Login: "carol", DisplayName: "Carol"},
		}),
	)
	m.mode = ModeCategories
	m.categories = []twitch.Category{{ID: "cat-1", Name: "Just Chatting"}}
	m.highlightedID = "rec-b"
	m.reconcileHighlight()

	m = openChannel(t, m)
	if m.SelectedChannel() == nil || m.SelectedChannel().Login != "bob" {
		t.Fatalf("first open selected %+v, want bob", m.SelectedChannel())
	}

	// Move one row down: with a frozen list this is carol, not a shifted alice.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "rec-c" {
		t.Fatalf("after j highlight = %q, want rec-c", m.HighlightedID())
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected ChannelSelectedMsg")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "carol" {
		t.Fatalf("second Enter selected %+v, want carol (not bob again)", sel)
	}
}

func TestPendingRecentsApplyOnRefreshAndBackToCategories(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	src := &fakeSource{
		categoriesFn: func(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-1", Name: "Just Chatting"}}}, nil
		},
		channelsFn: func(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "ch-x", Login: "xqc", DisplayName: "xQc", Viewers: twitch.IntPtr(10)},
			}}, nil
		},
	}
	fs := &fakeStore{}

	m := New(catEN,
		WithSource(src),
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "fav-1", Login: "fav_one", DisplayName: "Fav One"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-a", Login: "alice", DisplayName: "Alice"},
			{ID: "rec-b", Login: "bob", DisplayName: "Bob"},
		}),
	)
	m.mode = ModeChannels
	m.selectedCategory = &twitch.Category{ID: "cat-1", Name: "Just Chatting"}
	m.channels = []twitch.Channel{
		{ID: "ch-x", Login: "xqc", DisplayName: "xQc", Viewers: twitch.IntPtr(10)},
	}
	m.highlightedID = "rec-b"
	m.reconcileHighlight()

	frozen := visibleLogins(m.VisibleItems())
	m = openChannel(t, m)
	if got := visibleLogins(m.VisibleItems()); !equalStringSlices(got, frozen) {
		t.Fatalf("list moved before refresh: before=%v after=%v", frozen, got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if m.Recents()[0].Login != "bob" {
		t.Fatalf("after refresh Recents()[0] = %q, want bob", m.Recents()[0].Login)
	}
	if cmd != nil {
		m = feedLoadedMsgs(t, m, cmd)
	}
	got := visibleLogins(m.VisibleItems())
	want := []string{"fav_one", "bob", "alice", "xqc"}
	if !equalStringSlices(got, want) {
		t.Fatalf("after refresh order = %v, want %v", got, want)
	}
	if m.Favorites()[0].Login != "fav_one" {
		t.Fatalf("favorite moved: %+v", m.Favorites())
	}

	// Open alice (now second recent), freeze again, then Esc applies on categories.
	m.highlightedID = "rec-a"
	m.reconcileHighlight()
	beforeBack := visibleLogins(m.VisibleItems())
	m = openChannel(t, m)
	if got := visibleLogins(m.VisibleItems()); !equalStringSlices(got, beforeBack) {
		t.Fatalf("open before Esc reordered: before=%v after=%v", beforeBack, got)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Mode() != ModeCategories {
		t.Fatalf("mode=%q, want categories", m.Mode())
	}
	if m.Recents()[0].Login != "alice" {
		t.Fatalf("after Esc Recents()[0] = %q, want alice", m.Recents()[0].Login)
	}
	if cmd != nil {
		m = feedLoadedMsgs(t, m, cmd)
	}
	got = visibleLogins(m.VisibleItems())
	wantCats := []string{"fav_one", "alice", "bob", "Just Chatting"}
	if !equalStringSlices(got, wantCats) {
		t.Fatalf("after Esc order = %v, want %v", got, wantCats)
	}
}

func TestOpenChannelKeepsFavoritePositionAndDedup(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	fs := &fakeStore{}

	m := New(catEN,
		WithStore(fs),
		WithFavorites([]twitch.Channel{
			{ID: "fav-shared", Login: "shared", DisplayName: "Shared"},
			{ID: "fav-two", Login: "second_fav", DisplayName: "Second Fav"},
		}),
		WithRecents([]twitch.Channel{
			{ID: "rec-1", Login: "recent_one", DisplayName: "Recent One"},
		}),
	)
	m.mode = ModeChannels
	m.channels = []twitch.Channel{
		{ID: "fav-shared", Login: "shared", DisplayName: "Shared", Viewers: twitch.IntPtr(1)},
		{ID: "ch-new", Login: "brand_new", DisplayName: "Brand New", Viewers: twitch.IntPtr(2)},
	}
	m.highlightedID = "ch-new"
	m.reconcileHighlight()

	before := visibleLogins(m.VisibleItems())
	wantBefore := []string{"shared", "second_fav", "recent_one", "brand_new"}
	if !equalStringSlices(before, wantBefore) {
		t.Fatalf("setup = %v, want %v", before, wantBefore)
	}

	m = openChannel(t, m)
	if got := visibleLogins(m.VisibleItems()); !equalStringSlices(got, before) {
		t.Fatalf("visible order changed: before=%v after=%v", before, got)
	}
	favs := m.Favorites()
	if len(favs) != 2 || favs[0].Login != "shared" || favs[1].Login != "second_fav" {
		t.Fatalf("favorites moved: %+v", favs)
	}
	if len(fs.savedState.Recents) == 0 || fs.savedState.Recents[0].Login != "brand_new" {
		t.Fatalf("store recents[0] = %+v, want brand_new", fs.savedState.Recents)
	}

	m.applyPendingRecents()
	items := m.VisibleItems()
	logins := visibleLogins(items)
	want := []string{"shared", "second_fav", "brand_new", "recent_one"}
	if !equalStringSlices(logins, want) {
		t.Fatalf("after apply order = %v, want %v", logins, want)
	}
	var sharedCount int
	for _, it := range items {
		if it.Login() == "shared" {
			sharedCount++
			if it.Type != ItemTypeFavorite {
				t.Fatalf("shared must stay a favorite row, got %s", it.Type)
			}
		}
	}
	if sharedCount != 1 {
		t.Fatalf("shared appeared %d times, want 1 (dedup)", sharedCount)
	}
}

// feedLoadedMsgs runs a fetch/hydrate cmd tree into Update so channel and
// category pages land on the model.
func feedLoadedMsgs(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			m = feedLoadedMsgs(t, m, c)
		}
	case CategoriesLoadedMsg, ChannelsLoadedMsg, SearchLoadedMsg, SavedAvailabilityMsg:
		updated, next := m.Update(v)
		m = updated.(Model)
		if next != nil {
			m = feedLoadedMsgs(t, m, next)
		}
	}
	return m
}
