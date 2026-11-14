package ui

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var orientationLangs = []string{i18n.LangPTBR, i18n.LangEN}

func mustCatalog(t *testing.T, lang string) *i18n.Catalog {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New(%s): %v", lang, err)
	}
	return cat
}

// newCategoriesModel loads two categories from a fake source whose Search
// records the query it receives.
func newCategoriesModel(t *testing.T, lang string, width int, searched *[]string) Model {
	t.Helper()
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "cat-cs", Name: "Counter-Strike", Viewers: catalog.IntPtr(250000)},
				{ID: "cat-jc", Name: "Just Chatting", Viewers: catalog.IntPtr(410000)},
			}}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch-alan", Login: "alanzoka", DisplayName: "alanzoka", Viewers: catalog.IntPtr(42000)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			*searched = append(*searched, query)
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules"},
			}}, nil
		},
	}
	m := New(mustCatalog(t, lang), WithSource(src))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
	m, _ = pressKeyMsg(m, m.Init()())
	if len(m.VisibleItems()) != 2 {
		t.Fatalf("categories not loaded: %d items", len(m.VisibleItems()))
	}
	return m
}

func pressKeyMsg(m Model, msg tea.Msg) (Model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func TestFilterWithoutMatchesSuggestsSearchAndCtrlFRunsIt(t *testing.T) {
	for _, lang := range orientationLangs {
		t.Run(lang, func(t *testing.T) {
			var searched []string
			m := newCategoriesModel(t, lang, 80, &searched)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			m = typeRunes(m, "gaules")

			hint := fmt.Sprintf(m.t("filter.search_hint"), "gaules")
			if !strings.Contains(hint, "Ctrl+F") || !strings.Contains(hint, "Enter") {
				t.Fatalf("filter.search_hint must cite Enter and Ctrl+F: %q", hint)
			}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, m.t("filter.empty")) || !strings.Contains(view, hint) {
				t.Fatalf("empty filter must suggest %q:\n%s", hint, view)
			}
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, hint) {
				t.Fatalf("status bar must lead with the search hint at 80 cols:\n%q", bar)
			}

			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			if m.Mode() != ModeSearch || m.SearchQuery() != "gaules" || cmd == nil {
				t.Fatalf("Ctrl+F must search the filter text: mode=%q query=%q cmd=%v", m.Mode(), m.SearchQuery(), cmd != nil)
			}
			m, _ = pressKeyMsg(m, cmd())
			if len(searched) != 1 || searched[0] != "gaules" {
				t.Fatalf("searched = %v, want [gaules]", searched)
			}
			if !strings.Contains(ansi.Strip(m.View()), "Gaules") {
				t.Fatalf("search results not shown:\n%s", ansi.Strip(m.View()))
			}
			if m.Filter() != "" || m.Filtering() {
				t.Fatalf("filter must be consumed by the search: %q", m.Filter())
			}
		})
	}
}

func TestFilterWithMatchesDoesNotSuggestSearch(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, i18n.LangPTBR, 80, &searched)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "counter")
	if strings.Contains(ansi.Strip(m.View()), "Ctrl+F busca na Twitch") {
		t.Fatalf("hint shown although the filter matches:\n%s", ansi.Strip(m.View()))
	}
}

// The help opens at the list keys, where Esc (one level back) and Ctrl+R (the
// categories from anywhere) sit on rows of their own, whole at 80 columns.
func TestHelpExplainsEscAndCtrlRWithoutTruncation(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, width := range []int{80, 120} {
			t.Run(fmt.Sprintf("%s/%d", lang, width), func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = mustCatalog(t, lang)
				m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyF1})
				view := ansi.Strip(m.View())
				title := "=== " + m.t("help.title") + " ==="
				if !strings.Contains(view, title) {
					t.Fatalf("help title truncated or missing:\n%s", view)
				}
				rows := helpBody(m)
				for _, want := range []struct{ keys, text string }{
					{"Esc", m.t("action.back")},
					{"Ctrl+R", m.t("action.categories")},
				} {
					if helpRowWith(rows, want.keys, want.text) == "" {
						t.Errorf("help row %s %q truncated or missing:\n%s", want.keys, want.text, view)
					}
				}
				for _, row := range strings.Split(ansi.Strip(strings.Join(m.renderHelpLines(width), "\n")), "\n") {
					if w := ansi.StringWidth(row); w > width {
						t.Errorf("help row wider than %d: %d %q", width, w, row)
					}
				}
			})
		}
	}
}

func TestEscReturnsToCategoriesFromChannelsAndSearch(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, i18n.LangPTBR, 120, &searched)

	// Into a category: Enter loads its channels.
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeChannels {
		t.Fatalf("mode = %q, want channels", m.Mode())
	}

	// A filter is one step: the first Esc only clears it (without leaving the category).
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "alan")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeChannels || m.Filter() != "" || m.Filtering() {
		t.Fatalf("first Esc: mode=%q filter=%q filtering=%v, want channels and no filter", m.Mode(), m.Filter(), m.Filtering())
	}
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("second Esc: mode=%q, want categories with a reload", m.Mode())
	}
	m, _ = pressKeyMsg(m, cmd())
	if m.HighlightedID() != "cat-cs" {
		t.Fatalf("highlight after Esc = %q, want the category just left", m.HighlightedID())
	}

	// Search started from categories: Esc returns to categories.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeSearch {
		t.Fatalf("mode = %q, want search", m.Mode())
	}
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("Esc from search (from categories): mode=%q, want categories with reload", m.Mode())
	}
	m, _ = pressKeyMsg(m, cmd())

	// On the categories themselves Esc does nothing.
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd != nil {
		t.Fatalf("Esc on categories: mode=%q cmd=%v", m.Mode(), cmd != nil)
	}
}

func TestEscFromSearchReturnsToOrigin(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, i18n.LangEN, 120, &searched)

	// Path 1: search from an open category → Esc reloads that category's channels.
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeChannels || m.SelectedCategory() == nil || m.SelectedCategory().ID != "cat-cs" {
		t.Fatalf("precondition channels: mode=%q cat=%+v", m.Mode(), m.SelectedCategory())
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeSearch || m.list.search.origin != ModeChannels {
		t.Fatalf("search from category: mode=%q origin=%q", m.Mode(), m.list.search.origin)
	}
	fromCategoryView := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE search from category:\n%s", fromCategoryView)

	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeChannels || cmd == nil {
		t.Fatalf("Esc from search (from category): mode=%q cmd=%v, want channels reload", m.Mode(), cmd != nil)
	}
	if m.SelectedCategory() == nil || m.SelectedCategory().ID != "cat-cs" {
		t.Fatalf("category lost after Esc: %+v", m.SelectedCategory())
	}
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeChannels {
		t.Fatalf("after reload: mode=%q, want channels", m.Mode())
	}
	if len(m.Channels()) != 1 || m.Channels()[0].Login != "alanzoka" {
		t.Fatalf("channels after Esc = %+v, want alanzoka from cat-cs", m.Channels())
	}
	backToChannelsView := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE Esc back to channels:\n%s", backToChannelsView)
	if !strings.Contains(backToChannelsView, "Counter-Strike") || !strings.Contains(backToChannelsView, "alanzoka") {
		t.Fatalf("view after Esc must show category channels:\n%s", backToChannelsView)
	}

	// Path 2: search from categories → Esc returns to categories.
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("Esc from channels: mode=%q, want categories", m.Mode())
	}
	m, _ = pressKeyMsg(m, cmd())

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeSearch || m.list.search.origin != ModeCategories {
		t.Fatalf("search from categories: mode=%q origin=%q", m.Mode(), m.list.search.origin)
	}
	fromCategoriesView := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE search from categories:\n%s", fromCategoriesView)

	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("Esc from search (from categories): mode=%q, want categories reload", m.Mode())
	}
	m, _ = pressKeyMsg(m, cmd())
	backToCategoriesView := ansi.Strip(m.View())
	t.Logf("PTY CAPTURE Esc back to categories:\n%s", backToCategoriesView)
	if !strings.Contains(backToCategoriesView, "Counter-Strike") || !strings.Contains(backToCategoriesView, "Just Chatting") {
		t.Fatalf("view after Esc must show categories:\n%s", backToCategoriesView)
	}

	// Ctrl+R from a search started in a category still jumps to categories.
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeSearch || m.list.search.origin != ModeChannels {
		t.Fatalf("precondition for Ctrl+R: mode=%q origin=%q", m.Mode(), m.list.search.origin)
	}
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("Ctrl+R from search: mode=%q, want categories", m.Mode())
	}
}

func TestSeparateWindowNoticeOnlyOnFirstPlayback(t *testing.T) {
	for _, lang := range orientationLangs {
		t.Run(lang, func(t *testing.T) {
			m, _ := newFailureModel(t, lang, 80, "gaules")
			notice := m.t("player.window_notice")
			if m.PlayerWindowNotice() || strings.Contains(ansi.Strip(m.StatusBar()), notice) {
				t.Fatal("notice must wait until the video actually plays")
			}
			m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
			bar := ansi.Strip(m.StatusBar())
			if !m.PlayerWindowNotice() || !strings.Contains(bar, notice) {
				t.Fatalf("first playback must show %q:\n%q", notice, bar)
			}
			if !strings.Contains(bar, m.t("status.quit_hint")) {
				t.Fatalf("quit hint lost with the notice:\n%q", bar)
			}
			// A later status update (volume) keeps it; a key press dismisses it.
			m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 90})
			if !m.PlayerWindowNotice() {
				t.Fatal("notice dropped by a status update")
			}
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			if m.PlayerWindowNotice() {
				t.Fatal("notice must go away on the next key")
			}
			want := fmt.Sprintf(m.t("player.playing"), "gaules")
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, want) {
				t.Fatalf("after the notice the bar must show %q:\n%q", want, bar)
			}

			// Second channel in the same session: no notice.
			m, cmd := pressKeyMsg(m, ChannelSelectedMsg{Channel: catalog.Channel{Login: "alanzoka", DisplayName: "alanzoka"}})
			m = drainCmd(t, m, cmd)
			m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "alanzoka", Volume: 100})
			if m.PlayerWindowNotice() || strings.Contains(ansi.Strip(m.StatusBar()), notice) {
				t.Fatal("notice must appear only on the first playback of the session")
			}
		})
	}
}

func TestStatusBarAt80KeepsMainActionAndQuit(t *testing.T) {
	const login = "um_canal_de_nome_bem_comprido"
	type phase struct {
		name  string
		apply func(m Model) Model
		main  func(m Model) string
	}
	phases := []phase{
		{"starting", func(m Model) Model {
			// The setup chose gaules with Enter; this selection comes from the next Enter.
			m.list.enters++
			m, cmd := pressKeyMsg(m, ChannelSelectedMsg{Channel: catalog.Channel{Login: login, DisplayName: login, Viewers: catalog.IntPtr(12900)},
				Enter: m.list.enters})
			return drainCmd(t, m, cmd)
		}, func(m Model) string { return fmt.Sprintf(m.t("player.starting_channel"), login) }},
		{"playing", func(m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown}) // dismiss the first-play notice
			return m
		}, func(m Model) string { return fmt.Sprintf(m.t("player.playing"), "gaules") }},
		{"break", func(m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStateBreak, Login: "gaules", Volume: 85, BreakElapsed: 145 * time.Second})
			return m
		}, func(m Model) string { return fmt.Sprintf(m.t("player.break"), 145) }},
		{"error", func(m Model) Model {
			return failWith(t, m, 1, PlayerFailureNetwork, "error: Unable to open URL: https://gql.twitch.tv/gql (Connection refused)")
		}, func(m Model) string { return m.t("player.fail.network") }},
		{"filter-no-match", func(m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			return typeRunes(m, "nenhum_canal_assim")
		}, func(m Model) string { return fmt.Sprintf(m.t("filter.search_hint"), "nenhum_canal_assim") }},
	}
	for _, lang := range orientationLangs {
		for _, p := range phases {
			t.Run(lang+"/"+p.name, func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = mustCatalog(t, lang)
				m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
				m = p.apply(m)
				bar := ansi.Strip(m.StatusBar())
				if w := ansi.StringWidth(bar); w != 80 {
					t.Fatalf("bar is %d cells, want 80", w)
				}
				if main := p.main(m); !strings.Contains(bar, main) {
					t.Fatalf("main message %q cut at 80 columns:\n%q", main, bar)
				}
				if !strings.Contains(bar, m.t("status.quit_hint")) {
					t.Fatalf("quit hint lost at 80 columns:\n%q", bar)
				}
				// A cut secondary keeps a readable chunk, never a stub like "vo…".
				if strings.HasSuffix(strings.TrimRight(bar, " "), "· vo…") {
					t.Fatalf("secondary cut to a stub:\n%q", bar)
				}
			})
		}
	}
}

func TestStatusBarAt120KeepsTitleAndContext(t *testing.T) {
	for _, lang := range orientationLangs {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			bar := ansi.Strip(m.StatusBar())
			for _, want := range []string{
				m.t("app.title") + " | [" + lang + "]",
				fmt.Sprintf(m.t("player.playing"), "gaules"),
				"Counter-Strike",
				m.t("status.focus_list"),
				m.t("status.help_hint"),
				m.t("status.quit_hint"),
			} {
				if !strings.Contains(bar, want) {
					t.Fatalf("bar at 120 lacks %q:\n%q", want, bar)
				}
			}
		})
	}
}
