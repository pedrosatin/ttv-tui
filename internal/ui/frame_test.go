package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// rowIndex returns the first row of plain that contains want, or -1.
func rowIndex(rows []string, want string) int {
	for i, row := range rows {
		if strings.Contains(row, want) {
			return i
		}
	}
	return -1
}

// A single panel with a width and no height is drawn in full: nothing is cut,
// the narrow-chat notice goes under the list and the help under both.
func TestFrameWithoutHeightStacksNoticeAndHelpBelowPanels(t *testing.T) {
	const helpTitle = "Keyboard Shortcuts"

	for _, tc := range []struct {
		name       string
		width      int
		list, chat bool
		panel      string // text of the panel drawn first; empty when none is
		wantNotice bool
	}{
		{name: "chat", width: 120, chat: true, panel: "Chat — #gaules"},
		{name: "list", width: 120, list: true, panel: "Live Channels"},
		{name: "focus", width: 120, panel: focusHintLead},
		{name: "split too narrow for the chat", width: 60, list: true, chat: true, panel: "Live Channels", wantNotice: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 0})
			m = updated.(Model)
			m.layout.showList, m.layout.showChat = tc.list, tc.chat
			m.normalizeFocusAfterLayout()
			m.layout.helpVisible = true

			help := m.helpViewLines()
			rows := strings.Split(ansi.Strip(m.frame()), "\n")
			for i, row := range rows {
				if got := ansi.StringWidth(row); got > tc.width {
					t.Fatalf("row %d has %d cells in %d columns: %q", i, got, tc.width, row)
				}
			}
			if got, want := rows[len(rows)-1], ansi.Strip(m.StatusBar()); got != want {
				t.Fatalf("last row = %q, want the status bar %q", got, want)
			}

			panelAt := rowIndex(rows, tc.panel)
			helpAt := rowIndex(rows, helpTitle)
			if panelAt < 0 || helpAt < 0 || panelAt >= helpAt {
				t.Fatalf("panel at row %d and help at row %d, want the panel first:\n%s", panelAt, helpAt, strings.Join(rows, "\n"))
			}
			// Nothing is cut: the whole help sits right above the status bar.
			if want := len(rows) - 1 - len(help); helpAt != want {
				t.Fatalf("help starts at row %d, want %d (%d help rows above the status bar)", helpAt, want, len(help))
			}

			noticeAt := rowIndex(rows, narrowNoticeLead)
			if (noticeAt >= 0) != tc.wantNotice {
				t.Fatalf("notice at row %d, want shown=%v:\n%s", noticeAt, tc.wantNotice, strings.Join(rows, "\n"))
			}
			if tc.wantNotice && (noticeAt <= panelAt || noticeAt >= helpAt) {
				t.Fatalf("notice at row %d, want between the list (%d) and the help (%d)", noticeAt, panelAt, helpAt)
			}
		})
	}
}

// The split with a width and no height has as many rows as its taller panel,
// each one with both columns.
func TestFrameWithoutHeightSplitFollowsTheTallerPanel(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 0})
	m = withMessages(t, updated.(Model), 3)

	listW, chatW := m.panelWidths()
	if listW == 0 || chatW == 0 {
		t.Fatalf("widths = %d, %d, want a split", listW, chatW)
	}
	// Without a height the chat is asked for 20 rows, as frame does.
	want := max(len(m.renderBodyLines(listW)), len(m.renderChatLines(20, chatW)))

	rows := strings.Split(ansi.Strip(m.frame()), "\n")
	if len(rows) != want+1 {
		t.Fatalf("got %d rows, want %d and the status bar:\n%s", len(rows), want, strings.Join(rows, "\n"))
	}
	for i, row := range rows[:want] {
		if got := ansi.StringWidth(row); got != 120 {
			t.Fatalf("row %d has %d cells, want 120: %q", i, got, row)
		}
		if !strings.Contains(row, panelSeparator) {
			t.Fatalf("row %d has no panel separator: %q", i, row)
		}
	}
	if !strings.Contains(rows[0], "Live Channels") || !strings.Contains(rows[0], "Chat — #gaules") {
		t.Fatalf("first row = %q, want both panel titles", rows[0])
	}
	if got, want := rows[want], ansi.Strip(m.StatusBar()); got != want {
		t.Fatalf("last row = %q, want the status bar %q", got, want)
	}
}

// Focus mode on a terminal too short for the wrapped hint cuts the hint and
// keeps the status bar on the last row.
func TestFrameFocusHintIsCutToTheHeight(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 3})
	m = updated.(Model)
	m.layout.showList, m.layout.showChat = false, false
	m.normalizeFocusAfterLayout()

	if hint := m.focusModeHintLines(m.width); len(hint) <= 2 {
		t.Fatalf("the hint wraps to %d rows at 20 columns, want more than 2", len(hint))
	}
	rows := strings.Split(ansi.Strip(m.frame()), "\n")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], focusHintLead) {
		t.Fatalf("first row = %q, want the start of the focus hint", rows[0])
	}
	if got, want := rows[2], ansi.Strip(m.StatusBar()); got != want {
		t.Fatalf("last row = %q, want the status bar %q", got, want)
	}
}

// With neither a width nor a height, focus mode is the hint over the status bar.
func TestFrameUnsizedFocusShowsOnlyTheHint(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m.layout.showList, m.layout.showChat = false, false
	m.normalizeFocusAfterLayout()
	if m.width != 0 || m.height != 0 {
		t.Fatalf("size = %dx%d, want unsized", m.width, m.height)
	}

	rows := strings.Split(ansi.Strip(m.frame()), "\n")
	if len(rows) != 2 || !strings.Contains(rows[0], focusHintLead) {
		t.Fatalf("got %q, want the focus hint and the status bar", rows)
	}
}

// In the split layout without a height, all help lines appear below both panels
// (the taller of list and chat), with the status bar on the last row.
func TestFrameWithoutHeightSplitStacksHelpBelowPanels(t *testing.T) {
	const helpTitle = "Keyboard Shortcuts"

	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 0})
	m = withMessages(t, updated.(Model), 3)
	m.layout.helpVisible = true

	listW, chatW := m.panelWidths()
	if listW == 0 || chatW == 0 {
		t.Fatalf("widths = %d, %d, want a split", listW, chatW)
	}
	panelRows := max(len(m.renderBodyLines(listW)), len(m.renderChatLines(20, chatW)))
	help := m.helpViewLines()

	rows := strings.Split(ansi.Strip(m.frame()), "\n")

	panelAt := rowIndex(rows, "Live Channels")
	chatAt := rowIndex(rows, "Chat — #gaules")
	helpAt := rowIndex(rows, helpTitle)
	if panelAt < 0 || chatAt < 0 || helpAt < 0 || panelAt >= helpAt || chatAt >= helpAt {
		t.Fatalf("panels (list=%d, chat=%d) and help at %d, want panels before help:\n%s", panelAt, chatAt, helpAt, strings.Join(rows, "\n"))
	}

	if wantRows := panelRows + len(help) + 1; len(rows) != wantRows {
		t.Fatalf("got %d rows, want %d (%d panel rows + %d help rows + 1 status):\n%s", len(rows), wantRows, panelRows, len(help), strings.Join(rows, "\n"))
	}

	for i, hl := range help {
		gotRow := rows[panelRows+i]
		if !strings.Contains(gotRow, strings.TrimSpace(hl)) {
			t.Fatalf("row %d = %q, want help line %d %q", panelRows+i, gotRow, i, hl)
		}
	}

	if got, want := rows[len(rows)-1], ansi.Strip(m.StatusBar()); got != want {
		t.Fatalf("last row = %q, want status bar %q", got, want)
	}
}
