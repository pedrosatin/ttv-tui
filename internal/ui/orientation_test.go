package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
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
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{Items: []twitch.Category{
				{ID: "cat-cs", Name: "Counter-Strike", Viewers: twitch.IntPtr(250000)},
				{ID: "cat-jc", Name: "Just Chatting", Viewers: twitch.IntPtr(410000)},
			}}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
				{ID: "ch-alan", Login: "alanzoka", DisplayName: "alanzoka", Viewers: twitch.IntPtr(42000)},
			}}, nil
		},
		searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
			*searched = append(*searched, query)
			return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
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

func TestFilterWithoutMatchesSuggestsSearchAndCtrlSRunsIt(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, leaveFilter := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/leaveFilter=%v", lang, leaveFilter), func(t *testing.T) {
				var searched []string
				m := newCategoriesModel(t, lang, 80, &searched)
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
				m = typeRunes(m, "gaules")
				if leaveFilter {
					// Enter on an empty list leaves the filter prompt but keeps the text.
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
					if m.Filtering() || m.Filter() != "gaules" {
						t.Fatalf("after Enter: filtering=%v filter=%q", m.Filtering(), m.Filter())
					}
				}

				hint := fmt.Sprintf(m.t("filter.search_hint"), "gaules")
				view := ansi.Strip(m.View())
				if !strings.Contains(view, m.t("filter.empty")) || !strings.Contains(view, hint) {
					t.Fatalf("empty filter must suggest %q:\n%s", hint, view)
				}
				if !leaveFilter {
					if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, hint) {
						t.Fatalf("status bar must lead with the search hint at 80 cols:\n%q", bar)
					}
				}

				m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
				if m.Mode() != ModeSearch || m.SearchQuery() != "gaules" || cmd == nil {
					t.Fatalf("Ctrl+S must search the filter text: mode=%q query=%q cmd=%v", m.Mode(), m.SearchQuery(), cmd != nil)
				}
				m, _ = pressKeyMsg(m, cmd())
				if len(searched) != 1 || searched[0] != "gaules" {
					t.Fatalf("searched = %v, want [gaules]", searched)
				}
				if !strings.Contains(ansi.Strip(m.View()), "Gaules (gaules)") {
					t.Fatalf("search results not shown:\n%s", ansi.Strip(m.View()))
				}
				if m.Filter() != "" || m.Filtering() {
					t.Fatalf("filter must be consumed by the search: %q", m.Filter())
				}
			})
		}
	}
}

func TestFilterWithMatchesDoesNotSuggestSearch(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, i18n.LangPTBR, 80, &searched)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "counter")
	if strings.Contains(ansi.Strip(m.View()), "Ctrl+S busca na Twitch") {
		t.Fatalf("hint shown although the filter matches:\n%s", ansi.Strip(m.View()))
	}
}

func TestHelpExplainsEscAndCtrlRWithoutTruncation(t *testing.T) {
	keys := []string{"help.nav", "help.esc", "help.categories", "help.search", "help.player", "help.chat", "help.app"}
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
				focusNow := fmt.Sprintf(m.t("help.focus_now"), m.t("help.panel_list"))
				if w := ansi.StringWidth("  " + focusNow); w > 80 {
					t.Errorf("help.focus_now is %d cells, must fit 80 columns", w)
				}
				if !strings.Contains(view, focusNow) {
					t.Errorf("help focus line missing: %q\n%s", focusNow, view)
				}
				for _, k := range keys {
					line := m.t(k)
					if w := ansi.StringWidth("  " + line); w > 80 {
						t.Errorf("%s is %d cells, must fit 80 columns", k, w)
					}
					if !strings.Contains(view, line) {
						t.Errorf("help line %s truncated or missing: %q\n%s", k, line, view)
					}
				}
				esc, cats := m.t("help.esc"), m.t("help.categories")
				if !strings.Contains(esc, "Esc") || !strings.Contains(cats, "Esc") || !strings.Contains(cats, "Ctrl+R") {
					t.Fatalf("help must tell Esc from Ctrl+R: %q / %q", esc, cats)
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

	// A filter is one step: the first Esc only clears it.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "alan")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeChannels || m.Filter() != "" {
		t.Fatalf("first Esc: mode=%q filter=%q, want channels and no filter", m.Mode(), m.Filter())
	}
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd == nil {
		t.Fatalf("second Esc: mode=%q, want categories with a reload", m.Mode())
	}
	m, _ = pressKeyMsg(m, cmd())
	if m.HighlightedID() != "cat-cs" {
		t.Fatalf("highlight after Esc = %q, want the category just left", m.HighlightedID())
	}

	// From search results.
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeRunes(m, "gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pressKeyMsg(m, cmd())
	if m.Mode() != ModeSearch {
		t.Fatalf("mode = %q, want search", m.Mode())
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories {
		t.Fatalf("Esc from search: mode=%q, want categories", m.Mode())
	}

	// On the categories themselves Esc does nothing.
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Mode() != ModeCategories || cmd != nil {
		t.Fatalf("Esc on categories: mode=%q cmd=%v", m.Mode(), cmd != nil)
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
			m, cmd := pressKeyMsg(m, ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka", DisplayName: "alanzoka"}})
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
			m, cmd := pressKeyMsg(m, ChannelSelectedMsg{Channel: twitch.Channel{Login: login, DisplayName: login, Viewers: twitch.IntPtr(12900)}})
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
