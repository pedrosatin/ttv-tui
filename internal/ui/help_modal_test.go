package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var (
	keyHelpF1  = tea.KeyMsg{Type: tea.KeyF1}
	keyHelpQ   = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	keyHelpAsk = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	keyDown    = tea.KeyMsg{Type: tea.KeyDown}
	keyUp      = tea.KeyMsg{Type: tea.KeyUp}
	keyPgDown  = tea.KeyMsg{Type: tea.KeyPgDown}
	keyPgUp    = tea.KeyMsg{Type: tea.KeyPgUp}
	keyHome    = tea.KeyMsg{Type: tea.KeyHome}
	keyEnd     = tea.KeyMsg{Type: tea.KeyEnd}

	keyHelpTab      = tea.KeyMsg{Type: tea.KeyTab}
	keyHelpShiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}

	helpIndicatorRow = regexp.MustCompile(`(?:^|\s)(\d+)/(\d+)\s*$`)
)

// helpBody returns the plain rows above the status bar.
func helpBody(m Model) []string {
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	return rows[:len(rows)-1]
}

// helpPosition returns the indicator numbers, and false when none is drawn.
// The indicator is the last row of the help, which lies over the bottom rows.
func helpPosition(m Model) (cur, total int, ok bool) {
	rows := helpBody(m)
	if len(rows) == 0 || !m.HelpVisible() {
		return 0, 0, false
	}
	if match := helpIndicatorRow.FindStringSubmatch(rows[len(rows)-1]); match != nil {
		cur, _ = strconv.Atoi(match[1])
		total, _ = strconv.Atoi(match[2])
		return cur, total, true
	}
	return 0, 0, false
}

// helpSectionHeader is the row that opens a section of the help.
func helpSectionHeader(m Model, s helpSection) string {
	return "── " + m.helpSectionName(s) + " ──"
}

// helpTopSection is the section whose header is the first row under the fixed
// title, or "" when the top row is not a header.
func helpTopRow(m Model) string {
	rows := helpBody(m)
	for i, row := range rows {
		if strings.Contains(row, helpTitleRow(m)) && i+1 < len(rows) {
			return strings.TrimSpace(rows[i+1])
		}
	}
	return ""
}

func helpTitleRow(m Model) string {
	return "=== " + m.t("help.title") + " ==="
}

func helpTitleShown(m Model) bool {
	return strings.Contains(strings.Join(helpBody(m), "\n"), helpTitleRow(m))
}

// walkHelp scrolls one row at a time from the top and returns every row shown.
func walkHelp(t *testing.T, m Model) []string {
	t.Helper()
	m = sendKeys(m, keyHome)
	seen := append([]string(nil), helpBody(m)...)
	for range 200 {
		m = sendKeys(m, keyDown)
		seen = append(seen, helpBody(m)...)
		if cur, total, ok := helpPosition(m); !ok || cur == total {
			return seen
		}
	}
	t.Fatal("scrolling down never reached the last help row")
	return nil
}

func TestHelpEnterOpensNothingBehindIt(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, "en", 120, &searched)
	m = sendKeys(m, keyHelpAsk)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Enter with help open returned a command")
	}
	if m.Mode() != ModeCategories {
		t.Errorf("Enter with help open left categories: mode = %q", m.Mode())
	}
	if !m.HelpVisible() {
		t.Error("Enter with help open closed the help")
	}
}

func TestHelpEnterStartsNoChannel(t *testing.T) {
	m, pb, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	// Highlight a channel that is not playing, so Enter would really start it.
	m = sendKeys(m, keyEsc, keyDown)
	started := len(pb.started)
	m = sendKeys(m, keyHelpAsk)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || len(pb.started) != started {
		t.Errorf("Enter with help open acted on the list: cmd=%v started=%v", cmd != nil, pb.started)
	}
	if !m.HelpVisible() {
		t.Error("Enter with help open closed the help")
	}

	// Control: with the help closed, the same Enter does start the channel.
	if _, cmd = pressKey(sendKeys(m, keyEsc), tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("control failed: Enter on the list did nothing")
	}
}

func TestHelpCloseKeys(t *testing.T) {
	for name, closeKey := range map[string]tea.KeyMsg{
		"question": keyHelpAsk, "esc": keyEsc, "f1": keyHelpF1, "q": keyHelpQ,
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, 120, 24)
			focus := m.Focus()
			mode := m.Mode()
			m = sendKeys(m, keyHelpF1)
			if !m.HelpVisible() {
				t.Fatal("help did not open")
			}
			m = sendKeys(m, closeKey)
			if m.HelpVisible() {
				t.Fatal("help stayed open")
			}
			if m.Focus() != focus || m.Mode() != mode {
				t.Fatalf("closing moved the app: focus=%q mode=%q", m.Focus(), m.Mode())
			}
		})
	}
}

func TestHelpArrowsScrollHelpAndNotTheList(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 10)
	m = sendKeys(m, keyEsc) // list focus, so a leaked arrow would really move it
	highlighted := m.HighlightedID()
	m = sendKeys(m, keyHelpAsk)

	top := helpBody(m)
	m = sendKeys(m, keyDown)
	if strings.Join(helpBody(m), "\n") == strings.Join(top, "\n") {
		t.Fatal("Down did not scroll the help")
	}
	if m.HighlightedID() != highlighted {
		t.Fatalf("Down moved the list: %q -> %q", highlighted, m.HighlightedID())
	}
	m = sendKeys(m, keyUp)
	if strings.Join(helpBody(m), "\n") != strings.Join(top, "\n") {
		t.Fatal("Up did not scroll back to the top")
	}
	if m.HighlightedID() != highlighted {
		t.Fatalf("Up moved the list: %q -> %q", highlighted, m.HighlightedID())
	}
}

func TestHelpBatchedRunesScrollRowByRow(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 40, 15)
	m = sendKeys(m, keyHelpAsk)
	start, _, ok := helpPosition(m)
	if !ok {
		t.Fatal("no indicator at 40x15")
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")})
	if cur, _, _ := helpPosition(m); cur != start+3 {
		t.Fatalf("jjj moved to %d, want %d", cur, start+3)
	}
}

func TestHelpReachesAllContent(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, size := range [][2]int{{80, 24}, {40, 15}, {80, 12}, {60, 8}} {
			w, h := size[0], size[1]
			t.Run(fmt.Sprintf("%s/%dx%d", lang, w, h), func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = mustCatalog(t, lang)
				m = resize(m, w, h)
				m = sendKeys(m, keyEsc, keyHelpAsk)

				seen := walkHelp(t, m)
				for _, row := range seen {
					if width := ansi.StringWidth(row); width > w {
						t.Errorf("row wider than %d: %d %q", w, width, row)
					}
				}
				// Only help rows: the list behind a help that fits may elide.
				for _, row := range m.renderHelpLines(w) {
					if strings.Contains(row, "…") {
						t.Errorf("help row was truncated: %q", row)
					}
				}
				shown := map[string]bool{}
				for _, row := range seen {
					shown[strings.TrimRight(row, " ")] = true
				}
				for _, row := range m.renderHelpLines(w) {
					if plain := strings.TrimRight(ansi.Strip(row), " "); !shown[plain] {
						t.Errorf("help row never shown: %q", plain)
					}
				}

				// End lands on the same last page the row-by-row walk reaches.
				m = sendKeys(m, keyEnd)
				if !helpContainsLegend(strings.Join(helpBody(m), "\n"), lastWords(m.t("help.colors"), 3)) {
					t.Errorf("End does not show the end of the legend:\n%s", strings.Join(helpBody(m), "\n"))
				}
			})
		}
	}
}

func lastWords(s string, n int) string {
	words := strings.Fields(s)
	return strings.Join(words[len(words)-n:], " ")
}

func TestHelpNoIndicatorWhenEverythingFits(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 150)
	m = sendKeys(m, keyHelpAsk)
	if !helpTitleShown(m) {
		t.Fatal("help not shown")
	}
	before := strings.Join(helpBody(m), "\n")
	if _, _, ok := helpPosition(m); ok {
		t.Fatalf("indicator drawn although help fits:\n%s", before)
	}
	m = sendKeys(m, keyDown, keyPgDown, keyEnd, keyHelpTab)
	if got := strings.Join(helpBody(m), "\n"); got != before {
		t.Fatalf("help scrolled although it fits:\n%s", got)
	}
}

func TestHelpIndicatorTracksPosition(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 40, 15)
	m = sendKeys(m, keyHelpAsk)

	first, total, ok := helpPosition(m)
	if !ok {
		t.Fatalf("no indicator although help overflows:\n%s", strings.Join(helpBody(m), "\n"))
	}
	if first >= total {
		t.Fatalf("top position %d/%d", first, total)
	}
	if !helpTitleShown(m) {
		t.Fatal("the title left the screen")
	}

	m = sendKeys(m, keyEnd)
	if cur, _, _ := helpPosition(m); cur != total {
		t.Fatalf("End at %d, want %d", cur, total)
	}
	m = sendKeys(m, keyDown)
	if cur, _, _ := helpPosition(m); cur != total {
		t.Fatalf("Down past the end at %d, want %d", cur, total)
	}
	if !helpTitleShown(m) {
		t.Fatal("the title left the screen at the end")
	}
	m = sendKeys(m, keyHome)
	top, _, _ := helpPosition(m)
	if top >= first {
		t.Fatalf("Home at %d, want above the section the help opened at (%d)", top, first)
	}
	m = sendKeys(m, keyUp)
	if cur, _, _ := helpPosition(m); cur != top {
		t.Fatalf("Up past the top at %d, want %d", cur, top)
	}
	first = top

	m = sendKeys(m, keyPgDown)
	paged, _, _ := helpPosition(m)
	if paged <= first+1 {
		t.Fatalf("PgDn moved from %d to %d, want more than one row", first, paged)
	}
	m = sendKeys(m, keyPgUp)
	if cur, _, _ := helpPosition(m); cur != first {
		t.Fatalf("PgUp at %d, want %d", cur, first)
	}
}

func TestHelpReopensAtTheFocusSection(t *testing.T) {
	for name, closeKey := range map[string]tea.KeyMsg{
		"question": keyHelpAsk, "esc": keyEsc, "f1": keyHelpF1, "q": keyHelpQ,
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, 40, 15)
			m = sendKeys(m, keyHelpAsk, keyEnd)
			nav := helpSectionHeader(m, sectionNav)
			if helpTopRow(m) == nav {
				t.Fatal("End left the list section on top; this test proves nothing")
			}
			m = sendKeys(m, closeKey, keyHelpAsk)
			if !m.HelpVisible() || helpTopRow(m) != nav {
				t.Fatalf("reopened help is not at the list section:\n%s", strings.Join(helpBody(m), "\n"))
			}
		})
	}
}

func TestHelpShrinkingTheTerminalKeepsThePositionValid(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 40, 15)
	m = sendKeys(m, keyHelpAsk, keyEnd)

	m = resize(m, 120, 150)
	if _, _, ok := helpPosition(m); ok {
		t.Fatalf("indicator kept after growing to fit:\n%s", strings.Join(helpBody(m), "\n"))
	}
	if !helpTitleShown(m) {
		t.Fatal("help that fits must start at the title")
	}

	m = resize(m, 40, 15)
	m = sendKeys(m, keyEnd)
	end, total, _ := helpPosition(m)
	m = resize(m, 40, 30)
	m = sendKeys(m, keyUp)
	cur, newTotal, ok := helpPosition(m)
	if ok && cur != newTotal-1 {
		t.Fatalf("Up after resizing at %d/%d (was %d/%d)", cur, newTotal, end, total)
	}
}

func TestHelpLongPlayerErrorIsReachable(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m.player.lastLine = strings.TrimSpace(strings.Repeat("streamlink falhou ao abrir ", 6)) + " fimdoerro"
	m = resize(m, 40, 15)
	m = sendKeys(m, keyHelpAsk, keyEnd)
	if body := strings.Join(helpBody(m), "\n"); !strings.Contains(body, "fimdoerro") {
		t.Fatalf("end of player error not reachable:\n%s", body)
	}
}

// helpState is everything a key could change behind the help.
type helpState struct {
	mode, focus, highlighted, draft, filter, searchInput, layout, langs string
	composing, filtering, searching, filterMenu                         bool
	menuIndex, favorites, started, stopped, pauses, volumes             int
	reconnected, sent                                                   int
}

func snapshotBehindHelp(m Model, pb *fakePlayback, fc *fakeChat) helpState {
	return helpState{
		mode: string(m.Mode()), focus: string(m.Focus()), highlighted: m.HighlightedID(),
		draft: m.ComposeDraft(), filter: m.list.filter, searchInput: m.list.search.input,
		layout: string(m.LayoutMode()), langs: strings.Join(m.list.prefs.languages, ","),
		composing: m.Composing(), filtering: m.Filtering(), searching: m.Searching(),
		filterMenu: m.list.prefs.menuOpen, menuIndex: m.list.prefs.menuIndex,
		favorites: len(m.list.favorites), started: len(pb.started), stopped: pb.stopped,
		pauses: pb.pauses, volumes: len(pb.volDeltas), reconnected: fc.reconnected, sent: len(fc.sent),
	}
}

func TestHelpKeysNeverLeakBehindIt(t *testing.T) {
	r := func(c rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}} }
	keys := map[string]tea.KeyMsg{
		"enter": {Type: tea.KeyEnter}, "tab": {Type: tea.KeyTab}, "tab-rune": r('\t'),
		"i": r('i'), "f": r('f'), "r": r('r'), "l": r('l'), "y": r('y'), "slash": r('/'),
		"a": r('a'), "space": {Type: tea.KeySpace}, "plus": r('+'), "minus": r('-'),
		"backspace": {Type: tea.KeyBackspace}, "delete": {Type: tea.KeyDelete},
		"ctrl+f": {Type: tea.KeyCtrlF}, "ctrl+s": {Type: tea.KeyCtrlS},
		"ctrl+w": {Type: tea.KeyCtrlW}, "ctrl+h": {Type: tea.KeyCtrlH},
		"ctrl+l": {Type: tea.KeyCtrlL}, "ctrl+r": {Type: tea.KeyCtrlR},
		"ctrl+x": {Type: tea.KeyCtrlX}, "ctrl+t": {Type: tea.KeyCtrlT},
		"up": keyUp, "down": keyDown, "pgup": keyPgUp, "pgdown": keyPgDown,
		"home": keyHome, "end": keyEnd, "j": r('j'), "k": r('k'), "G": r('G'),
		"shift+tab": keyHelpShiftTab, "left": {Type: tea.KeyLeft}, "right": {Type: tea.KeyRight},
		"paste-q":    {Type: tea.KeyRunes, Runes: []rune{'q'}, Paste: true},
		"paste-text": {Type: tea.KeyRunes, Runes: []rune("texto colado"), Paste: true},
	}
	modes := append(everyModeEnter(t), struct {
		name  string
		enter func(t *testing.T, m Model) Model
	}{"filter menu", func(t *testing.T, m Model) Model {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		if !m.list.prefs.menuOpen {
			t.Fatal("filter menu did not open")
		}
		return m
	}})

	for _, mode := range modes {
		for name, k := range keys {
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				m, pb, fc := setupTwoPanelModel(t)
				m = resize(m, 80, 12)
				m, _ = pressKey(m, keyEsc)
				m = mode.enter(t, m)
				m, _ = pressKey(m, keyHelpF1)
				if !m.HelpVisible() {
					t.Fatal("help did not open")
				}
				before := snapshotBehindHelp(m, pb, fc)

				m, cmd := pressKey(m, k)

				if !m.HelpVisible() {
					t.Fatal("key closed the help")
				}
				if cmd != nil {
					t.Error("key returned a command")
				}
				if after := snapshotBehindHelp(m, pb, fc); after != before {
					t.Fatalf("key leaked:\nbefore %+v\nafter  %+v", before, after)
				}
			})
		}
	}
}

func TestHelpCloseReturnsToTheModeItCoveredWithInputIntact(t *testing.T) {
	for _, mode := range everyModeEnter(t) {
		t.Run(mode.name, func(t *testing.T) {
			m, pb, fc := setupTwoPanelModel(t)
			m = resize(m, 120, 24)
			m, _ = pressKey(m, keyEsc)
			m = mode.enter(t, m)
			before := snapshotBehindHelp(m, pb, fc)
			m = sendKeys(m, keyHelpF1, keyHelpQ)
			if m.HelpVisible() {
				t.Fatal("q left the help open")
			}
			if after := snapshotBehindHelp(m, pb, fc); after != before {
				t.Fatalf("opening and closing changed the app:\nbefore %+v\nafter  %+v", before, after)
			}
		})
	}
}

// Ctrl+Q is the contract of TestCtrlQQuitsFromEveryMode; the help must not
// hold the app hostage while its own text says "Ctrl+Q quit".
func TestHelpDoesNotBlockQuit(t *testing.T) {
	m, pb, fc := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	m = sendKeys(m, keyHelpAsk)
	pbStops, chatStops := pb.stopped, fc.stopped
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !m.Quitting() || !cmdQuits(cmd) {
		t.Fatal("Ctrl+Q did not quit with help open")
	}
	if pb.stopped != pbStops+1 || fc.stopped != chatStops+1 {
		t.Fatalf("quit skipped cleanup: player %d chat %d", pb.stopped, fc.stopped)
	}
}

func TestHelpCtrlCOnlyShowsTheQuitHint(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	m = sendKeys(m, keyHelpAsk)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.Quitting() || cmdQuits(cmd) {
		t.Fatal("Ctrl+C quit the app")
	}
	if !m.HelpVisible() {
		t.Fatal("Ctrl+C closed the help")
	}
	if m.CtrlCNotice() != m.t("status.ctrl_c_hint") {
		t.Fatalf("CtrlCNotice = %q", m.CtrlCNotice())
	}
}

// With the filter menu open, F1 used to set the flag and draw nothing: under a
// modal help that would swallow every key on an invisible screen.
func TestHelpOverFilterMenuIsVisibleAndRestoresIt(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	m = sendKeys(m, keyEsc, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}, keyHelpF1)
	if !helpTitleShown(m) {
		t.Fatalf("help hidden behind the filter menu:\n%s", strings.Join(helpBody(m), "\n"))
	}
	m = sendKeys(m, keyEsc)
	if m.HelpVisible() {
		t.Fatal("Esc left the help open")
	}
	if !m.list.prefs.menuOpen {
		t.Fatal("closing the help closed the filter menu too")
	}
}

// A one-row terminal draws only the status bar. Help there cannot be seen, so
// it must not eat keys.
func TestHelpInvisibleOnOneRowDoesNotTrapKeys(t *testing.T) {
	var searched []string
	m := newCategoriesModel(t, "en", 120, &searched)
	m = resize(m, 120, 1)
	m = sendKeys(m, keyHelpAsk)
	_, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter was swallowed by a help nobody can see")
	}
}

func TestHelpTwoRowsStillScrollsToTheEnd(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 2)
	m = sendKeys(m, keyHelpAsk)
	if rows := helpBody(m); len(rows) != 1 || !strings.Contains(rows[0], helpSectionHeader(m, sectionNav)) {
		t.Fatalf("top row = %q", rows)
	}
	m = sendKeys(m, keyEnd)
	if rows := helpBody(m); len(rows) != 1 || !strings.Contains(rows[0], lastWords(m.t("help.colors"), 1)) {
		t.Fatalf("End row = %q", rows)
	}
}

func TestHelpTinyTerminalsDoNotPanic(t *testing.T) {
	for w := 1; w <= 14; w++ {
		for h := 1; h <= 6; h++ {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, w, h)
			m = sendKeys(m, keyHelpF1, keyDown, keyEnd, keyPgUp, keyPgDown, keyHome, keyUp)
			if rows := strings.Split(m.View(), "\n"); len(rows) > h {
				t.Errorf("%dx%d drew %d rows", w, h, len(rows))
			}
		}
	}
}

func TestHelpWrapsWithConsistentIndent(t *testing.T) {
	for _, lang := range orientationLangs {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			m = resize(m, 40, 15)
			m = sendKeys(m, keyHelpAsk)
			rows := m.renderHelpLines(40)
			if !strings.HasPrefix(rows[0], "===") {
				t.Fatalf("title row = %q", rows[0])
			}
			// Headers start the row; keys sit at 2 or, under a situation, 4; the
			// text and its continuation start at the text column.
			textCol := helpKeyColumn(m.helpEntries(), 40)
			for _, row := range rows[1:] {
				plain := ansi.Strip(row)
				indent := len(plain) - len(strings.TrimLeft(plain, " "))
				if indent == 0 && strings.HasPrefix(plain, "── ") {
					continue
				}
				if indent != 2 && indent != 4 && indent != textCol {
					t.Errorf("indent %d on %q (text column %d)", indent, plain, textCol)
				}
			}
		})
	}
}

func TestBlankRowSeparatesOverlaysFromTheBackground(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 100, 150)
	m = sendKeys(m, keyEsc)

	assertBlankAbove := func(m Model, marker string) {
		t.Helper()
		rows := helpBody(m)
		for i, row := range rows {
			if strings.HasPrefix(row, marker) {
				if i == 0 || strings.TrimSpace(rows[i-1]) != "" {
					t.Fatalf("no blank row above %q:\n%s", marker, strings.Join(rows, "\n"))
				}
				return
			}
		}
		t.Fatalf("%q not on screen:\n%s", marker, strings.Join(rows, "\n"))
	}

	help := sendKeys(m, keyHelpAsk)
	assertBlankAbove(help, helpTitleRow(help))

	menu := sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	assertBlankAbove(menu, "=== "+menu.t("channel_filter.title")+" ===")
}
