package ui

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// expectKey is one catalog key (with its format arguments) that a rendered
// surface must show, resolved in the language under test.
type expectKey struct {
	key  string
	args []any
}

func ek(key string, args ...any) expectKey { return expectKey{key: key, args: args} }

// rawCatalogKey matches a catalog key that leaked to the screen instead of its
// text (what Model.t returns when a key is missing from every catalog).
var rawCatalogKey = regexp.MustCompile(`\b(?:status|player|help|layout|chat|channels|categories|error|channel_filter|search|filter|auth|app|section|favorites|focus|store|action|key)\.[a-z0-9_]+(?:\.[a-z0-9_]+)*\b`)

func auditCatalog(t *testing.T, lang string) *i18n.Catalog {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New(%s): %v", lang, err)
	}
	return cat
}

func auditModel(t *testing.T, lang string, width int) Model {
	t.Helper()
	m := New(auditCatalog(t, lang))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	return updated.(Model)
}

func auditPlaying(t *testing.T, lang string, width int) Model {
	t.Helper()
	m := playingModel(t, lang, &fakePlayback{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	return updated.(Model)
}

// auditChannelOpen has a channel open, which is what gives the layouts a chat
// to arrange; before that the layout keys only explain why they do nothing.
func auditChannelOpen(t *testing.T, lang string, width int) Model {
	t.Helper()
	m := New(auditCatalog(t, lang), WithPlayback(&fakePlayback{}), WithChat(&fakeChat{}))
	m = auditSend(m, tea.WindowSizeMsg{Width: width, Height: 30})
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{Login: "gaules"}})
	m = updated.(Model)
	if cmd != nil {
		m = auditSend(m, cmd())
	}
	return m
}

func auditSend(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// assertResolved checks that every expected key has its own text in lang (no
// fallback to English, no empty value), that text is on screen, and that no raw
// key leaked.
func assertResolved(t *testing.T, cat *i18n.Catalog, surface, screen string, want []expectKey) {
	t.Helper()
	if strings.TrimSpace(screen) == "" {
		t.Fatalf("%s rendered empty", surface)
	}
	for _, w := range want {
		if !cat.Has(w.key) {
			t.Errorf("%s: key %q has no %s text (would fall back to English)", surface, w.key, cat.Lang())
			continue
		}
		text := CatalogText(cat, w.key)
		if len(w.args) > 0 {
			text = fmt.Sprintf(text, w.args...)
		}
		if text == "" {
			t.Errorf("%s: key %q resolved to empty text", surface, w.key)
			continue
		}
		if !strings.Contains(screen, text) {
			t.Errorf("%s (%s): missing %q (key %s) in:\n%s", surface, cat.Lang(), text, w.key, screen)
		}
	}
	if leaked := rawCatalogKey.FindAllString(screen, -1); len(leaked) > 0 {
		t.Errorf("%s (%s): raw catalog key on screen: %v\n%s", surface, cat.Lang(), leaked, screen)
	}
}

// TestStatusBarResolvesSameKeysInBothLanguages renders the status bar in the
// states the operator sees, in en and pt-BR, and requires the same keys to be
// resolved in the language itself.
func TestStatusBarResolvesSameKeysInBothLanguages(t *testing.T) {
	type scenario struct {
		name  string
		build func(t *testing.T, lang string) Model
		want  func() []expectKey
	}
	layoutScenario := func(name string, presses int, mode string) scenario {
		return scenario{
			name: name,
			build: func(t *testing.T, lang string) Model {
				m := auditChannelOpen(t, lang, 200)
				for i := 0; i < presses; i++ {
					m = auditSend(m, tea.KeyMsg{Type: tea.KeyCtrlW})
				}
				return m
			},
			want: func() []expectKey {
				return []expectKey{ek("status.layout", "§"+mode)}
			},
		}
	}
	scenarios := []scenario{
		{"empty", func(t *testing.T, lang string) Model { return auditModel(t, lang, 200) }, func() []expectKey {
			return []expectKey{ek("list.empty_action"), ek("app.title"), ek("status.help_hint"), ek("status.quit_hint")}
		}},
		{"layout_without_chat", func(t *testing.T, lang string) Model {
			return auditSend(auditModel(t, lang, 200), tea.KeyMsg{Type: tea.KeyCtrlW})
		}, func() []expectKey {
			return []expectKey{ek("layout.no_chat")}
		}},
		layoutScenario("layout chat", 1, "chat"),
		layoutScenario("layout list", 2, "list"),
		layoutScenario("layout focus", 3, "focus"),
		{"window notice and volume", func(t *testing.T, lang string) Model {
			return auditSend(auditPlaying(t, lang, 200), PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80})
		}, func() []expectKey {
			return []expectKey{ek("player.window_notice"), ek("player.vol", 80), ek("player.playing", "gaules")}
		}},
		{"paused", func(t *testing.T, lang string) Model {
			m := auditSend(auditPlaying(t, lang, 200), PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80})
			return auditSend(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 80})
		}, func() []expectKey {
			return []expectKey{ek("player.paused", "gaules"), ek("player.vol", 80)}
		}},
		{"commercial break", func(t *testing.T, lang string) Model {
			m := auditSend(auditPlaying(t, lang, 200), PlayerStatusMsg{Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 80})
			return auditSend(m, PlayerStatusMsg{Generation: 1, State: PlayerStateBreak, Login: "gaules", Volume: 80, BreakElapsed: 0})
		}, func() []expectKey {
			return []expectKey{ek("player.break", 0), ek("player.vol", 80)}
		}},
		{"mpv keys warning", func(t *testing.T, lang string) Model {
			return auditSend(auditPlaying(t, lang, 200), PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, InputConfFailed: true})
		}, func() []expectKey {
			return []expectKey{ek("player.warn.input_conf")}
		}},
		{"channel filter menu", func(t *testing.T, lang string) Model {
			m := auditModel(t, lang, 200)
			m.list.prefs.menuOpen = true
			return m
		}, func() []expectKey { return []expectKey{ek("channel_filter.status_open")} }},
		{"search prompt", func(t *testing.T, lang string) Model {
			m := auditModel(t, lang, 200)
			m.list.search.active, m.list.search.input = true, "doom"
			return m
		}, func() []expectKey { return []expectKey{ek("search.prompt", "doom")} }},
		{"filter prompt", func(t *testing.T, lang string) Model {
			m := auditModel(t, lang, 200)
			m.list.filtering, m.list.filter = true, "cs"
			return m
		}, func() []expectKey { return []expectKey{ek("filter.prompt", "cs")} }},
		{"Ctrl+C hint", func(t *testing.T, lang string) Model {
			return auditSend(auditModel(t, lang, 200), tea.KeyMsg{Type: tea.KeyCtrlC})
		}, func() []expectKey { return []expectKey{ek("status.ctrl_c_hint")} }},
	}
	for _, failure := range []PlayerFailure{
		PlayerFailureOffline, PlayerFailureNetwork, PlayerFailureTimeout,
		PlayerFailureStreamlinkMissing, PlayerFailureMPVMissing, PlayerFailureUnknown,
	} {
		scenarios = append(scenarios, scenario{
			name: "player failure " + string(failure),
			build: func(t *testing.T, lang string) Model {
				return auditSend(auditPlaying(t, lang, 200), PlayerStatusMsg{
					Generation: 1, State: PlayerStateError, Login: "gaules", Failure: failure, LastLine: "raw tool line",
				})
			},
			want: func() []expectKey {
				return []expectKey{ek("player.fail." + string(failure)), ek("player.fail.detail", "raw tool line")}
			},
		})
	}

	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := auditCatalog(t, lang)
		for _, sc := range scenarios {
			t.Run(lang+"/"+sc.name, func(t *testing.T) {
				m := sc.build(t, lang)
				want := sc.want()
				for i, w := range want {
					// Layout scenarios name the mode by its identifier ("§mode");
					// the screen must show that mode's catalog label.
					if len(w.args) == 1 {
						if s, ok := w.args[0].(string); ok && strings.HasPrefix(s, "§") {
							mode := strings.TrimPrefix(s, "§")
							want[i] = ek(w.key, cat.T("layout.mode."+mode))
							want = append(want, ek("layout.mode."+mode))
						}
					}
				}
				assertResolved(t, cat, "status bar", ansi.Strip(m.StatusBar()), want)
			})
		}
	}
}

// TestHelpResolvesSameKeysInBothLanguages renders the help overlay in both
// languages and ties its layout line to the labels the status bar uses.
func TestHelpResolvesSameKeysInBothLanguages(t *testing.T) {
	helpKeys := []string{
		"help.title", "help.close", "help.nav", "help.esc", "help.categories", "help.search",
		"help.channel_filter", "help.layout", "help.player", "help.chat", "help.app",
		"help.nocolor", "help.colors",
	}
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			cat := auditCatalog(t, lang)
			m := auditModel(t, lang, 200)
			m = auditSend(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
			if !m.HelpVisible() {
				t.Fatal("? did not open the help")
			}
			screen := ansi.Strip(strings.Join(m.renderHelpLines(400), "\n"))

			want := []expectKey{ek("help.focus_now", cat.T("help.panel_list")), ek("help.panel_list")}
			for _, k := range helpKeys {
				want = append(want, ek(k))
			}
			assertResolved(t, cat, "help", screen, want)

			// The help line and the status bar name the layout modes the same way.
			for _, mode := range []string{"split", "chat", "list", "focus"} {
				label := cat.T("layout.mode." + mode)
				if !cat.Has("layout.mode." + mode) {
					t.Errorf("layout.mode.%s has no %s text", mode, lang)
				}
				if !strings.Contains(cat.T("help.layout"), label) {
					t.Errorf("help.layout (%s) does not name mode %q as %q: %s", lang, mode, label, cat.T("help.layout"))
				}
			}
		})
	}
}

// TestPTBRLayoutTokenIsPortuguese pins the operator-visible fix: the layout
// token on the pt-BR bar is a pt-BR word, and en keeps its own.
func TestPTBRLayoutTokenIsPortuguese(t *testing.T) {
	cases := []struct {
		lang    string
		presses int
		want    string
	}{
		{i18n.LangPTBR, 1, "layout: chat"},
		{i18n.LangPTBR, 2, "layout: lista"},
		{i18n.LangPTBR, 3, "layout: foco"},
		{i18n.LangEN, 1, "layout: chat"},
		{i18n.LangEN, 2, "layout: list"},
		{i18n.LangEN, 3, "layout: focus"},
	}
	for _, tc := range cases {
		m := auditChannelOpen(t, tc.lang, 120)
		for i := 0; i < tc.presses; i++ {
			m = auditSend(m, tea.KeyMsg{Type: tea.KeyCtrlW})
		}
		if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, tc.want) {
			t.Errorf("%s: status bar %q missing %q", tc.lang, got, tc.want)
		}
	}
}

// At 80 columns the pt-BR layout label must not push the volume out, and the
// volume must not push the layout label out either: the app title yields first.
func TestPTBRLayoutTokenAndVolumeFitAt80Columns(t *testing.T) {
	for presses, label := range map[int]string{1: "chat", 2: "lista", 3: "foco"} {
		m := auditChannelOpen(t, i18n.LangPTBR, 80)
		m = auditSend(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 55})
		for i := 0; i < presses; i++ {
			m = auditSend(m, tea.KeyMsg{Type: tea.KeyCtrlW})
		}
		got := ansi.Strip(m.StatusBar())
		if !strings.Contains(got, "vol 55") {
			t.Errorf("layout %s: volume gone from the 80-column bar: %q", label, got)
		}
		if want := "layout: " + label; !strings.Contains(got, want) {
			t.Errorf("layout %s: %q gone from the 80-column bar: %q", label, want, got)
		}
		if w := ansi.StringWidth(got); w > 80 {
			t.Errorf("layout %s: bar is %d columns: %q", label, w, got)
		}
	}
}
