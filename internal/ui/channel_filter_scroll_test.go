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

var menuPosition = regexp.MustCompile(`^\s*(\d+)/(\d+)$`)

// menuWithLanguages opens the filter menu over n distinct stream languages.
func menuWithLanguages(t *testing.T, lang string, n, width, height int) Model {
	t.Helper()
	m := New(testCatalog(t, lang))
	m.list.mode = ModeChannels
	for i := range n {
		code := fmt.Sprintf("%c%c", 'a'+i/26, 'a'+i%26)
		m.list.channels = append(m.list.channels, catalog.Channel{
			ID:           fmt.Sprintf("id-%d", i),
			Login:        fmt.Sprintf("chan%d", i),
			DisplayName:  fmt.Sprintf("Chan%d", i),
			Language:     catalog.LanguagePtr(code),
			Viewers:      catalog.IntPtr(100 + i),
			Availability: catalog.AvailabilityLive,
		})
	}
	m.reconcileHighlight()
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: height})
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !m.ChannelFilterMenuOpen() {
		t.Fatal("l must open the filter menu")
	}
	return m
}

func viewRows(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// cursorRows returns the on-screen rows holding the menu cursor.
func cursorRows(m Model) []string {
	var out []string
	for _, row := range viewRows(m) {
		if strings.Contains(row, "▸") {
			out = append(out, row)
		}
	}
	return out
}

// menuPositionRow returns the position indicator on screen, or "" without one.
func menuPositionRow(m Model) string {
	for _, row := range viewRows(m) {
		if menuPosition.MatchString(strings.TrimRight(row, " ")) {
			return strings.TrimSpace(row)
		}
	}
	return ""
}

func assertMenuCursorVisible(t *testing.T, m Model, height int, step string) {
	t.Helper()
	rows := viewRows(m)
	if len(rows) > height {
		t.Fatalf("%s: %d rows on a %d-row terminal:\n%s", step, len(rows), height, strings.Join(rows, "\n"))
	}
	if got := cursorRows(m); len(got) != 1 {
		t.Fatalf("%s: cursor rows = %d, want 1:\n%s", step, len(got), strings.Join(rows, "\n"))
	}
}

func TestChannelFilterMenuCursorVisibleWith30LanguagesAt80x24(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m := menuWithLanguages(t, lang, 30, 80, 24)
			last := len(m.list.prefs.menuItems) - 1
			if last != 35 {
				t.Fatalf("menu items = %d, want 36", last+1)
			}
			for i := 0; i <= last; i++ {
				assertMenuCursorVisible(t, m, 24, fmt.Sprintf("down step %d", i))
				if m.list.prefs.menuIndex != i {
					t.Fatalf("index = %d, want %d", m.list.prefs.menuIndex, i)
				}
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
			}
			for i := last; i >= 0; i-- {
				assertMenuCursorVisible(t, m, 24, fmt.Sprintf("up step %d", i))
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
			}
		})
	}
}

func TestChannelFilterMenuCursorRowIsTheSelectedItem(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangEN, 30, 80, 24)
	for range 12 {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	// Index 0 is "all languages", so index 12 is the 12th code: al.
	got := cursorRows(m)
	if len(got) != 1 || !strings.Contains(got[0], "AL (1 in list)") {
		t.Fatalf("cursor row = %q, want AL (1 in list)", got)
	}
}

func TestChannelFilterMenuAudienceReachableWith30Languages(t *testing.T) {
	var persisted int
	m := menuWithLanguages(t, i18n.LangPTBR, 30, 80, 24)
	m.list.prefs.persist = func(_ []string, minViewers int) error {
		persisted = minViewers
		return nil
	}

	for m.list.prefs.menuItems[m.list.prefs.menuIndex].Kind != filterMenuAudience {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, m.t("channel_filter.audience_header")) {
		t.Fatalf("audience header must show on its first row:\n%s", view)
	}
	if !strings.Contains(cursorRows(m)[0], m.t("channel_filter.audience_any")) {
		t.Fatalf("cursor must sit on any-audience: %q", cursorRows(m))
	}

	for m.list.prefs.menuIndex+1 < len(m.list.prefs.menuItems) {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	if !strings.Contains(cursorRows(m)[0], "5000+") {
		t.Fatalf("cursor must reach 5000+: %q", cursorRows(m))
	}
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if m.FilterMinViewers() != 5000 || persisted != 5000 {
		t.Fatalf("min viewers = %d persisted = %d, want 5000", m.FilterMinViewers(), persisted)
	}
	if !strings.Contains(cursorRows(m)[0], "(•) 5000+") {
		t.Fatalf("selection must show on the cursor row: %q", cursorRows(m))
	}
}

func TestChannelFilterMenuPositionOnlyWhenScrolling(t *testing.T) {
	// 2 header rows + all-languages + audience header + 5 bands + N languages:
	// 14 languages fill the 23 rows of an 80x24 body exactly.
	for _, tc := range []struct {
		name      string
		languages int
		want      string
	}{
		{"few languages", 3, ""},
		{"exact fit", 14, ""},
		{"one row over", 15, "1/21"},
		{"thirty", 30, "1/36"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := menuWithLanguages(t, i18n.LangEN, tc.languages, 80, 24)
			if got := menuPositionRow(m); got != tc.want {
				t.Fatalf("position = %q, want %q:\n%s", got, tc.want, ansi.Strip(m.View()))
			}
		})
	}
}

func TestChannelFilterMenuPositionFollowsCursorAndAlignsRight(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangEN, 30, 80, 24)
	for range 9 {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if got := menuPositionRow(m); got != "10/36" {
		t.Fatalf("position = %q, want 10/36", got)
	}
	for _, row := range viewRows(m) {
		if strings.HasSuffix(strings.TrimRight(row, " "), "10/36") {
			if width := ansi.StringWidth(strings.TrimRight(row, " ")); width != 80 {
				t.Fatalf("position ends at column %d, want 80: %q", width, row)
			}
			return
		}
	}
	t.Fatal("position row not found")
}

func TestChannelFilterMenuKeepsTitleAndHintWhileScrolling(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangPTBR, 30, 80, 24)
	for m.list.prefs.menuIndex+1 < len(m.list.prefs.menuItems) {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	rows := viewRows(m)
	if !strings.Contains(rows[0], m.t("channel_filter.title")) {
		t.Fatalf("title must stay on the first row:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[1], "Enter/Espaço") {
		t.Fatalf("hint must stay on the second row:\n%s", strings.Join(rows, "\n"))
	}
}

func TestChannelFilterMenuCursorVisibleOnSmallTerminals(t *testing.T) {
	for height := 2; height <= 12; height++ {
		t.Run(fmt.Sprintf("height %d", height), func(t *testing.T) {
			m := menuWithLanguages(t, i18n.LangEN, 30, 80, height)
			for i := range m.list.prefs.menuItems {
				assertMenuCursorVisible(t, m, height, fmt.Sprintf("step %d", i))
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			}
		})
	}
}

func TestChannelFilterMenuCursorSurvivesResize(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangEN, 30, 80, 24)
	for range 30 {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for _, height := range []int{10, 40, 24, 5} {
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: height})
		assertMenuCursorVisible(t, m, height, fmt.Sprintf("resize to %d", height))
	}
}

func TestChannelFilterMenuTallTerminalShowsEverythingWithoutPosition(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangEN, 30, 80, 60)
	if got := menuPositionRow(m); got != "" {
		t.Fatalf("position = %q, want none on a tall terminal", got)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"AA (1 in list)", "BD (1 in list)", "5000+"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func TestChannelFilterMenuScrollsInEveryLayout(t *testing.T) {
	want := []LayoutMode{LayoutSplit, LayoutChat, LayoutList, LayoutFocus}
	for cycles, mode := range want {
		m := menuWithLanguages(t, i18n.LangEN, 30, 80, 24)
		m = m.closeChannelFilterMenu()
		m.chat.panel.channel = "gaules"
		m.layout.showList, m.layout.showChat = true, true
		for range cycles {
			m.cycleLayout()
		}
		if got := m.LayoutMode(); got != mode {
			t.Fatalf("layout = %q, want %q", got, mode)
		}
		m = m.openChannelFilterMenu()
		t.Run(string(mode), func(t *testing.T) {
			for i := range m.list.prefs.menuItems {
				assertMenuCursorVisible(t, m, 24, fmt.Sprintf("step %d", i))
				want := fmt.Sprintf("%d/36", i+1)
				if got := menuPositionRow(m); got != want {
					t.Fatalf("position = %q, want %q:\n%s", got, want, ansi.Strip(m.View()))
				}
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
			}
		})
	}
}

func TestChannelFilterMenuQuickNavigation(t *testing.T) {
	m := menuWithLanguages(t, i18n.LangEN, 30, 80, 24)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.list.prefs.menuIndex != 35 {
		t.Fatalf("End index = %d, want 35", m.list.prefs.menuIndex)
	}
	assertMenuCursorVisible(t, m, 24, "End")
	if got := menuPositionRow(m); got != "36/36" {
		t.Fatalf("position = %q", got)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.list.prefs.menuIndex != 0 {
		t.Fatalf("Home index = %d", m.list.prefs.menuIndex)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.list.prefs.menuIndex != 19 {
		t.Fatalf("PgDown index = %d, want 19", m.list.prefs.menuIndex)
	}
	if got := menuPositionRow(m); got != "20/36" {
		t.Fatalf("position = %q", got)
	}
	assertMenuCursorVisible(t, m, 24, "PgDown")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.list.prefs.menuIndex != 0 {
		t.Fatalf("PgUp index = %d", m.list.prefs.menuIndex)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.list.prefs.menuIndex != 0 {
		t.Fatalf("j/k index = %d", m.list.prefs.menuIndex)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.list.prefs.menuIndex != 35 {
		t.Fatalf("G index = %d", m.list.prefs.menuIndex)
	}
	short := menuWithLanguages(t, i18n.LangEN, 0, 80, 24)
	short, _ = pressKey(short, tea.KeyMsg{Type: tea.KeyEnd})
	assertMenuCursorVisible(t, short, 24, "short End")
	if short.list.prefs.menuIndex != 5 {
		t.Fatalf("short End index = %d", short.list.prefs.menuIndex)
	}
	short, _ = pressKey(short, tea.KeyMsg{Type: tea.KeyHome})
	if short.list.prefs.menuIndex != 0 {
		t.Fatalf("short Home index = %d", short.list.prefs.menuIndex)
	}
}
