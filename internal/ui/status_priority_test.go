package ui

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

const (
	glyphPlaying = "▶"
	glyphPaused  = "‖"
	glyphError   = "✖"
	glyphWarning = "!"
)

// barCell is one visible cell of a rendered line and the SGR state it got.
type barCell struct {
	text    string
	reverse bool
	colored bool
}

var sgrSeqRe = regexp.MustCompile(`^\x1b\[([0-9;]*)m`)

// barCells replays the SGR stream the way a terminal does, so a reset that
// drops the reverse shows up on the cells after it.
func barCells(t *testing.T, line string) []barCell {
	t.Helper()
	var cells []barCell
	reverse, colored := false, false
	for len(line) > 0 {
		if seq := sgrSeqRe.FindStringSubmatch(line); seq != nil {
			line = line[len(seq[0]):]
			for _, p := range strings.Split(seq[1], ";") {
				n, err := strconv.Atoi(p)
				switch {
				case p == "", err == nil && n == 0:
					reverse, colored = false, false
				case n == 7:
					reverse = true
				case n == 27:
					reverse = false
				case n == 39:
					colored = false
				case (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
					colored = true
				}
			}
			continue
		}
		r := []rune(line)[0]
		line = line[len(string(r)):]
		for range ansi.StringWidth(string(r)) {
			cells = append(cells, barCell{text: string(r), reverse: reverse, colored: colored})
		}
	}
	return cells
}

// barState is one lead the status bar can show, applied to a running model.
type barState struct {
	name  string
	apply func(t *testing.T, m Model) Model
	glyph string
}

func playingState(state PlayerState, login string) func(t *testing.T, m Model) Model {
	return func(_ *testing.T, m Model) Model {
		m.playerWindowNotice = false
		m.playerState = state
		m.playerLogin = login
		m.playerVolume = 100
		m.selectedChannel = &twitch.Channel{Login: login, DisplayName: login, Viewers: twitch.IntPtr(1234)}
		return m
	}
}

func barStates() []barState {
	return []barState{
		{"playing", playingState(PlayerStatePlaying, "gaules"), glyphPlaying},
		{"paused", playingState(PlayerStatePaused, "zackrawrr"), glyphPaused},
		{"starting", playingState(PlayerStateStarting, "gaules"), ""},
		{"break", playingState(PlayerStateBreak, "gaules"), ""},
		{"player-error", func(t *testing.T, m Model) Model {
			return failWith(t, m, 1, PlayerFailureOffline, offlineLine)
		}, glyphError},
		{"list-error", func(_ *testing.T, m Model) Model {
			m.err = errors.New("boom")
			return m
		}, glyphError},
		{"auth-warning", func(_ *testing.T, m Model) Model {
			m.authNotice = m.t("auth.token_invalid_notice")
			return m
		}, glyphWarning},
		{"copy-failed", func(_ *testing.T, m Model) Model {
			m.copyNotice = m.t("chat.copy.failed")
			return m
		}, glyphWarning},
		{"ended", playingState(PlayerStateEnded, "gaules"), ""},
	}
}

func statusTestModel(t *testing.T, lang string, width int) Model {
	t.Helper()
	m, _, _ := setupTwoPanelModel(t)
	m.catalog = mustCatalog(t, lang)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
	m.playerWindowNotice = false
	return m
}

func TestStatusBarKeepsReverseToEndOfLine(t *testing.T) {
	defer forceOrientationColorsForTest(false)
	for _, colors := range []bool{true, false} {
		forceOrientationColorsForTest(colors)
		for _, width := range []int{80, 120} {
			for _, st := range barStates() {
				t.Run(fmt.Sprintf("colors=%v/%d/%s", colors, width, st.name), func(t *testing.T) {
					m := st.apply(t, statusTestModel(t, i18n.LangPTBR, width))
					rows := strings.Split(m.View(), "\n")
					for name, line := range map[string]string{"StatusBar": m.StatusBar(), "View": rows[len(rows)-1]} {
						cells := barCells(t, line)
						if len(cells) != width {
							t.Fatalf("%s: %d cells, want %d:\n%q", name, len(cells), width, line)
						}
						for i, c := range cells {
							if !c.reverse {
								t.Fatalf("%s: cell %d (%q) lost the reverse:\n%q", name, i, c.text, line)
							}
						}
					}
				})
			}
		}
	}
}

func TestStatusBarLongLoginKeepsVolumeAndFilterAt80(t *testing.T) {
	for _, lang := range orientationLangs {
		// 18 is the case from the review; 25 is the longest login Twitch allows.
		for _, login := range []string{"zackrawrr_official", "a_login_of_25_characters_"} {
			for _, state := range []PlayerState{PlayerStatePlaying, PlayerStatePaused} {
				t.Run(fmt.Sprintf("%s/%d/%s", lang, len(login), state), func(t *testing.T) {
					m := playingState(state, login)(t, statusTestModel(t, lang, 80))
					m.filterLanguages = []string{"pt"}
					m.filterMinViewers = 500
					bar := ansi.Strip(m.StatusBar())
					assertStatusWidth(t, bar, 80)
					main := fmt.Sprintf(m.t("player.playing"), login)
					if state == PlayerStatePaused {
						main = fmt.Sprintf(m.t("player.paused"), login)
					}
					for _, want := range []string{main, m.playerVolumeText(), "PT · 500+", m.t("status.quit_hint")} {
						if !strings.Contains(bar, want) {
							t.Fatalf("bar lost %q at 80 columns:\n%q", want, bar)
						}
					}
					if strings.Contains(bar, m.t("app.title")) {
						t.Fatalf("title kept while the state is short of room:\n%q", bar)
					}
				})
			}
		}
	}
}

func TestStatusBarCutOrderAtEveryWidth(t *testing.T) {
	login := "zackrawrr_official"
	for _, lang := range orientationLangs {
		for width := 40; width <= 160; width++ {
			m := playingState(PlayerStatePlaying, login)(t, statusTestModel(t, lang, width))
			m.filterLanguages = []string{"pt"}
			m.filterMinViewers = 500
			m.showList = false // layout: chat, so the layout token is in play.
			if m.LayoutMode() != LayoutChat {
				t.Fatalf("layout = %q", m.LayoutMode())
			}
			bar := ansi.Strip(m.StatusBar())
			title := strings.Contains(bar, m.t("app.title"))
			tag := strings.Contains(bar, "["+lang+"]")
			token := strings.Contains(bar, m.layoutStatusText())
			volume := strings.Contains(bar, m.playerVolumeText())
			filter := strings.Contains(bar, "PT · 500+")
			switch {
			case title && !tag:
				t.Fatalf("%d cols: language gone before the title:\n%q", width, bar)
			case tag && !token:
				t.Fatalf("%d cols: layout token gone before the language:\n%q", width, bar)
			case token && (!volume || !filter):
				t.Fatalf("%d cols: volume or filter gone before the layout token:\n%q", width, bar)
			case filter && !volume:
				t.Fatalf("%d cols: volume gone before the filter:\n%q", width, bar)
			}
		}
	}
}

// The title is the least useful text in the bar: the main context outlasts it.
func TestStatusBarTitleYieldsBeforeContext(t *testing.T) {
	category := "Chess"
	freed := false
	for width := 80; width <= 160; width++ {
		m := playingState(PlayerStatePlaying, "gaules")(t, statusTestModel(t, i18n.LangPTBR, width))
		m.selectedChannel.CategoryName = &category
		m.focus = FocusList
		bar := ansi.Strip(m.StatusBar())
		title, context := strings.Contains(bar, m.t("app.title")), strings.Contains(bar, category)
		if title && !context {
			t.Fatalf("%d cols: title kept at the cost of context:\n%q", width, bar)
		}
		freed = freed || (context && !title)
	}
	if !freed {
		t.Fatal("no width where dropping the title made room for the context")
	}
}

func TestStatusBarHintsStayAnchoredToTheRight(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, width := range []int{80, 120} {
			for _, st := range barStates() {
				t.Run(fmt.Sprintf("%s/%d/%s", lang, width, st.name), func(t *testing.T) {
					m := st.apply(t, statusTestModel(t, lang, width))
					bar := ansi.Strip(m.StatusBar())
					hint := m.t("status.quit_hint") + " "
					if !strings.HasSuffix(bar, hint) {
						t.Fatalf("quit hint left the right edge:\n%q", bar)
					}
				})
			}
		}
	}
}

func TestStatusGlyphsReadWithoutColor(t *testing.T) {
	forceOrientationColorsForTest(false)
	defer forceOrientationColorsForTest(false)
	for _, lang := range orientationLangs {
		for _, width := range []int{80, 120} {
			for _, st := range barStates() {
				t.Run(fmt.Sprintf("%s/%d/%s", lang, width, st.name), func(t *testing.T) {
					m := st.apply(t, statusTestModel(t, lang, width))
					raw := m.StatusBar()
					if sgrColorRe.MatchString(raw) {
						t.Fatalf("color off still paints:\n%q", raw)
					}
					bar := ansi.Strip(raw)
					for _, g := range []string{glyphPlaying, glyphPaused, glyphError, glyphWarning + " "} {
						want := strings.TrimSpace(g) == st.glyph
						if got := strings.Contains(bar, g); got != want {
							t.Fatalf("glyph %q present=%v, want %v:\n%q", g, got, want, bar)
						}
					}
				})
			}
		}
	}
}

func TestStatusGlyphLeadsTheMessage(t *testing.T) {
	forceOrientationColorsForTest(false)
	cases := []struct {
		name  string
		apply func(t *testing.T, m Model) Model
		want  func(m Model) string
	}{
		{"playing", playingState(PlayerStatePlaying, "gaules"), func(m Model) string {
			return glyphPlaying + " " + fmt.Sprintf(m.t("player.playing"), "gaules")
		}},
		{"paused", playingState(PlayerStatePaused, "gaules"), func(m Model) string {
			return glyphPaused + " " + fmt.Sprintf(m.t("player.paused"), "gaules")
		}},
		{"error", func(t *testing.T, m Model) Model {
			return failWith(t, m, 1, PlayerFailureNetwork, "")
		}, func(m Model) string { return glyphError + " " + m.t("player.fail.network") }},
		{"warning", func(_ *testing.T, m Model) Model {
			m.corruptNotice = "store.corrupt"
			return m
		}, func(Model) string { return glyphWarning + " store.corrupt" }},
	}
	for _, lang := range orientationLangs {
		for _, tc := range cases {
			t.Run(lang+"/"+tc.name, func(t *testing.T) {
				m := tc.apply(t, statusTestModel(t, lang, 120))
				if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, tc.want(m)) {
					t.Fatalf("bar lacks %q:\n%q", tc.want(m), bar)
				}
			})
		}
	}
}

// The glyph replaces the color instead of adding to it: only its cell is
// painted, so the text keeps the plain bar contrast.
func TestStatusColorStaysOnTheGlyph(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)
	for _, st := range barStates() {
		if st.glyph == "" {
			continue
		}
		t.Run(st.name, func(t *testing.T) {
			m := st.apply(t, statusTestModel(t, i18n.LangPTBR, 120))
			var painted []string
			for _, c := range barCells(t, m.StatusBar()) {
				if c.colored {
					painted = append(painted, c.text)
				}
			}
			if got := strings.Join(painted, ""); got != st.glyph {
				t.Fatalf("painted %q, want only the glyph %q:\n%q", got, st.glyph, m.StatusBar())
			}
		})
	}
}

func TestInformativeNoticeHasNoWarningGlyph(t *testing.T) {
	m := statusTestModel(t, i18n.LangPTBR, 120)
	m.statusNotice = m.t("favorites.added")
	if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, glyphWarning+" "+m.statusNotice) {
		t.Fatalf("a confirmation is not a warning:\n%q", bar)
	}
}

func TestWarningNoticesCarryTheGlyph(t *testing.T) {
	const width = 70 // Below the split limit, where the chat keys warn.
	m := statusTestModel(t, i18n.LangPTBR, width)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	want := glyphWarning + " " + m.t("chat.hidden_narrow_key")
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, want) {
		t.Fatalf("bar lacks %q:\n%q", want, bar)
	}
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, InputConfFailed: true})
	want = glyphWarning + " " + m.t("player.warn.input_conf")
	m.width = 120
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, want) {
		t.Fatalf("bar lacks %q:\n%q", want, bar)
	}
}

func volumeModel(t *testing.T, lang string, width, volume int) (Model, *fakePlayback) {
	t.Helper()
	m, pb := newFailureModel(t, lang, width, "gaules")
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: volume})
	m.playerWindowNotice = false
	return m, pb
}

func TestVolumeLimitsAnswerTheKey(t *testing.T) {
	cases := []struct {
		name   string
		key    rune
		volume int
		notice string
		delta  int
	}{
		{"max-plus", '+', 100, "player.vol_max", 5},
		{"max-equals", '=', 100, "player.vol_max", 5},
		{"min-minus", '-', 0, "player.vol_min", -5},
		{"min-underscore", '_', 0, "player.vol_min", -5},
	}
	for _, lang := range orientationLangs {
		for _, width := range []int{80, 120} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/%d/%s", lang, width, tc.name), func(t *testing.T) {
					m, pb := volumeModel(t, lang, width, tc.volume)
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
					bar := ansi.Strip(m.StatusBar())
					assertStatusWidth(t, bar, width)
					want := glyphWarning + " " + m.t(tc.notice)
					if !strings.Contains(bar, want) {
						t.Fatalf("bar lacks %q:\n%q", want, bar)
					}
					if vol := fmt.Sprintf(m.t("player.vol"), tc.volume); !strings.Contains(bar, vol) {
						t.Fatalf("limit notice hid the volume %q:\n%q", vol, bar)
					}
					// The UI volume can lag an update in flight, so the player still decides.
					if len(pb.volDeltas) != 1 || pb.volDeltas[0] != tc.delta {
						t.Fatalf("volDeltas = %v, want [%d]", pb.volDeltas, tc.delta)
					}
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
					if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, m.t(tc.notice)) {
						t.Fatalf("notice must go away on the next key:\n%q", bar)
					}
				})
			}
		}
	}
}

func TestVolumeBelowTheLimitStaysQuiet(t *testing.T) {
	for _, tc := range []struct {
		key    rune
		volume int
	}{{'+', 95}, {'-', 5}, {'-', 100}, {'+', 0}} {
		m, _ := volumeModel(t, i18n.LangEN, 120, tc.volume)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		if m.StatusNotice() != "" {
			t.Fatalf("%c at %d warned: %q", tc.key, tc.volume, m.StatusNotice())
		}
	}
}

func TestVolumeNoticeLeavesWhenThePlayerMoves(t *testing.T) {
	updates := map[string]PlayerStatusMsg{
		"volume": {Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80},
		"state":  {Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 100},
	}
	for name, msg := range updates {
		t.Run(name, func(t *testing.T) {
			m, _ := volumeModel(t, i18n.LangPTBR, 80, 100)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
			notice := m.t("player.vol_max")
			// The same status again changes nothing, so the notice stays.
			m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
			if !strings.Contains(ansi.Strip(m.StatusBar()), notice) {
				t.Fatalf("an unchanged status dropped the notice:\n%q", ansi.Strip(m.StatusBar()))
			}
			m, _ = pressKeyMsg(m, msg)
			if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, notice) {
				t.Fatalf("stale limit notice over a change made in the mpv window:\n%q", bar)
			}
		})
	}
}

func TestVolumeLimitDoesNotOutrankPlayerFailure(t *testing.T) {
	m, _ := volumeModel(t, i18n.LangPTBR, 80, 100)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = failWith(t, m, 1, PlayerFailureNetwork, "error: Unable to open URL")
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, glyphError+" "+m.t("player.fail.network")) {
		t.Fatalf("player failure must lead:\n%q", bar)
	}
	if strings.Contains(bar, m.t("player.vol_max")) {
		t.Fatalf("limit notice survived the failure:\n%q", bar)
	}
}

func TestVolumeKeysWithoutPlayerStaySilent(t *testing.T) {
	cat := mustCatalog(t, i18n.LangPTBR)
	m := New(cat, WithPlayback(&fakePlayback{}))
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if m.StatusNotice() != "" {
		t.Fatalf("+ with no player warned: %q", m.StatusNotice())
	}
}

func TestPlayerFailureStillLeadsWithFilterAndNotices(t *testing.T) {
	failures := []PlayerFailure{
		PlayerFailureOffline, PlayerFailureNetwork, PlayerFailureTimeout,
		PlayerFailureStreamlinkMissing, PlayerFailureMPVMissing, PlayerFailureUnknown,
	}
	for _, lang := range orientationLangs {
		for _, f := range failures {
			t.Run(lang+"/"+string(f), func(t *testing.T) {
				m := statusTestModel(t, lang, 80)
				m.filterLanguages = []string{"pt"}
				m.filterMinViewers = 500
				m = failWith(t, m, 1, f, offlineLine)
				m.ctrlCNotice = m.t("status.ctrl_c_hint")
				m.statusNotice = "secondary notice"
				bar := ansi.Strip(m.StatusBar())
				assertStatusWidth(t, bar, 80)
				lead := glyphError + " " + m.playerFailureText()
				for _, want := range []string{lead, m.t("status.quit_hint")} {
					if !strings.Contains(bar, want) {
						t.Fatalf("bar lacks %q:\n%q", want, bar)
					}
				}
				// The filter rides along whenever the failure leaves it the room.
				frame := " " + lead + " · PT · 500+ | " + m.t("status.quit_hint") + " "
				if ansi.StringWidth(frame) <= 80 && !strings.Contains(bar, "PT · 500+") {
					t.Fatalf("filter lost with room to spare:\n%q", bar)
				}
			})
		}
	}
}

func TestListErrorDetailOutranksVolume(t *testing.T) {
	m := playingState(PlayerStatePlaying, "gaules")(t, statusTestModel(t, i18n.LangPTBR, 120))
	m.err = errors.New("boom")
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, glyphError+" "+m.listErrorAction()) || !strings.Contains(bar, "boom") {
		t.Fatalf("list error lost its action or detail:\n%q", bar)
	}
	if strings.Contains(bar, m.playerVolumeText()) {
		t.Fatalf("volume took the columns of the error detail:\n%q", bar)
	}
}

func TestWindowNoticeKeepsVolumeAt80(t *testing.T) {
	for _, lang := range orientationLangs {
		m := statusTestModel(t, lang, 80)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 85})
		m.playerWindowNotice = true
		bar := ansi.Strip(m.StatusBar())
		for _, want := range []string{m.t("player.window_notice"), m.playerVolumeText(), m.t("status.quit_hint")} {
			if !strings.Contains(bar, want) {
				t.Fatalf("%s: bar lacks %q:\n%q", lang, want, bar)
			}
		}
	}
}

func TestStatusBarNeverExceedsWidth(t *testing.T) {
	states := append(barStates(),
		barState{name: "wide-name", apply: playingState(PlayerStatePlaying, "配信者のチャンネル名前")},
		barState{name: "long-filter", apply: func(t *testing.T, m Model) Model {
			m = playingState(PlayerStatePaused, "a_login_of_25_characters_")(t, m)
			m.filterLanguages = []string{"pt", "en", "es", "fr", "de", "it", "ja"}
			m.filterMinViewers = 10000
			return m
		}},
		barState{name: "filter-menu", apply: func(_ *testing.T, m Model) Model {
			m.filterLanguages = []string{"pt"}
			return m.openChannelFilterMenu()
		}},
		barState{name: "search-prompt", apply: func(_ *testing.T, m Model) Model {
			m.searching = true
			m.searchInput = strings.Repeat("busca muito longa ", 12)
			return m
		}},
		barState{name: "volume-max", apply: func(t *testing.T, m Model) Model {
			m = playingState(PlayerStatePlaying, "a_login_of_25_characters_")(t, m)
			m.statusNotice = m.t("player.vol_max")
			return m
		}},
		barState{name: "player-error-detail", apply: func(t *testing.T, m Model) Model {
			return failWith(t, m, 1, PlayerFailureUnknown, strings.Repeat("streamlink exited with code 1 ", 12))
		}},
	)
	for _, lang := range orientationLangs {
		for _, width := range []int{80, 120} {
			for _, st := range states {
				t.Run(fmt.Sprintf("%s/%d/%s", lang, width, st.name), func(t *testing.T) {
					m := st.apply(t, statusTestModel(t, lang, width))
					bar := m.StatusBar()
					if strings.Contains(bar, "\n") {
						t.Fatalf("bar wrapped:\n%q", bar)
					}
					if w := ansi.StringWidth(bar); w > width {
						t.Fatalf("bar is %d cells, want at most %d:\n%q", w, width, bar)
					}
					assertAllLinesFit(t, m.View(), width)
				})
			}
		}
	}
}

func TestStatusBarVeryNarrowStaysInsideWidth(t *testing.T) {
	for width := 1; width <= 40; width++ {
		for _, st := range barStates() {
			m := st.apply(t, statusTestModel(t, i18n.LangPTBR, width))
			bar := m.StatusBar()
			if w := ansi.StringWidth(bar); w > width || strings.Contains(bar, "\n") {
				t.Fatalf("%s at %d: %d cells:\n%q", st.name, width, w, bar)
			}
		}
	}
}

func TestStatusStringsExistInBothCatalogs(t *testing.T) {
	for _, key := range []string{"player.vol_max", "player.vol_min"} {
		seen := map[string]string{}
		for _, lang := range orientationLangs {
			got := mustCatalog(t, lang).T(key)
			if got == "" || got == key {
				t.Fatalf("%s: %s untranslated: %q", lang, key, got)
			}
			if w := ansi.StringWidth(got); w > 24 {
				t.Fatalf("%s: %s is %d cells, the notice must stay short", lang, key, w)
			}
			seen[lang] = got
		}
		if seen[i18n.LangPTBR] == seen[i18n.LangEN] {
			t.Fatalf("%s reads the same in both languages: %q", key, seen[i18n.LangEN])
		}
	}
	for _, lang := range orientationLangs {
		legend := mustCatalog(t, lang).T("help.nocolor")
		for _, g := range []string{glyphPlaying, glyphPaused, glyphError, glyphWarning} {
			if !strings.Contains(legend, g) {
				t.Fatalf("%s: help.nocolor must list %q: %q", lang, g, legend)
			}
		}
	}
}
