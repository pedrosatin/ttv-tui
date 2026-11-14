package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// countingSource is a catalog that records what it was asked.
type countingSource struct {
	fakeSource
	mu       sync.Mutex
	asked    []string
	category []catalog.Category
	channels []catalog.Channel
	hits     []catalog.Channel
	catHits  []catalog.Category
	err      error
}

func (s *countingSource) note(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, call)
}

func (s *countingSource) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func (s *countingSource) Categories(_ context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	s.note("categories:" + page.Cursor)
	return catalog.Page[catalog.Category]{Items: s.category}, s.err
}

func (s *countingSource) Channels(_ context.Context, id string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	s.note("channels:" + id + ":" + page.Cursor)
	return catalog.Page[catalog.Channel]{Items: s.channels}, s.err
}

func (s *countingSource) Search(_ context.Context, query string) (catalog.Page[catalog.Channel], error) {
	s.note("search:" + query)
	return catalog.Page[catalog.Channel]{Items: s.hits}, s.err
}

func (s *countingSource) SearchCategories(_ context.Context, query string) ([]catalog.Category, error) {
	s.note("searchCategories:" + query)
	return s.catHits, s.err
}

func twitchCatalog() *countingSource {
	return &countingSource{
		category: []catalog.Category{{ID: "509658", Name: "Just Chatting", Viewers: catalog.IntPtr(300000)}},
		channels: []catalog.Channel{{ID: "1", Login: "twitchone", DisplayName: "TwitchOne", Viewers: catalog.IntPtr(10)}},
	}
}

func kickCatalog() *countingSource {
	return &countingSource{
		category: []catalog.Category{
			{ID: "28", Name: "Slots & Casino", Viewers: catalog.IntPtr(73321)},
			{ID: "68", Name: "Chess", Viewers: catalog.IntPtr(5002)},
		},
		channels: []catalog.Channel{liveKick("gmhikaru", 4268, "Total World Champs", "Chess")},
		hits: []catalog.Channel{
			{ID: "kick:gaules", Login: "gaules", DisplayName: "Gaules", Platform: platform.Kick, URL: "https://kick.com/gaules", Availability: catalog.AvailabilityLive},
		},
		catHits: []catalog.Category{{ID: "187", Name: "Project Zomboid", Viewers: catalog.IntPtr(900)}},
	}
}

// twoPlatforms is a model with the catalogs of Twitch and Kick, already past
// the first load.
func twoPlatforms(t *testing.T, lang string, tw, kick *countingSource, opts ...Option) Model {
	t.Helper()
	all := append([]Option{
		WithPlatformCatalog(platform.Twitch, tw, tw),
		WithPlatformCatalog(platform.Kick, kick, kick),
	}, opts...)
	m := New(mustCatalog(t, lang), all...)
	return drainAll(t, m, m.Init())
}

var keyP = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}

// openFirst presses Enter on the highlighted row until a channel is open, so
// the chat panel exists and the two panels share the screen.
func openFirst(t *testing.T, m Model) Model {
	t.Helper()
	for range 2 {
		if m.SelectedChannel() != nil {
			break
		}
		var cmd tea.Cmd
		m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = drainAll(t, m, cmd)
	}
	if m.SelectedChannel() == nil || !m.chat.hasPanel() {
		t.Fatalf("no channel open: selected %+v", m.SelectedChannel())
	}
	return m
}

func pressP(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := pressKey(m, keyP)
	return drainAll(t, m, cmd)
}

func categoryNames(m Model) []string {
	var names []string
	for _, c := range m.Categories() {
		names = append(names, c.Name)
	}
	return names
}

func TestPlatformKey_SwitchesTheListAndBack(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	m := twoPlatforms(t, i18n.LangEN, tw, kick)
	if m.ListPlatform() != platform.Twitch || strings.Join(categoryNames(m), ",") != "Just Chatting" {
		t.Fatalf("start: platform %q, categories %v", m.ListPlatform(), categoryNames(m))
	}
	if len(kick.calls()) != 0 {
		t.Fatalf("Kick was asked before the list went there: %v", kick.calls())
	}

	m, cmd := pressKey(m, keyP)
	if m.ListPlatform() != platform.Kick || !m.Loading() || len(m.Categories()) != 0 {
		t.Fatalf("after p: platform %q loading %v categories %v", m.ListPlatform(), m.Loading(), categoryNames(m))
	}
	m = drainAll(t, m, cmd)
	if got := strings.Join(categoryNames(m), ","); got != "Slots & Casino,Chess" {
		t.Fatalf("Kick categories = %q", got)
	}
	if m.Mode() != ModeCategories || m.Loading() {
		t.Fatalf("mode %q loading %v", m.Mode(), m.Loading())
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Kick · Live Categories", "Slots & Casino", "73.3k"} {
		if !strings.Contains(view, want) {
			t.Errorf("list missing %q:\n%s", want, view)
		}
	}
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, "list: Kick") {
		t.Errorf("bar does not name the platform of the list: %q", bar)
	}

	m = pressP(t, m)
	if m.ListPlatform() != platform.Twitch || strings.Join(categoryNames(m), ",") != "Just Chatting" {
		t.Fatalf("second p: platform %q, categories %v", m.ListPlatform(), categoryNames(m))
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Twitch · Live Categories") || strings.Contains(view, "Slots") {
		t.Errorf("list after going back:\n%s", view)
	}
}

func TestPlatformKey_ChannelsAndSearchComeFromThatPlatform(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	m := twoPlatforms(t, i18n.LangEN, tw, kick)
	m = pressP(t, m)
	before := len(tw.calls())

	// Down to Chess, then into it.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	if m.Mode() != ModeChannels || len(m.Channels()) != 1 || m.Channels()[0].Login != "gmhikaru" {
		t.Fatalf("mode %q channels %+v", m.Mode(), m.Channels())
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Kick · Live Channels — Chess", "gmhikaru", "4.3k", "Total World Champs"} {
		if !strings.Contains(view, want) {
			t.Errorf("channel list missing %q:\n%s", want, view)
		}
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	if m.Mode() != ModeSearch || len(m.Channels()) != 1 || m.Channels()[0].Login != "gaules" {
		t.Fatalf("search: mode %q channels %+v", m.Mode(), m.Channels())
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Project Zomboid") || !strings.Contains(view, "Gaules") {
		t.Errorf("search results:\n%s", view)
	}

	calls := strings.Join(kick.calls(), " ")
	for _, want := range []string{"channels:68:", "search:gaules", "searchCategories:gaules"} {
		if !strings.Contains(calls, want) {
			t.Errorf("Kick was not asked %q: %v", want, kick.calls())
		}
	}
	if len(tw.calls()) != before {
		t.Errorf("Twitch was asked while the list showed Kick: %v", tw.calls()[before:])
	}
}

func TestPlatformKey_SinglePlatformHasNothingToSwitchTo(t *testing.T) {
	tw := twitchCatalog()
	m := New(mustCatalog(t, i18n.LangEN), WithSource(tw))
	m = drainAll(t, m, m.Init())
	asked := len(tw.calls())

	m, cmd := pressKey(m, keyP)
	if cmd != nil || m.ListPlatform() != platform.Twitch || len(tw.calls()) != asked || len(m.Categories()) != 1 {
		t.Fatalf("p with one platform changed the list: cmd=%v platform=%q", cmd != nil, m.ListPlatform())
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "Twitch ·") {
		t.Errorf("a single platform is named in the header:\n%s", view)
	}
	if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, "list:") {
		t.Errorf("a single platform is named in the bar: %q", bar)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if help := ansi.Strip(m.View()); strings.Contains(help, "Platform:") {
		t.Errorf("help offers a key that does nothing:\n%s", help)
	}
}

func TestPlatformKey_OpensOnTheDefaultPlatform(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	cat := mustCatalog(t, i18n.LangEN)

	// Kick configured first: Twitch still opens, and Kick is the next one.
	m := New(cat, WithPlatformCatalog(platform.Kick, kick, kick), WithPlatformCatalog(platform.Twitch, tw, tw))
	if m.ListPlatform() != platform.Twitch || !m.Loading() {
		t.Fatalf("opened on %q (loading %v), want twitch", m.ListPlatform(), m.Loading())
	}
	m = drainAll(t, m, m.Init())
	if m = pressP(t, m); m.ListPlatform() != platform.Kick {
		t.Fatalf("p went to %q, want kick", m.ListPlatform())
	}

	// Without the default platform the first one configured opens.
	only := New(cat, WithPlatformCatalog(platform.Kick, kick, kick))
	if only.ListPlatform() != platform.Kick {
		t.Fatalf("opened on %q, want kick", only.ListPlatform())
	}
	only = drainAll(t, only, only.Init())
	if got := strings.Join(categoryNames(only), ","); got != "Slots & Casino,Chess" {
		t.Fatalf("categories = %q", got)
	}

	// A platform with neither capability has no turn.
	none := New(cat, WithSource(tw), WithPlatformCatalog(platform.Kick, nil, nil))
	if _, cmd := pressKey(none, keyP); cmd != nil {
		t.Error("p switched to a platform with no catalog")
	}
}

func TestPlatformKey_LeavesPlaybackAndChatAlone(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	pb := &fakePlayback{}
	fc := &fakeChat{}
	m := twoPlatforms(t, i18n.LangEN, tw, kick, WithPlayback(pb), WithChat(fc))
	// Into the Twitch category and its channel.
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "twitchone", Volume: 100})
	m = updated.(Model)
	if len(pb.started) != 1 || m.PlayerState() != PlayerStatePlaying {
		t.Fatalf("setup: started %v state %q", pb.started, m.PlayerState())
	}

	m = pressP(t, m)
	if m.ListPlatform() != platform.Kick || m.Mode() != ModeCategories {
		t.Fatalf("platform %q mode %q", m.ListPlatform(), m.Mode())
	}
	if pb.stopped != 0 || len(pb.started) != 1 || m.PlayerState() != PlayerStatePlaying || m.PlayerLogin() != "twitchone" {
		t.Errorf("playback touched: stopped=%d started=%v state=%q", pb.stopped, pb.started, m.PlayerState())
	}
	if sel := m.SelectedChannel(); sel == nil || sel.Login != "twitchone" {
		t.Errorf("selected channel = %+v, want the one playing", sel)
	}
	// The category belonged to Twitch; the bar must not offer it on Kick.
	if m.SelectedCategory() != nil {
		t.Errorf("selected category survived the switch: %+v", m.SelectedCategory())
	}
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "twitchone") || !strings.Contains(bar, "list: Kick") {
		t.Errorf("bar = %q, want what plays and the platform of the list", bar)
	}
}

// An answer of the platform the list just left must not land on the new one.
func TestPlatformKey_DropsTheLateAnswerOfThePreviousPlatform(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	m := New(mustCatalog(t, i18n.LangEN), WithPlatformCatalog(platform.Twitch, tw, tw), WithPlatformCatalog(platform.Kick, kick, kick))
	late := m.Init()
	m = pressP(t, m)
	if got := strings.Join(categoryNames(m), ","); got != "Slots & Casino,Chess" {
		t.Fatalf("Kick categories = %q", got)
	}
	m = drainAll(t, m, late)
	if got := strings.Join(categoryNames(m), ","); got != "Slots & Casino,Chess" {
		t.Fatalf("the Twitch answer replaced the Kick list: %q", got)
	}
}

func TestPlatformKey_OnlyActsOnTheList(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	fc := &fakeChat{}

	// While filtering, p is text.
	m := twoPlatforms(t, i18n.LangEN, tw, kick)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m, _ = pressKey(m, keyP)
	if m.ListPlatform() != platform.Twitch || m.Filter() != "p" {
		t.Errorf("filter: platform %q filter %q", m.ListPlatform(), m.Filter())
	}

	// While searching, too.
	m = twoPlatforms(t, i18n.LangEN, tw, kick)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m, _ = pressKey(m, keyP)
	if m.ListPlatform() != platform.Twitch || !m.Searching() {
		t.Errorf("search: platform %q searching %v", m.ListPlatform(), m.Searching())
	}

	// On the chat panel p belongs to nobody, as f and l.
	m = twoPlatforms(t, i18n.LangEN, tw, kick, WithChat(fc))
	m = openFirst(t, resize(m, 120, 30))
	m, _ = pressKey(m, keyTab)
	if m.Focus() != FocusChat {
		t.Fatalf("focus = %q, want chat", m.Focus())
	}
	if m, _ = pressKey(m, keyP); m.ListPlatform() != platform.Twitch {
		t.Errorf("p on the chat panel switched the list to %q", m.ListPlatform())
	}

	// With the list hidden the key says why nothing happened.
	m = twoPlatforms(t, i18n.LangEN, tw, kick, WithChat(fc))
	m = openFirst(t, resize(m, 120, 30))
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlH})
	m, _ = pressKey(m, keyP)
	if m.ListPlatform() != platform.Twitch || m.StatusNotice() != m.t("layout.hidden_list_key") {
		t.Errorf("hidden list: platform %q notice %q", m.ListPlatform(), m.StatusNotice())
	}
}

func TestHelp_NamesThePlatformKey(t *testing.T) {
	tests := map[string]string{
		i18n.LangEN:   "  Platform: p switches the list to the next (Twitch, Kick) · video and chat stay",
		i18n.LangPTBR: "  Plataforma: p troca a lista para a próxima (Twitch, Kick) · vídeo e chat ficam",
	}
	for lang, want := range tests {
		m := twoPlatforms(t, lang, twitchCatalog(), kickCatalog())
		// One row at 80 columns, like the rows around it.
		m = resize(m, 80, 60)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		found := false
		for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			found = found || strings.TrimRight(line, " ") == want
		}
		if !found {
			t.Errorf("%s: help has no row %q:\n%s", lang, want, ansi.Strip(m.View()))
		}
	}
}

// The header always names the platform; the bar repeats it only in the room
// the rest leaves, so at 80 columns it never costs the focus hint.
func TestStatusBar_PlatformTokenTakesOnlyTheRoomLeft(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		tw, kick := twitchCatalog(), kickCatalog()
		m := twoPlatforms(t, lang, tw, kick, WithChat(&fakeChat{}), WithPlayback(&fakePlayback{}))
		m = resize(m, 80, 24)
		m = pressP(t, m)
		token := fmt.Sprintf(m.t("status.list_platform"), "Kick")

		// At rest there is room.
		bar := ansi.Strip(m.StatusBar())
		assertStatusWidth(t, bar, 80)
		if !strings.Contains(bar, token) {
			t.Errorf("%s: bar at rest = %q, want %q", lang, bar, token)
		}

		// While something plays, the focus hint and the volume come first.
		m = pressP(t, m)
		m = openFirst(t, m)
		updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "twitchone", Volume: 100})
		m = updated.(Model)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
		m = pressP(t, m)
		bar = ansi.Strip(m.StatusBar())
		assertStatusWidth(t, bar, 80)
		for _, keep := range []string{m.t("status.focus_list"), "twitchone", "vol 100"} {
			if !strings.Contains(bar, keep) {
				t.Errorf("%s: the bar lost %q at 80 columns: %q", lang, keep, bar)
			}
		}
		if view := ansi.Strip(m.View()); !strings.Contains(strings.SplitN(view, "\n", 2)[0], "Kick · ") {
			t.Errorf("%s: header does not name the platform:\n%s", lang, view)
		}
	}
}

func kickBlocked() error {
	return &catalog.Error{Platform: platform.Kick, Kind: catalog.Bloqueio, Op: "categories", StatusCode: 403, Err: errors.New("request refused by the site")}
}

var savedChannels = []catalog.Channel{
	{ID: "9", Login: "twitchfav", DisplayName: "TwitchFav"},
	{ID: "kick:kickfav", Login: "kickfav", DisplayName: "KickFav", Platform: platform.Kick, URL: "https://kick.com/kickfav"},
}

// A refused catalog is not a broken list: the saved channels stay on screen and
// still open, and the notice names the catalog.
func TestBlockedCatalog_KeepsTheListAndSaysUnavailable(t *testing.T) {
	tests := map[string][2]string{
		i18n.LangEN:   {"Kick catalog unavailable right now", "r tries again · p switches platform"},
		i18n.LangPTBR: {"Catálogo da Kick indisponível agora", "r tenta de novo · p troca de plataforma"},
	}
	for lang, want := range tests {
		t.Run(lang, func(t *testing.T) {
			tw, kick := twitchCatalog(), kickCatalog()
			kick.err = kickBlocked()
			pb := &fakePlayback{}
			m := twoPlatforms(t, lang, tw, kick, WithFavorites(savedChannels), WithPlayback(pb))
			m = resize(m, 120, 30)
			m = pressP(t, m)

			view := ansi.Strip(m.View())
			for _, text := range []string{want[0], want[1], "KickFav"} {
				if !strings.Contains(view, text) {
					t.Errorf("list missing %q:\n%s", text, view)
				}
			}
			if strings.Contains(view, "TwitchFav") {
				t.Fatal("favorito da Twitch apareceu na Kick")
			}
			if strings.Contains(view, m.t("error.action_retry")) || strings.Contains(view, m.t("error.network")) {
				t.Errorf("the refusal took the list down:\n%s", view)
			}
			bar := ansi.Strip(m.StatusBar())
			if !strings.Contains(bar, want[0]) || !strings.Contains(bar, "!") {
				t.Errorf("bar = %q, want the unavailable warning", bar)
			}

			// The rows still move and open.
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			if it, ok := m.HighlightedItem(); !ok || it.Channel == nil || it.Channel.Login != "kickfav" {
				t.Fatalf("highlight under the notice = %+v (ok=%v)", it, ok)
			}
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainAll(t, m, cmd)
			if len(pb.started) != 1 || pb.started[0] != "kick:kickfav" {
				t.Fatalf("saved channel did not open: %v", pb.started)
			}

			// r asks again; an answer clears the notice.
			kick.err = nil
			m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			m = drainAll(t, m, cmd)
			view = ansi.Strip(m.View())
			if strings.Contains(view, want[0]) || !strings.Contains(view, "Slots & Casino") || m.Err() != nil {
				t.Errorf("after retry (err %v):\n%s", m.Err(), view)
			}
		})
	}
}

func TestBlockedCatalog_WithNothingSavedShowsOnlyTheNotice(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	kick.err = kickBlocked()
	m := twoPlatforms(t, i18n.LangEN, tw, kick)
	m = resize(m, 120, 30)
	m = pressP(t, m)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Kick catalog unavailable right now") {
		t.Errorf("notice missing:\n%s", view)
	}
	// Not an empty category list: nothing was listed.
	for _, unwanted := range []string{m.t("categories.empty"), m.t("list.not_loaded"), m.t("list.empty_action")} {
		if strings.Contains(view, unwanted) {
			t.Errorf("list shows %q next to the notice:\n%s", unwanted, view)
		}
	}
	// p still leaves.
	if m = pressP(t, m); m.ListPlatform() != platform.Twitch || m.Err() != nil || len(m.Categories()) != 1 {
		t.Errorf("after p: platform %q err %v categories %v", m.ListPlatform(), m.Err(), categoryNames(m))
	}
}

func TestBlockedCatalog_FitsIn80Columns(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		tw, kick := twitchCatalog(), kickCatalog()
		kick.err = kickBlocked()
		fc := &fakeChat{}
		m := twoPlatforms(t, lang, tw, kick, WithFavorites(savedChannels), WithChat(fc))
		m = openFirst(t, resize(m, 80, 24))
		m = pressP(t, m)

		listW, chatW := m.panelWidths()
		if listW == 0 || chatW == 0 {
			t.Fatalf("%s: 80 columns lost a panel: list %d chat %d", lang, listW, chatW)
		}
		var list []string
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if len(lines) != 24 {
			t.Fatalf("%s: %d rows, want 24", lang, len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > 80 {
				t.Errorf("%s: row %d is %d cells wide: %q", lang, i, w, line)
			}
			if i < len(lines)-1 {
				list = append(list, strings.TrimSpace(ansi.Truncate(line, listW, "")))
			}
		}
		// Wrapped, never cut: every word of the notice is in the list column.
		column := strings.Join(list, " ")
		notice := fmt.Sprintf(m.t("catalog.unavailable"), "Kick") + " " + m.t("catalog.unavailable_action_switch")
		for _, word := range strings.Fields(notice) {
			if !strings.Contains(column, word) {
				t.Errorf("%s: notice lost %q at 80 columns:\n%s", lang, word, strings.Join(list, "\n"))
			}
		}
		if !strings.Contains(column, "Kick · ") || !strings.Contains(column, "KickFav") {
			t.Errorf("%s: header or saved rows missing:\n%s", lang, strings.Join(list, "\n"))
		}
		assertStatusWidth(t, ansi.Strip(m.StatusBar()), 80)
	}
}

// What plays leads the bar; the notice stays in the list.
func TestBlockedCatalog_ThePlayerKeepsTheBar(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	kick.err = kickBlocked()
	m := twoPlatforms(t, i18n.LangEN, tw, kick, WithPlayback(&fakePlayback{}))
	m = resize(m, 120, 30)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "twitchone", Volume: 100})
	m = updated.(Model)
	// Past the one-time notice about the mpv window.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})

	m = pressP(t, m)
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "twitchone") || strings.Contains(bar, "unavailable") {
		t.Errorf("bar = %q, want the player in the lead", bar)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Kick catalog unavailable right now") {
		t.Errorf("notice missing from the list:\n%s", view)
	}
}

// The second page refused leaves the first one where it was.
func TestBlockedCatalog_ARefusedNextPageKeepsTheLoadedRows(t *testing.T) {
	var mu sync.Mutex
	blocked := false
	nextPageAsks := 0
	kick := &fakeSource{categoriesFn: func(_ context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
		mu.Lock()
		defer mu.Unlock()
		if page.Cursor != "" {
			nextPageAsks++
		}
		if page.Cursor != "" && blocked {
			return catalog.Page[catalog.Category]{}, kickBlocked()
		}
		if page.Cursor != "" {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "3", Name: "Third", Viewers: catalog.IntPtr(1)}}}, nil
		}
		return catalog.Page[catalog.Category]{
			Items:   []catalog.Category{{ID: "1", Name: "First", Viewers: catalog.IntPtr(9)}, {ID: "2", Name: "Second", Viewers: catalog.IntPtr(8)}},
			Cursor:  "2",
			HasMore: true,
		}, nil
	}}
	m := New(mustCatalog(t, i18n.LangEN), WithPlatformCatalog(platform.Kick, kick, kick))
	m = resize(m, 120, 30)
	m = drainAll(t, m, m.Init())

	mu.Lock()
	blocked = true
	mu.Unlock()
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = drainAll(t, m, cmd)
	view := ansi.Strip(m.View())
	for _, want := range []string{"First", "Second", "Kick catalog unavailable right now"} {
		if !strings.Contains(view, want) {
			t.Fatalf("after the refused page, missing %q:\n%s", want, view)
		}
	}
	if it, ok := m.HighlightedItem(); !ok || it.Category == nil || it.Category.Name != "Second" {
		t.Fatalf("highlight = %+v (ok=%v), want Second", it, ok)
	}

	// Scrolling at the last row does not ask the site again: a held key would
	// send one request per refusal to the wall that blocked the app.
	for range 10 {
		m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
		m = drainAll(t, m, cmd)
	}
	mu.Lock()
	asks := nextPageAsks
	mu.Unlock()
	if asks != 1 {
		t.Fatalf("next page asked %d times after 10 more keys, want only the refused one", asks)
	}

	// "r", as the notice says, tries again once the site answers.
	mu.Lock()
	blocked = false
	mu.Unlock()
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = drainAll(t, m, cmd)
	if view = ansi.Strip(m.View()); strings.Contains(view, "unavailable") {
		t.Fatalf("after r:\n%s", view)
	}
	for range 3 {
		m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
		m = drainAll(t, m, cmd)
	}
	if view = ansi.Strip(m.View()); !strings.Contains(view, "Third") || strings.Contains(view, "unavailable") {
		t.Fatalf("after the page came:\n%s", view)
	}
}

// Any other failure still replaces the rows, and names who failed.
func TestCatalogError_NamesThePlatformThatFailed(t *testing.T) {
	tests := []struct {
		lang string
		kind catalog.ErrorKind
		want string
	}{
		{i18n.LangEN, catalog.Servidor, "Kick service error: the service failed to respond."},
		{i18n.LangEN, catalog.Contrato, "Unexpected response from the Kick API."},
		{i18n.LangPTBR, catalog.Servidor, "Erro no servidor da Kick: o serviço falhou ao responder."},
		{i18n.LangPTBR, catalog.Contrato, "Resposta inesperada da API da Kick."},
	}
	for _, tc := range tests {
		tw, kick := twitchCatalog(), kickCatalog()
		kick.err = &catalog.Error{Platform: platform.Kick, Kind: tc.kind, Op: "categories", StatusCode: 500}
		m := twoPlatforms(t, tc.lang, tw, kick, WithFavorites(savedChannels))
		m = resize(m, 200, 30)
		m = pressP(t, m)
		view := ansi.Strip(m.View())
		if !strings.Contains(view, tc.want) || !strings.Contains(view, m.t("error.action_retry")) {
			t.Errorf("%s %s: list missing %q:\n%s", tc.lang, tc.kind, tc.want, view)
		}
		if strings.Contains(view, "KickFav") {
			t.Errorf("%s %s: a hard error left the rows on screen:\n%s", tc.lang, tc.kind, view)
		}
	}

	// An error with no platform is of the platform on screen.
	m := New(mustCatalog(t, i18n.LangEN))
	if got := m.errorMessage(&catalog.Error{Kind: catalog.Servidor}); got != "Twitch service error: the service failed to respond." {
		t.Errorf("error without a platform = %q", got)
	}
	if got := m.errorMessage(&catalog.Error{Kind: catalog.Contrato}); got != "Unexpected response from the Twitch API." {
		t.Errorf("error without a platform = %q", got)
	}
}

// A search hit says a channel is live and nothing else; opening it asks the
// platform for the rest. A row of a category listing already has it.
func TestOpenChannel_AsksForStreamDataOnlyWhenTheRowHasNone(t *testing.T) {
	tw, kick := twitchCatalog(), kickCatalog()
	resolver := &platformResolver{answer: []catalog.Channel{liveKick("gaules", 3681, "GAMEPLAY DO ALASTRO", "Project Zomboid")}}
	m := twoPlatforms(t, i18n.LangEN, tw, kick, WithPlayback(&fakePlayback{}), WithPlatformResolver(platform.Kick, resolver))
	m = resize(m, 200, 30)
	m = pressP(t, m)

	// From the category listing: the row carries title and viewers.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	if sel := m.SelectedChannel(); sel == nil || sel.Login != "gmhikaru" {
		t.Fatalf("selected = %+v, want gmhikaru", sel)
	}
	if calls := resolver.calls(); len(calls) != 0 {
		t.Fatalf("a listed channel was looked up again: %v", calls)
	}

	// From the search: live, with no stream data.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)
	calls := resolver.calls()
	if len(calls) != 1 || len(calls[0]) != 1 || calls[0][0] != "gaules" {
		t.Fatalf("lookups = %v, want one for gaules", calls)
	}
	sel := m.SelectedChannel()
	if sel == nil || sel.Viewers == nil || *sel.Viewers != 3681 || sel.CategoryName == nil || *sel.CategoryName != "Project Zomboid" {
		t.Fatalf("selected after the lookup = %+v", sel)
	}
}

func TestListHeader_NamesThePlatformAt80Columns(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		tw, kick := twitchCatalog(), kickCatalog()
		kick.category = append(kick.category, catalog.Category{ID: "8", Name: "Grand Theft Auto V (GTA) with a very long name", Viewers: catalog.IntPtr(153523)})
		m := twoPlatforms(t, lang, tw, kick, WithChat(&fakeChat{}))
		m = openFirst(t, resize(m, 80, 24))
		m = pressP(t, m)
		// Past the recent row of the channel just opened, down to the long name.
		for range 3 {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
		}
		m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = drainAll(t, m, cmd)
		if cat := m.SelectedCategory(); m.Mode() != ModeChannels || cat == nil || cat.ID != "8" {
			t.Fatalf("%s: mode %q category %+v", lang, m.Mode(), cat)
		}
		if listW, chatW := m.panelWidths(); listW == 0 || chatW == 0 {
			t.Fatalf("%s: 80 columns lost a panel: list %d chat %d", lang, listW, chatW)
		}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > 80 {
				t.Errorf("%s: row %d is %d cells wide: %q", lang, i, w, line)
			}
		}
		// The long category name is what gets cut, not the platform.
		if !strings.Contains(lines[0], "Kick · ") {
			t.Errorf("%s: header lost the platform: %q", lang, lines[0])
		}
		bar := ansi.Strip(m.StatusBar())
		assertStatusWidth(t, bar, 80)
	}
}
