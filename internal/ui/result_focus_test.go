package ui

import (
	"context"
	"errors"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// focusFixture builds a model with a favorite and a recent that do not belong
// to the listing about to be opened; they stay on the home screen.
func focusFixture(t *testing.T, src *fakeSource) Model {
	t.Helper()
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithSource(src),
		WithFavorites([]catalog.Channel{{ID: "fav-1", Login: "xqc", DisplayName: "xQc"}}),
		WithRecents([]catalog.Channel{{ID: "rec-1", Login: "eslcs", DisplayName: "ESLCS"}}),
	)
	m.list.loading = false
	m.list.categories = []catalog.Category{
		{ID: "jc", Name: "Just Chatting"},
		{ID: "cs", Name: "Counter-Strike"},
	}
	m.list.highlightedID = "cs"
	return m
}

func channelsPage(items ...catalog.Channel) catalog.Page[catalog.Channel] {
	return catalog.Page[catalog.Channel]{Items: items}
}

func live(id, login string) catalog.Channel {
	return catalog.Channel{ID: id, Login: login, DisplayName: login, Viewers: catalog.IntPtr(100), Availability: catalog.AvailabilityLive}
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
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
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
	if got := m.visibleItemIDs(); !equalStringSlices(got, []string{"c-1", "c-2"}) {
		t.Fatalf("a categoria lista só os canais dela; favoritos e recentes ficam na tela inicial, got order %v", got)
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

func TestOpenCategoryHighlightsFavoriteInPlace(t *testing.T) {
	// O primeiro canal da categoria é favorito: ele fica no lugar dele, com ★,
	// e recebe o destaque como qualquer primeiro resultado.
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return channelsPage(live("fav-1", "xqc"), live("c-2", "gaules")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	it, ok := m.HighlightedItem()
	if !ok || it.ID() != "fav-1" || it.Type != ItemTypeChannel || !it.Favorite {
		t.Fatalf("highlight = %+v, want fav-1 starred in the category section", it)
	}
}

func TestOpenCategoryWhoseChannelsAreAllSavedHighlightsFirstLoaded(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return channelsPage(live("rec-1", "eslcs"), live("fav-1", "xqc")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != "rec-1" {
		t.Fatalf("highlight = %q, want rec-1 (first loaded channel)", got)
	}
}

func TestSearchHighlightsFirstResult(t *testing.T) {
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return channelsPage(
				catalog.Channel{ID: "s-1", Login: "gaules", DisplayName: "Gaules", Availability: catalog.AvailabilityOffline},
				catalog.Channel{ID: "s-2", Login: "gaulesbr", DisplayName: "GaulesBR"},
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
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return channelsPage(
				catalog.Channel{ID: "clone-0481", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: catalog.AvailabilityOffline},
				catalog.Channel{ID: "exact", Login: "landonorris", DisplayName: "LandoNorris", Availability: catalog.AvailabilityOffline},
				catalog.Channel{ID: "clone-16", Login: "landonorris16", DisplayName: "landonorris16", Availability: catalog.AvailabilityOffline},
			), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("landonorris")
	m = run(t, updated.(Model), cmd)

	if len(m.list.channels) < 1 || m.list.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %+v, want exact landonorris first", m.list.channels)
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
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return channelsPage(
				catalog.Channel{ID: "clone-live", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(12)},
				catalog.Channel{ID: "exact-off", Login: "landonorris", DisplayName: "LandoNorris", Availability: catalog.AvailabilityOffline},
			), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("LandoNorris")
	m = run(t, updated.(Model), cmd)

	if m.list.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %q, want exact first (chosen rule)", m.list.channels[0].Login)
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

func TestSearchHighlightsExactChannelAlreadyInRecents(t *testing.T) {
	// landonorris já é recente e também é o canal exato da busca: a busca lista só
	// resultados, e o destaque tem de ir ao canal exato, não ao primeiro clone.
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return channelsPage(
				catalog.Channel{ID: "clone-0481", Login: "landonorris0481", DisplayName: "landonorris0481", Availability: catalog.AvailabilityOffline},
				catalog.Channel{ID: "twitch-exact", Login: "landonorris", DisplayName: "LandoNorris", Availability: catalog.AvailabilityOffline},
				catalog.Channel{ID: "clone-16", Login: "landonorris16", DisplayName: "landonorris16", Availability: catalog.AvailabilityOffline},
			), nil
		},
	}
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat,
		WithSource(src),
		WithRecents([]catalog.Channel{{ID: "recent-exact", Login: "landonorris", DisplayName: "LandoNorris"}}),
	)
	m.list.loading = false

	updated, cmd := m.startSearch("landonorris")
	m = run(t, updated.(Model), cmd)

	if m.list.channels[0].Login != "landonorris" {
		t.Fatalf("channels[0] = %q, want exact", m.list.channels[0].Login)
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
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
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

	// Enter right after refresh must open the channel that stayed highlighted.
	_, enterCmd := pressEnter(t, m)
	if enterCmd == nil {
		t.Fatal("expected command from Enter after refresh")
	}
	sel, ok := enterCmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "gaules" {
		t.Fatalf("Enter after refresh opened %+v, want gaules", sel)
	}

	updated, _ = m.Update(ChannelsLoadedMsg{
		Generation: m.list.generation,
		Category:   *m.list.selectedCategory,
		Page:       channelsPage(live("c-3", "third")),
		Append:     true,
	})
	m = updated.(Model)
	if got := m.HighlightedID(); got != "c-2" {
		t.Fatalf("next page moved highlight to %q, want c-2 kept by identity", got)
	}
}

func TestRefreshRemovedItemFallsToNearestNeighborNotTop(t *testing.T) {
	// Mirrors the Rafa report: highlight on a mid-list channel; refresh drops it;
	// the highlight must land on the neighbor at that position, not the first
	// favorite/recent at the top of VisibleItems.
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			calls++
			if calls == 1 {
				return channelsPage(
					live("c-1", "strogo"),
					live("c-shadow", "shadowkekw"),
					live("c-3", "gaules"),
				), nil
			}
			// shadowkekw went offline / left the first page.
			return channelsPage(
				live("c-1", "strogo"),
				live("c-3", "gaules"),
				live("c-9", "newcomer"),
			), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)

	// Ao abrir, o destaque fica em c-1 (primeira linha da categoria). A lista
	// mostra só os canais da categoria: c-1, c-shadow, c-3. Um Down cai em shadowkekw.
	if got := m.HighlightedID(); got != "c-1" {
		t.Fatalf("after open, highlight = %q, want c-1", got)
	}
	m.moveHighlight(1)
	if got := m.HighlightedID(); got != "c-shadow" {
		t.Fatalf("after Down, highlight = %q, want c-shadow", got)
	}
	prevIdx := m.list.highlightIndex

	updated, cmd := m.retryCurrentMode()
	m = run(t, updated.(Model), cmd)

	if got := m.HighlightedID(); got == "fav-1" || got == "rec-1" {
		t.Fatalf("refresh jumped to the top (%q); want nearest neighbor, not favorite/recent", got)
	}
	if got := m.HighlightedID(); got == "c-shadow" {
		t.Fatal("removed channel stayed highlighted")
	}
	// O índice anterior apontava para c-shadow; depois da remoção, a linha nesse
	// índice é c-3 (gaules). Nova ordem: c-1, c-3, c-9.
	if got := m.HighlightedID(); got != "c-3" {
		t.Fatalf("highlight = %q at index %d (was %d), want c-3 (neighbor)", got, m.list.highlightIndex, prevIdx)
	}

	_, enterCmd := pressEnter(t, m)
	if enterCmd == nil {
		t.Fatal("expected command from Enter after refresh removed the old channel")
	}
	sel, ok := enterCmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "gaules" {
		t.Fatalf("Enter opened %+v, want gaules (neighbor of removed shadowkekw)", sel)
	}
}

func TestRefreshDuringSearchKeepsOpenHighlightRules(t *testing.T) {
	// Opening a search still focuses the first result (v1.1 ticket 05). A later
	// refresh of that search is not an opening: identity wins.
	calls := 0
	src := &fakeSource{
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			calls++
			if calls == 1 {
				return channelsPage(live("s-1", "alpha"), live("s-2", "beta"), live("s-3", "gamma")), nil
			}
			// Refresh reorders; beta remains.
			return channelsPage(live("s-9", "zeta"), live("s-3", "gamma"), live("s-2", "beta")), nil
		},
	}
	m := focusFixture(t, src)

	updated, cmd := m.startSearch("streamers")
	m = run(t, updated.(Model), cmd)
	if got := m.HighlightedID(); got != "s-1" {
		t.Fatalf("open search highlight = %q, want s-1 (first result)", got)
	}

	m.moveHighlight(1) // s-1 -> s-2
	if got := m.HighlightedID(); got != "s-2" {
		t.Fatalf("after Down, highlight = %q, want s-2", got)
	}

	updated, cmd = m.retryCurrentMode()
	m = run(t, updated.(Model), cmd)
	if got := m.HighlightedID(); got != "s-2" {
		t.Fatalf("search refresh moved highlight to %q, want s-2 by identity", got)
	}
	if m.list.focusResults {
		t.Fatal("refresh must not leave focusResults set (that would re-open to first result)")
	}
}

func TestRefreshPreservesHighlightByLoginWhenIDChurns(t *testing.T) {
	// Upstream sometimes re-keys the same login with a different ID string.
	// Identity must still hold so Enter opens the same channel.
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			calls++
			if calls == 1 {
				return channelsPage(live("id-old", "shadowkekw"), live("id-other", "zubarefff")), nil
			}
			return channelsPage(live("id-other", "zubarefff"), live("id-new", "shadowkekw")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)

	// After open, highlight is already on id-old (first category channel).
	// Visible: fav-1, rec-1, id-old(shadowkekw), id-other(zubarefff).
	if got := m.HighlightedID(); got != "id-old" {
		t.Fatalf("after open, highlight = %q, want id-old", got)
	}
	ch, ok := m.HighlightedChannel()
	if !ok || ch.Login != "shadowkekw" {
		t.Fatalf("highlighted channel = %+v, want shadowkekw", ch)
	}

	updated, cmd := m.retryCurrentMode()
	m = run(t, updated.(Model), cmd)
	gotCh, ok := m.HighlightedChannel()
	if !ok || gotCh.Login != "shadowkekw" {
		t.Fatalf("after ID churn, highlighted = %+v (id %q), want shadowkekw", gotCh, m.HighlightedID())
	}
	_, enterCmd := pressEnter(t, m)
	sel, ok := enterCmd().(ChannelSelectedMsg)
	if !ok || sel.Channel.Login != "shadowkekw" {
		t.Fatalf("Enter opened %+v, want shadowkekw", sel)
	}
}

// TestRefreshPTYEvidence prints the ▸ marker before and after pressing r, so a
// PTY/script capture can show the highlight stayed on the same channel.
func TestRefreshPTYEvidence(t *testing.T) {
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			calls++
			if calls == 1 {
				return channelsPage(live("c-1", "strogo"), live("c-shadow", "shadowkekw"), live("c-zub", "zubarefff")), nil
			}
			return channelsPage(live("c-zub", "zubarefff"), live("c-1", "strogo"), live("c-shadow", "shadowkekw")), nil
		},
	}
	m := focusFixture(t, src)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(Model)

	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	m.moveHighlight(1) // strogo -> shadowkekw

	before := markerLine(m.View())
	beforeCh, ok := m.HighlightedChannel()
	if !ok || beforeCh.Login != "shadowkekw" {
		t.Fatalf("before refresh: highlighted %+v, want shadowkekw; view marker %q", beforeCh, before)
	}
	t.Logf("BEFORE r: %s", before)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = run(t, updated.(Model), cmd)

	after := markerLine(m.View())
	afterCh, ok := m.HighlightedChannel()
	if !ok || afterCh.Login != "shadowkekw" {
		t.Fatalf("after refresh: highlighted %+v, want shadowkekw; view marker %q", afterCh, after)
	}
	t.Logf("AFTER  r: %s", after)
}

func markerLine(view string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "▸") {
			return strings.TrimSpace(line)
		}
	}
	return "(no ▸ marker)"
}

// Com a categoria carregando, a lista não tem linha: a seta não escolhe nada, e
// o primeiro canal ganha o destaque quando a página chega.
func TestArrowDuringLoadStillFocusesFirstResult(t *testing.T) {
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return channelsPage(live("c-1", "strogo"), live("c-2", "gaules")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != "c-1" {
		t.Fatalf("highlight = %q, want the first channel c-1", got)
	}
}

func TestFailedLoadThenRetryStillFocusesFirstResult(t *testing.T) {
	calls := 0
	src := &fakeSource{
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			calls++
			if calls == 1 {
				return catalog.Page[catalog.Channel]{}, errors.New("rede caiu")
			}
			return channelsPage(live("c-1", "strogo")), nil
		},
	}
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if m.list.err == nil {
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
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return channelsPage(), nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			return channelsPage(), nil
		},
	}

	// Categoria vazia: os favoritos e recentes ficam na tela inicial, então
	// não há linha; o destaque não aponta para nada e Enter não faz nada.
	m := focusFixture(t, src)
	m, cmd := pressEnter(t, m)
	m = run(t, m, cmd)
	if got := m.HighlightedID(); got != "" {
		t.Fatalf("empty category left a dangling highlight %q", got)
	}
	if _, cmd := pressEnter(t, m); cmd != nil {
		t.Fatal("Enter on an empty category should do nothing")
	}
	_ = m.View()

	// Empty search with nothing saved: no highlight, Enter is a no-op.
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	bare := New(cat, WithSource(src))
	bare.list.loading = false
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
