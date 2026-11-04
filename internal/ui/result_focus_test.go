package ui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// focusFixture builds a model with a favorite and a recent that do not belong
// to the listing about to be opened; they render above the results.
func focusFixture(t *testing.T, src *fakeSource) Model {
	t.Helper()
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithSource(src),
		WithFavorites([]twitch.Channel{{ID: "fav-1", Login: "xqc", DisplayName: "xQc"}}),
		WithRecents([]twitch.Channel{{ID: "rec-1", Login: "eslcs", DisplayName: "ESLCS"}}),
	)
	m.loading = false
	m.categories = []twitch.Category{
		{ID: "jc", Name: "Just Chatting"},
		{ID: "cs", Name: "Counter-Strike"},
	}
	m.highlightedID = "cs"
	return m
}

func channelsPage(items ...twitch.Channel) twitch.Page[twitch.Channel] {
	return twitch.Page[twitch.Channel]{Items: items}
}

func live(id, login string) twitch.Channel {
	return twitch.Channel{ID: id, Login: login, DisplayName: login, Viewers: twitch.IntPtr(100), Availability: twitch.AvailabilityLive}
}

// run applies cmd's message to m, as the Bubble Tea runtime would.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func pressEnter(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return updated.(Model), cmd
}

func TestOpenCategoryHighlightsFirstCategoryChannel(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return channelsPage(live("c-1", "strogo"), live("c-2", "gaules")), nil
		},
	}
	m := focusFixture(t, src)

	m, cmd := pressEnter(t, m)
	if m.Mode() != ModeChannels {
		t.Fatalf("Enter on category should open channels, mode = %v", m.Mode())
	}
	m = run(t, m, cmd)

	if got := m.HighlightedID(); got != "c-1" {
		t.Fatalf("highlight = %q, want first category channel c-1 (not favorite/recent)", got)
	}
	items := m.VisibleItems()
	if items[0].ID() != "fav-1" || items[1].ID() != "rec-1" {
		t.Fatalf("favorites and recents must stay on top, got order %v", m.visibleItemIDs())
	}

	// Enter right after the load opens exactly the highlighted channel.
	_, cmd = pressEnter(t, m)
	if cmd == nil {
		t.Fatal("expected command from Enter on channel")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "strogo" {
		t.Fatalf("Enter opened %+v, want strogo", sel)
	}
}

func TestOpenCategorySkipsChannelsAlreadyShownAsFavorite(t *testing.T) {
	// The category's top channel is a favorite: it renders in Favorites and the
	// highlight goes to the first row of the category section.
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return channelsPage(live("fav-1", "xqc"), live("c-2", "gaules")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != "c-2" {
		t.Fatalf("highlight = %q, want c-2 (first row of the category section)", got)
	}
}

func TestOpenCategoryWhoseChannelsAreAllSavedHighlightsFirstLoaded(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return channelsPage(live("rec-1", "eslcs"), live("fav-1", "xqc")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != "rec-1" {
		t.Fatalf("highlight = %q, want rec-1 (first loaded channel, rendered as recent)", got)
	}
}

func TestSearchHighlightsFirstResult(t *testing.T) {
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return channelsPage(
				twitch.Channel{ID: "s-1", Login: "gaules", DisplayName: "Gaules", Availability: twitch.AvailabilityOffline},
				twitch.Channel{ID: "s-2", Login: "gaulesbr", DisplayName: "GaulesBR"},
			), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("gaules")
	m = run(t, updated.(Model), cmd)

	if got := m.HighlightedID(); got != "s-1" {
		t.Fatalf("highlight = %q, want first search result s-1", got)
	}
	_, cmd = pressEnter(t, m)
	if cmd == nil {
		t.Fatal("expected command from Enter on search result")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "gaules" {
		t.Fatalf("Enter opened %+v, want gaules", sel)
	}
}

func TestSearchPrefersExactLoginOverOfflineClones(t *testing.T) {
	// Twitch relevance returns the clone first; PreferSearchChannels must put
	// the exact login first so Enter opens landonorris, not landonorris0481.
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return channelsPage(
				twitch.Channel{ID: "clone-0481", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: twitch.AvailabilityOffline},
				twitch.Channel{ID: "exact", Login: "landonorris", DisplayName: "LandoNorris", Availability: twitch.AvailabilityOffline},
				twitch.Channel{ID: "clone-16", Login: "landonorris16", DisplayName: "landonorris16", Availability: twitch.AvailabilityOffline},
			), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("landonorris")
	m = run(t, updated.(Model), cmd)

	if len(m.channels) < 1 || m.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %+v, want exact landonorris first", m.channels)
	}
	if got := m.HighlightedID(); got != "exact" {
		t.Fatalf("highlight = %q, want exact", got)
	}
	_, cmd = pressEnter(t, m)
	if cmd == nil {
		t.Fatal("expected command from Enter on exact search result")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "landonorris" {
		t.Fatalf("Enter opened %+v, want landonorris", sel)
	}
}

func TestSearchExactOfflineStillFirstWhenCloneIsLive(t *testing.T) {
	// Chosen rule: exact login stays first even if offline while a clone is live.
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return channelsPage(
				twitch.Channel{ID: "clone-live", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: twitch.AvailabilityLive, Viewers: twitch.IntPtr(12)},
				twitch.Channel{ID: "exact-off", Login: "landonorris", DisplayName: "LandoNorris", Availability: twitch.AvailabilityOffline},
			), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("LandoNorris")
	m = run(t, updated.(Model), cmd)

	if m.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %q, want exact first (chosen rule)", m.channels[0].Login)
	}
	if got := m.HighlightedID(); got != "exact-off" {
		t.Fatalf("highlight = %q, want exact-off", got)
	}
	_, cmd = pressEnter(t, m)
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "landonorris" {
		t.Fatalf("Enter opened %+v, want landonorris", sel)
	}
}

func TestSearchHighlightsExactEvenWhenFoldedIntoRecents(t *testing.T) {
	// Real failure mode: landonorris is already a recent, so VisibleItems omits it
	// from the search section and would otherwise highlight the first clone.
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return channelsPage(
				twitch.Channel{ID: "clone-0481", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: twitch.AvailabilityOffline},
				twitch.Channel{ID: "twitch-exact", Login: "landonorris", DisplayName: "LandoNorris", Availability: twitch.AvailabilityOffline},
				twitch.Channel{ID: "clone-16", Login: "landonorris16", DisplayName: "landonorris16", Availability: twitch.AvailabilityOffline},
			), nil
		},
	}
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithSource(src),
		WithRecents([]twitch.Channel{{ID: "recent-exact", Login: "landonorris", DisplayName: "LandoNorris"}}),
	)
	m.loading = false

	updated, cmd := m.startSearch("landonorris")
	m = run(t, updated.(Model), cmd)

	if m.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %q, want exact", m.channels[0].Login)
	}
	// Highlight must be the exact channel row (rendered as recent), not clone-0481.
	got := m.HighlightedID()
	if got == "clone-0481" || got == "clone-16" {
		t.Fatalf("highlight = %q, landed on a clone", got)
	}
	ch, ok := m.HighlightedChannel()
	if !ok || ch.Login != "landonorris" {
		t.Fatalf("highlighted channel = %+v, want landonorris", ch)
	}
	_, cmd = pressEnter(t, m)
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "landonorris" {
		t.Fatalf("Enter opened %+v, want landonorris", sel)
	}
}

func TestRefreshAndNextPageKeepHighlightByIdentity(t *testing.T) {
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			calls++
			if calls == 1 {
				return channelsPage(live("c-1", "strogo"), live("c-2", "gaules")), nil
			}
			// Refresh reorders the listing; c-2 is still present.
			return channelsPage(live("c-9", "newcomer"), live("c-1", "strogo"), live("c-2", "gaules")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)

	m.moveHighlight(1)
	if got := m.HighlightedID(); got != "c-2" {
		t.Fatalf("after Down, highlight = %q, want c-2", got)
	}

	updated, cmd := m.retryCurrentMode()
	m = run(t, updated.(Model), cmd)
	if got := m.HighlightedID(); got != "c-2" {
		t.Fatalf("refresh moved highlight to %q, want c-2 kept by identity", got)
	}

	updated, _ = m.Update(ChannelsLoadedMsg{
		Generation: m.generation,
		Category:   *m.selectedCategory,
		Page:       channelsPage(live("c-3", "third")),
		Append:     true,
	})
	m = updated.(Model)
	if got := m.HighlightedID(); got != "c-2" {
		t.Fatalf("next page moved highlight to %q, want c-2 kept by identity", got)
	}
}

func TestManualMoveDuringLoadWins(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return channelsPage(live("c-1", "strogo")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	chosen := m.HighlightedID()
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != chosen {
		t.Fatalf("load overrode the user's move: highlight = %q, want %q", got, chosen)
	}
}

func TestFailedLoadThenRetryStillFocusesFirstResult(t *testing.T) {
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			calls++
			if calls == 1 {
				return twitch.Page[twitch.Channel]{}, errors.New("rede caiu")
			}
			return channelsPage(live("c-1", "strogo")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if m.err == nil {
		t.Fatal("expected load error")
	}
	updated, cmd := m.retryCurrentMode()
	m = run(t, updated.(Model), cmd)
	if got := m.HighlightedID(); got != "c-1" {
		t.Fatalf("highlight = %q, want c-1 after a successful retry", got)
	}
}

func TestEmptyCategoryAndEmptySearchKeepAValidHighlight(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return channelsPage(), nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return channelsPage(), nil
		},
	}

	// Empty category: the highlight lands on a row that exists.
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if _, ok := m.HighlightedItem(); !ok {
		t.Fatalf("empty category left a dangling highlight %q", m.HighlightedID())
	}
	_ = m.View()

	// Empty search with nothing saved: no highlight, Enter is a no-op.
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	bare := New(cat, WithSource(src))
	bare.loading = false
	updated, cmd := bare.startSearch("ninguem")
	bare = run(t, updated.(Model), cmd)
	if got := bare.HighlightedID(); got != "" {
		t.Fatalf("empty search highlight = %q, want empty", got)
	}
	if _, cmd := pressEnter(t, bare); cmd != nil {
		t.Fatal("Enter on an empty search should do nothing")
	}
	_ = bare.View()
}
