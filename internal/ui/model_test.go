package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

type fakeSource struct {
	categoriesFn       func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error)
	channelsFn         func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error)
	searchFn           func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error)
	searchCategoriesFn func(ctx context.Context, query string) ([]twitch.Category, error)
	channelsByLoginsFn func(ctx context.Context, logins []string) ([]twitch.Channel, error)
}

func (f *fakeSource) Categories(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	if f.categoriesFn != nil {
		return f.categoriesFn(ctx, page)
	}
	return twitch.Page[twitch.Category]{}, nil
}

func (f *fakeSource) Channels(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	if f.channelsFn != nil {
		return f.channelsFn(ctx, categoryID, page)
	}
	return twitch.Page[twitch.Channel]{}, nil
}

func (f *fakeSource) Search(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
	if f.searchFn != nil {
		return f.searchFn(ctx, query)
	}
	return twitch.Page[twitch.Channel]{}, nil
}

func (f *fakeSource) SearchCategories(ctx context.Context, query string) ([]twitch.Category, error) {
	if f.searchCategoriesFn != nil {
		return f.searchCategoriesFn(ctx, query)
	}
	return nil, nil
}

func (f *fakeSource) ChannelsByLogins(ctx context.Context, logins []string) ([]twitch.Channel, error) {
	if f.channelsByLoginsFn != nil {
		return f.channelsByLoginsFn(ctx, logins)
	}
	return nil, nil
}

func TestCtrlQQuitsModel(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}
	m := New(cat)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	got, ok := updated.(Model)
	if !ok {
		t.Fatalf("expected ui.Model, got %T", updated)
	}
	if !got.Quitting() {
		t.Fatal("expected model Quitting() to be true after Ctrl+Q")
	}
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd after Ctrl+Q")
	}
	msg := cmd()
	if _, isQuit := msg.(tea.QuitMsg); !isQuit {
		t.Fatalf("expected tea.QuitMsg from cmd(), got %T (%v)", msg, msg)
	}
}

func TestStatusBarReflectsCatalogLanguage(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	viewEN := New(catEN).View()
	if !strings.Contains(viewEN, "[en]") || !strings.Contains(viewEN, "Ready") || !strings.Contains(viewEN, "Ctrl+Q quit") {
		t.Fatalf("unexpected English status bar: %q", viewEN)
	}
	if strings.Contains(viewEN, "Ctrl+C") {
		t.Fatalf("English status bar still cites Ctrl+C: %q", viewEN)
	}

	viewPT := New(catPT).View()
	if !strings.Contains(viewPT, "[pt-BR]") || !strings.Contains(viewPT, "Pronto") || !strings.Contains(viewPT, "Ctrl+Q sair") {
		t.Fatalf("unexpected Portuguese status bar: %q", viewPT)
	}
	if strings.Contains(viewPT, "Ctrl+C") {
		t.Fatalf("Portuguese status bar still cites Ctrl+C: %q", viewPT)
	}
}

func TestCategoriesLoadingAndOptionalViewersRender(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "509658", Name: "Just Chatting", Viewers: twitch.IntPtr(560000)},
					{ID: "99999", Name: "Mystery Game", Viewers: nil},
				},
				Cursor:  "cur-2",
				HasMore: false,
			}, nil
		},
	}

	m := New(catPT, WithSource(src))
	if !m.Loading() {
		t.Fatal("expected model to start in loading state when Source is provided")
	}
	loadingView := m.View()
	if !strings.Contains(loadingView, "Carregando categorias") {
		t.Fatalf("expected loading message in view, got: %q", loadingView)
	}

	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("expected non-nil Init() cmd with Source")
	}
	loadedMsg := initCmd()
	updated, _ := m.Update(loadedMsg)
	m = updated.(Model)

	if m.Loading() {
		t.Fatal("expected Loading() to be false after CategoriesLoadedMsg")
	}
	if m.HighlightedID() != "509658" {
		t.Fatalf("HighlightedID() = %q, want %q", m.HighlightedID(), "509658")
	}

	view := m.View()
	if !strings.Contains(view, "Just Chatting") || !strings.Contains(view, "560.0k") {
		t.Fatalf("expected Just Chatting and 560.0k in view, got: %q", view)
	}
	if !strings.Contains(view, "Mystery Game") || !strings.Contains(view, "—") {
		t.Fatalf("expected Mystery Game with '—' placeholder for nil viewers, got: %q", view)
	}
}

func TestCategoriesNavigationArrowsJKAndNextPage(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}

	var requestedCursors []string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			requestedCursors = append(requestedCursors, page.Cursor)
			if page.Cursor == "" {
				return twitch.Page[twitch.Category]{
					Items: []twitch.Category{
						{ID: "cat-1", Name: "One", Viewers: twitch.IntPtr(3000)},
						{ID: "cat-2", Name: "Two", Viewers: twitch.IntPtr(2000)},
					},
					Cursor:  "cursor-1",
					HasMore: true,
				}, nil
			}
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "cat-3", Name: "Three", Viewers: twitch.IntPtr(1000)},
				},
				Cursor:  "cursor-2",
				HasMore: false,
			}, nil
		},
	}

	m := New(catEN, WithSource(src), WithPageSize(2))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	if m.HighlightedID() != "cat-1" {
		t.Fatalf("initial HighlightedID = %q, want cat-1", m.HighlightedID())
	}

	// Move down with 'j' to the last loaded item -> triggers next page fetch.
	updated, nextCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "cat-2" {
		t.Fatalf("after 'j', HighlightedID = %q, want cat-2", m.HighlightedID())
	}
	if nextCmd == nil {
		t.Fatal("expected reaching last item with HasMore=true to return next-page tea.Cmd")
	}

	updated, _ = m.Update(nextCmd())
	m = updated.(Model)
	if len(m.Categories()) != 3 {
		t.Fatalf("len(Categories) = %d, want 3 after appending second page", len(m.Categories()))
	}
	if len(requestedCursors) != 2 || requestedCursors[1] != "cursor-1" {
		t.Fatalf("requestedCursors = %v, want [\"\", \"cursor-1\"]", requestedCursors)
	}

	// Navigate down with Down arrow to cat-3, then up with 'k' and Up arrow.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.HighlightedID() != "cat-3" {
		t.Fatalf("after Down arrow, HighlightedID = %q, want cat-3", m.HighlightedID())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.HighlightedID() != "cat-2" {
		t.Fatalf("after 'k', HighlightedID = %q, want cat-2", m.HighlightedID())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.HighlightedID() != "cat-1" {
		t.Fatalf("after Up arrow, HighlightedID = %q, want cat-1", m.HighlightedID())
	}
}

func TestSelectionByIDPreservedWhenFiltering(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}

	m := New(catEN)
	updated, _ := m.Update(CategoriesLoadedMsg{
		Generation: m.Generation(),
		Page: twitch.Page[twitch.Category]{
			Items: []twitch.Category{
				{ID: "id-chat", Name: "Just Chatting", Viewers: twitch.IntPtr(500000)},
				{ID: "id-wc3", Name: "Warcraft III", Viewers: twitch.IntPtr(20000)},
				{ID: "id-wow", Name: "World of Warcraft", Viewers: twitch.IntPtr(15000)},
			},
		},
	})
	m = updated.(Model)

	// Move highlight to "World of Warcraft" (index 2 in full list).
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "id-wow" {
		t.Fatalf("HighlightedID = %q, want id-wow", m.HighlightedID())
	}

	// Filter by "Warcraft": visible list is now [Warcraft III (index 0), World of Warcraft (index 1)].
	// Highlight must remain on "id-wow" (NOT reset to index 0, and NOT use unfiltered index 1).
	m = m.SetFilter("Warcraft")
	if m.HighlightedID() != "id-wow" {
		t.Fatalf("after SetFilter(Warcraft), HighlightedID = %q, want id-wow", m.HighlightedID())
	}

	// Press Enter: must select "id-wow" ("World of Warcraft"), not "Just Chatting" or "Warcraft III".
	updated, selectCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	selected := m.SelectedCategory()
	if selected == nil || selected.ID != "id-wow" || selected.Name != "World of Warcraft" {
		t.Fatalf("SelectedCategory() = %+v, want id-wow (World of Warcraft)", selected)
	}
	if selectCmd == nil {
		t.Fatal("expected CategorySelectedMsg cmd on Enter")
	}
	msg := selectCmd()
	selMsg, ok := msg.(CategorySelectedMsg)
	if !ok || selMsg.Category.ID != "id-wow" {
		t.Fatalf("CategorySelectedMsg = %+v, want Category.ID=id-wow", msg)
	}

	// Also verify filtering down to a single item at filtered index 0 selects its ID ("id-wc3"),
	// never categories[0] ("id-chat").
	m = m.SetFilter("Warcraft III")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.SelectedCategory() == nil || m.SelectedCategory().ID != "id-wc3" {
		t.Fatalf("SelectedCategory() after filtering to Warcraft III = %+v, want id-wc3", m.SelectedCategory())
	}
}

func TestErrorStateDisplaysActionableMessageAndRetryRecovers(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	calls := 0
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			calls++
			if calls == 1 {
				return twitch.Page[twitch.Category]{}, &twitch.Error{
					Kind:       twitch.Servidor,
					Op:         "categories",
					StatusCode: 503,
					Err:        errors.New("service unavailable"),
				}
			}
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "21779", Name: "League of Legends", Viewers: twitch.IntPtr(400000)},
				},
			}, nil
		},
	}

	m := New(catPT, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	if m.Err() == nil {
		t.Fatal("expected error state after failed load")
	}
	errView := m.View()
	if !strings.Contains(errView, "Erro no servidor da Twitch") {
		t.Fatalf("expected server error message in view, got: %q", errView)
	}
	if !strings.Contains(errView, "Pressione r para tentar novamente") {
		t.Fatalf("expected actionable retry instruction in view, got: %q", errView)
	}

	// Press 'r' to retry.
	updated, retryCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if !m.Loading() || m.Err() != nil {
		t.Fatalf("expected Loading=true and Err=nil after pressing r, got Loading=%v Err=%v", m.Loading(), m.Err())
	}
	if retryCmd == nil {
		t.Fatal("expected non-nil retry cmd after pressing r")
	}

	updated, _ = m.Update(retryCmd())
	m = updated.(Model)
	if m.Err() != nil || len(m.Categories()) != 1 {
		t.Fatalf("expected retry to load 1 category without error, got len=%d err=%v", len(m.Categories()), m.Err())
	}
}

func TestCancelActiveRequestCancelsContextAndDiscardsStaleGeneration(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}

	started := make(chan struct{})
	canceled := make(chan error, 1)
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			close(started)
			<-ctx.Done()
			canceled <- ctx.Err()
			return twitch.Page[twitch.Category]{}, ctx.Err()
		},
	}

	m := New(catEN, WithSource(src))
	staleGen := m.Generation()
	cmd := m.Init()
	go func() {
		_ = cmd()
	}()
	<-started

	// Simulate screen change / cancellation while request is in flight.
	m = m.CancelActiveRequest()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected in-flight context to be canceled with context.Canceled, got %v", err)
	}

	// Stale message from previous generation must be ignored.
	updated, _ := m.Update(CategoriesLoadedMsg{
		Generation: staleGen,
		Page: twitch.Page[twitch.Category]{
			Items: []twitch.Category{{ID: "1", Name: "Stale"}},
		},
	})
	m = updated.(Model)
	if len(m.Categories()) != 0 {
		t.Fatalf("expected stale generation message to be ignored, got categories=%+v", m.Categories())
	}
}

func TestOpenCategoryLoadsChannelsAndEnterEmitsChannelSelectedMsg(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	var gotCategoryID string
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "32399", Name: "Counter-Strike", Viewers: twitch.IntPtr(180000)},
				},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			gotCategoryID = categoryID
			return twitch.Page[twitch.Channel]{
				Items: []twitch.Channel{
					{
						ID:          "181077473",
						Login:       "gaules",
						DisplayName: "Gaules",
						Viewers:     twitch.IntPtr(45200),
						Title:       twitch.StringPtr("Major CS2 Ao Vivo"),
						CategoryID:  twitch.StringPtr("32399"),
					},
					{
						ID:          "999000",
						Login:       "silentstream",
						DisplayName: "SilentStream",
						Viewers:     nil,
						Title:       nil,
					},
				},
			}, nil
		},
	}

	m := New(catPT, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	// Press Enter on "Counter-Strike" category.
	updated, loadChCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.Mode() != ModeChannels {
		t.Fatalf("Mode() = %q, want %q", m.Mode(), ModeChannels)
	}
	if !m.Loading() {
		t.Fatal("expected Loading()=true while fetching channels")
	}
	if !strings.Contains(m.View(), "Carregando canais ao vivo") {
		t.Fatalf("expected loading channels text in view, got: %q", m.View())
	}
	if loadChCmd == nil {
		t.Fatal("expected non-nil cmd to fetch channels")
	}

	updated, _ = m.Update(loadChCmd())
	m = updated.(Model)
	if m.Loading() {
		t.Fatal("expected Loading()=false after ChannelsLoadedMsg")
	}
	if gotCategoryID != "32399" {
		t.Fatalf("gotCategoryID = %q, want 32399", gotCategoryID)
	}

	view := m.View()
	for _, want := range []string{"Counter-Strike", "Gaules", "gaules", "45.2k", "Major CS2 Ao Vivo", "SilentStream", "silentstream", "—"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in channels view, got: %q", want, view)
		}
	}

	// Press Enter on the highlighted channel -> emits ChannelSelectedMsg with identity, without quitting.
	updated, selCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.Quitting() {
		t.Fatal("selecting a channel must not quit the TUI")
	}
	if m.SelectedChannel() == nil || m.SelectedChannel().ID != "181077473" || m.SelectedChannel().Login != "gaules" {
		t.Fatalf("SelectedChannel() = %+v, want ID=181077473 Login=gaules", m.SelectedChannel())
	}
	if selCmd == nil {
		t.Fatal("expected ChannelSelectedMsg cmd on Enter")
	}
	msg := selCmd()
	chSel, ok := msg.(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("expected ChannelSelectedMsg, got %T (%v)", msg, msg)
	}
	if chSel.Channel.ID != "181077473" || chSel.Channel.Login != "gaules" || chSel.Channel.DisplayName != "Gaules" {
		t.Fatalf("ChannelSelectedMsg.Channel = %+v, want Gaules identity", chSel.Channel)
	}
}

func TestLocalFilterSlashAndEscPreservesSelectionByIDWithoutNetworkRequest(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New(en): %v", err)
	}

	channelsCalls := 0
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "1", Name: "CS2", Viewers: twitch.IntPtr(10000)}},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			channelsCalls++
			return twitch.Page[twitch.Channel]{
				Items: []twitch.Channel{
					{ID: "ch-alanzoka", Login: "alanzoka", DisplayName: "alanzoka", Viewers: twitch.IntPtr(30000), Title: twitch.StringPtr("Playing Horror")},
					{ID: "ch-caze", Login: "casimito", DisplayName: "Casimito", Viewers: twitch.IntPtr(25000), Title: twitch.StringPtr("Reacts da Madrugada")},
					{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(20000), Title: twitch.StringPtr("Tribo Major")},
				},
			}, nil
		},
	}

	m := New(catEN, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	updated, chCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(chCmd())
	m = updated.(Model)

	if channelsCalls != 1 {
		t.Fatalf("channelsCalls = %d, want 1", channelsCalls)
	}

	// Move highlight to index 2 ("ch-gaules").
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.HighlightedID() != "ch-gaules" {
		t.Fatalf("HighlightedID = %q, want ch-gaules", m.HighlightedID())
	}

	// Press '/' and type "gaules": filters locally without any new request, preserving "ch-gaules".
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if !m.Filtering() || cmd != nil {
		t.Fatalf("expected Filtering()=true and nil cmd on '/', got Filtering=%v cmd=%v", m.Filtering(), cmd)
	}
	for _, r := range "gaules" {
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("expected nil cmd while typing filter rune %q", r)
		}
	}
	if channelsCalls != 1 {
		t.Fatalf("channelsCalls after local filter = %d, want 1 (no network call)", channelsCalls)
	}
	if len(m.VisibleChannels()) != 1 || m.HighlightedID() != "ch-gaules" {
		t.Fatalf("VisibleChannels=%d HighlightedID=%q, want 1 and ch-gaules", len(m.VisibleChannels()), m.HighlightedID())
	}

	// Press Esc to clear filter: full list restored and "ch-gaules" stays highlighted.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Filtering() || m.Filter() != "" {
		t.Fatalf("after Esc, Filtering=%v Filter=%q, want false and empty", m.Filtering(), m.Filter())
	}
	if len(m.VisibleChannels()) != 3 || m.HighlightedID() != "ch-gaules" {
		t.Fatalf("after Esc, VisibleChannels=%d HighlightedID=%q, want 3 and ch-gaules", len(m.VisibleChannels()), m.HighlightedID())
	}

	// Now filter by '/' + "casimito" -> only "ch-caze" is visible (at filtered index 0), and press Enter.
	// Must select "ch-caze" by ID, NOT unfiltered index 0 ("ch-alanzoka").
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	for _, r := range "casimito" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if m.HighlightedID() != "ch-caze" {
		t.Fatalf("HighlightedID after filtering casimito = %q, want ch-caze", m.HighlightedID())
	}
	updated, selCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.SelectedChannel() == nil || m.SelectedChannel().ID != "ch-caze" || m.SelectedChannel().Login != "casimito" {
		t.Fatalf("SelectedChannel() = %+v, want ch-caze (casimito)", m.SelectedChannel())
	}
	if selCmd == nil {
		t.Fatal("expected ChannelSelectedMsg cmd on Enter")
	}
	if got := selCmd().(ChannelSelectedMsg); got.Channel.ID != "ch-caze" {
		t.Fatalf("ChannelSelectedMsg = %+v, want ch-caze", got)
	}
}

func TestCtrlFSearchAndCtrlRReturnsToCategories(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	var searchedQuery string
	categoriesCalls := 0
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			categoriesCalls++
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "509658", Name: "Just Chatting", Viewers: twitch.IntPtr(500000)},
					{ID: "21779", Name: "League of Legends", Viewers: twitch.IntPtr(300000)},
				},
			}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			searchedQuery = query
			return twitch.Page[twitch.Channel]{
				Items: []twitch.Channel{
					{ID: "181077473", Login: "gaules", DisplayName: "Gaules"},
					{ID: "36029255", Login: "riotgames", DisplayName: "Riot Games"},
				},
			}, nil
		},
	}

	m := New(catPT, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	// Press Ctrl+F to open search prompt, type "gaules", and press Enter.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = updated.(Model)
	if !m.Searching() {
		t.Fatal("expected Searching()=true after Ctrl+F")
	}
	for _, r := range "gaules" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, searchCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.Mode() != ModeSearch || !m.Loading() {
		t.Fatalf("expected ModeSearch and Loading=true, got Mode=%q Loading=%v", m.Mode(), m.Loading())
	}
	if searchCmd == nil {
		t.Fatal("expected non-nil searchCmd")
	}

	updated, _ = m.Update(searchCmd())
	m = updated.(Model)
	if searchedQuery != "gaules" {
		t.Fatalf("searchedQuery = %q, want %q", searchedQuery, "gaules")
	}

	view := m.View()
	if !strings.Contains(view, "Gaules (gaules)") || !strings.Contains(view, "Riot Games (riotgames)") {
		t.Fatalf("expected search view to show both display name and login, got: %q", view)
	}

	// Press Ctrl+R to return to categories.
	updated, reloadCatsCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if m.Mode() != ModeCategories {
		t.Fatalf("after Ctrl+R, Mode() = %q, want %q", m.Mode(), ModeCategories)
	}
	if reloadCatsCmd != nil {
		updated, _ = m.Update(reloadCatsCmd())
		m = updated.(Model)
	}
	if len(m.Categories()) != 2 || !strings.Contains(m.View(), "Just Chatting") {
		t.Fatalf("expected categories restored after Ctrl+R, got view: %q", m.View())
	}
}

func TestEmptyAndErrorStatesInChannelsAndSearch(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	channelAttempts := 0
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{{ID: "1", Name: "Artifact", Viewers: twitch.IntPtr(10)}},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			channelAttempts++
			if channelAttempts == 1 {
				return twitch.Page[twitch.Channel]{}, &twitch.Error{
					Kind: twitch.Rede,
					Op:   "channels",
					Err:  errors.New("connection reset"),
				}
			}
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{}}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{}}, nil
		},
	}

	m := New(catPT, WithSource(src))
	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	// Open category -> first attempt fails with Rede error.
	updated, chCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(chCmd())
	m = updated.(Model)

	if m.Err() == nil {
		t.Fatal("expected error state on failed channel load")
	}
	if !strings.Contains(m.View(), "Erro de rede") || !strings.Contains(m.View(), "Pressione r para tentar novamente") {
		t.Fatalf("expected explicit error and retry hint in view, got: %q", m.View())
	}

	// Retry with 'r' -> succeeds with empty channel list and shows explicit empty message.
	updated, retryCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	updated, _ = m.Update(retryCmd())
	m = updated.(Model)

	if m.Err() != nil {
		t.Fatalf("unexpected error after retry: %v", m.Err())
	}
	if !strings.Contains(m.View(), "Nenhum canal ao vivo nesta categoria.") {
		t.Fatalf("expected explicit empty category message in view, got: %q", m.View())
	}

	// Run global search with no results -> shows explicit empty search message.
	m = m.SetFilter("ninguem_online_xyz")
	updated, searchCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = updated.(Model)
	updated, _ = m.Update(searchCmd())
	m = updated.(Model)

	if !strings.Contains(m.View(), "Nenhum resultado para esta busca.") {
		t.Fatalf("expected explicit empty search message in view, got: %q", m.View())
	}
}

func TestInvalidTokenWarnsOnceInStatusBarWithoutCrashingApp(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}

	secretToken := "supersecrettoken1234567890abcd"
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "509658", Name: "Just Chatting", Viewers: twitch.IntPtr(1000)},
					{ID: "32399", Name: "Counter-Strike", Viewers: twitch.IntPtr(2000)},
				},
			}, nil
		},
	}

	m := New(
		catPT,
		WithSource(src),
		WithTokenCheck(func(ctx context.Context) error {
			return errors.New("token rejected 401")
		}),
	)

	updated, _ := m.Update(m.Init()())
	m = updated.(Model)

	if m.Err() != nil {
		t.Fatalf("expected nil Err() so UI stays usable, got %v", m.Err())
	}
	if len(m.Categories()) != 2 {
		t.Fatalf("expected 2 categories loaded, got %d", len(m.Categories()))
	}
	view := m.View()
	if !strings.Contains(view, "ttv-tui login --from-browser") {
		t.Fatalf("expected renewal notice in status bar, got view: %q", view)
	}
	if strings.Contains(view, secretToken) {
		t.Fatalf("view leaked token: %q", view)
	}

	// Next user interaction clears the one-time notice while keeping navigation working.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.AuthNotice() != "" {
		t.Fatalf("expected AuthNotice cleared after key press, got %q", m.AuthNotice())
	}
	if m.HighlightedID() != "32399" {
		t.Fatalf("HighlightedID = %q, want 32399", m.HighlightedID())
	}
}

type fakePlayback struct {
	started   []string
	stopped   int
	pauses    int
	volDeltas []int
	startMsg  PlayerStatusMsg
}

func (f *fakePlayback) Start(login string) tea.Cmd {
	f.started = append(f.started, login)
	msg := f.startMsg
	if msg.State == "" {
		msg = PlayerStatusMsg{Generation: uint64(len(f.started)), State: PlayerStateStarting, Login: login, Volume: 100, FromStart: true}
	} else {
		msg.FromStart = true
		msg.Login = login
		if msg.Volume == 0 {
			msg.Volume = 100
		}
	}
	return func() tea.Msg { return msg }
}

func (f *fakePlayback) Stop() tea.Cmd {
	f.stopped++
	return func() tea.Msg {
		return PlayerStatusMsg{State: PlayerStateEnded, FromStart: false}
	}
}

func (f *fakePlayback) TogglePause() tea.Cmd {
	f.pauses++
	return func() tea.Msg { return nil }
}

func (f *fakePlayback) AdjustVolume(delta int) tea.Cmd {
	f.volDeltas = append(f.volDeltas, delta)
	return func() tea.Msg { return nil }
}

func (f *fakePlayback) Listen() tea.Cmd { return nil }

func TestChannelSelectedStartsPlaybackAndShowsStatus(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))
	ch := twitch.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: ch})
	m = updated.(Model)
	if !m.playerSwitching {
		t.Fatal("expected playerSwitching after ChannelSelected")
	}
	if m.PlayerState() != PlayerStateStarting {
		t.Fatalf("PlayerState=%q want iniciando", m.PlayerState())
	}
	if cmd == nil {
		t.Fatal("expected Start cmd")
	}
	if len(pb.started) != 1 || pb.started[0] != "gaules" {
		t.Fatalf("started=%v", pb.started)
	}

	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.playerSwitching {
		t.Fatal("expected playerSwitching cleared after FromStart handshake")
	}
	if m.PlayerGeneration() != 1 {
		t.Fatalf("PlayerGeneration=%d want 1", m.PlayerGeneration())
	}
	view := m.View()
	if !strings.Contains(view, "Starting gaules") {
		t.Fatalf("status bar missing starting text: %q", view)
	}

	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)
	if m.PlayerState() != PlayerStatePlaying {
		t.Fatalf("PlayerState=%q", m.PlayerState())
	}
	if !strings.Contains(m.View(), "Playing gaules") {
		t.Fatalf("view=%q", m.View())
	}
	if !strings.Contains(m.View(), "vol 100") {
		t.Fatalf("view missing volume: %q", m.View())
	}
}

func TestPlayerControlKeys(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 50})
	m = updated.(Model)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected TogglePause cmd")
	}
	_ = cmd // fake returns nil msg
	if pb.pauses != 1 {
		t.Fatalf("pauses=%d want 1", pb.pauses)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected AdjustVolume cmd")
	}
	_ = cmd
	if len(pb.volDeltas) != 1 || pb.volDeltas[0] != 5 {
		t.Fatalf("volDeltas=%v want [5]", pb.volDeltas)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected AdjustVolume -5 cmd")
	}
	_ = cmd
	if len(pb.volDeltas) != 2 || pb.volDeltas[1] != -5 {
		t.Fatalf("volDeltas=%v want [5 -5]", pb.volDeltas)
	}

	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 45})
	m = updated.(Model)
	if m.PlayerState() != PlayerStatePaused {
		t.Fatalf("PlayerState=%q", m.PlayerState())
	}
	if !strings.Contains(m.View(), "Paused gaules") || !strings.Contains(m.View(), "vol 45") {
		t.Fatalf("view=%q", m.View())
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected Stop cmd")
	}
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if pb.stopped != 1 {
		t.Fatalf("stopped=%d want 1", pb.stopped)
	}
	if m.PlayerState() != PlayerStateEnded {
		t.Fatalf("PlayerState=%q want encerrado", m.PlayerState())
	}
	if m.Quitting() {
		t.Fatal("Ctrl+X must not quit the app")
	}
}

func TestSpaceDoesNotPauseWhileComposing(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)

	m.composing = true
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if pb.pauses != 0 {
		t.Fatalf("Space while composing must not pause; pauses=%d", pb.pauses)
	}
	if cmd != nil {
		t.Fatal("expected no player cmd while composing Space")
	}
	if m.composeDraft != " " {
		t.Fatalf("composeDraft=%q want space", m.composeDraft)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updated.(Model)
	if len(pb.volDeltas) != 0 {
		t.Fatalf("+ while composing must not change volume; deltas=%v", pb.volDeltas)
	}
	if m.composeDraft != " +" {
		t.Fatalf("composeDraft=%q want %q", m.composeDraft, " +")
	}
	_ = cmd
}

func TestPlayerStatusDiscardsStaleGeneration(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "aaa"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	updated, cmd = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "bbb"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.PlayerGeneration() != 2 {
		t.Fatalf("gen=%d want 2", m.PlayerGeneration())
	}

	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "aaa"})
	m = updated.(Model)
	if m.PlayerState() == PlayerStatePlaying && m.PlayerLogin() == "aaa" {
		t.Fatal("stale generation event must not change current player status")
	}
	if m.PlayerLogin() != "bbb" {
		t.Fatalf("login=%q want bbb", m.PlayerLogin())
	}
}

// A break event from an older generation must not show the interval (or its
// counter) for the current channel; a current-generation break ending returns
// the bar to playing without any key press.
func TestPlayerBreakStaleGenerationAndRecovery(t *testing.T) {
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catPT, WithPlayback(pb))

	for _, login := range []string{"aaa", "bbb"} {
		updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: login}})
		m = updated.(Model)
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	updated, _ := m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "bbb", Volume: 80})
	m = updated.(Model)

	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStateBreak, Login: "aaa", BreakElapsed: 9 * time.Second, Volume: 80})
	m = updated.(Model)
	if m.PlayerState() != PlayerStatePlaying {
		t.Fatalf("stale break changed state to %s", m.PlayerState())
	}
	if strings.Contains(m.playerStatusText(), "Intervalo") {
		t.Fatalf("status shows interval from old generation: %q", m.playerStatusText())
	}

	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStateBreak, Login: "bbb", BreakElapsed: 3 * time.Second, Volume: 80})
	m = updated.(Model)
	if got := m.playerStatusText(); !strings.Contains(got, "Intervalo comercial: o vídeo volta sozinho (3s)") {
		t.Fatalf("break text = %q", got)
	}
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "bbb", Volume: 80})
	m = updated.(Model)
	if got := m.playerStatusText(); m.PlayerState() != PlayerStatePlaying || strings.Contains(got, "Intervalo") {
		t.Fatalf("after break end: state=%s text=%q", m.PlayerState(), got)
	}
}

func TestCtrlQStopsPlayback(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m = updated.(Model)
	if !m.Quitting() {
		t.Fatal("expected quitting")
	}
	if pb.stopped != 1 {
		t.Fatalf("stopped=%d want 1", pb.stopped)
	}
}

func TestPlayerVolumeClampedInUIAndStaleSwitchEventsDiscarded(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))

	// Select channel A (gen 1) and finish start handshake.
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "chanA"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// Select channel B (gen 2): while playerSwitching is true (before Start cmd finishes),
	// an in-flight pump event from gen 1 without FromStart must be ignored.
	updated, cmdB := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "chanB"}})
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStatePlaying,
		Login:      "chanA",
		Volume:     35,
		FromStart:  false,
	})
	m = updated.(Model)
	if m.PlayerLogin() != "chanB" || m.PlayerState() != PlayerStateStarting {
		t.Fatalf("in-flight gen 1 event leaked during switch: login=%q state=%q", m.PlayerLogin(), m.PlayerState())
	}

	// Complete channel B Start handshake (gen 2).
	updated, _ = m.Update(cmdB())
	m = updated.(Model)
	if m.PlayerGeneration() != 2 {
		t.Fatalf("PlayerGeneration = %d, want 2", m.PlayerGeneration())
	}

	// Out-of-bounds high volume in PlayerStatusMsg is clamped to 100.
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "chanB", Volume: 135})
	m = updated.(Model)
	if m.PlayerVolume() != 100 || !strings.Contains(m.View(), "vol 100") {
		t.Fatalf("PlayerVolume = %d (want 100), view = %q", m.PlayerVolume(), m.View())
	}

	// Volume 0 (muted) on playing state updates PlayerVolume to 0 and renders "vol 0".
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "chanB", Volume: 0})
	m = updated.(Model)
	if m.PlayerVolume() != 0 || !strings.Contains(m.View(), "vol 0") {
		t.Fatalf("PlayerVolume = %d (want 0), view = %q", m.PlayerVolume(), m.View())
	}

	// Negative volume is clamped to 0.
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePaused, Login: "chanB", Volume: -15})
	m = updated.(Model)
	if m.PlayerVolume() != 0 {
		t.Fatalf("PlayerVolume = %d, want 0", m.PlayerVolume())
	}
}
