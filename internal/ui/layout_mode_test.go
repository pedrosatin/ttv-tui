package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func sendLayoutKey(t *testing.T, m Model, keyType tea.KeyType) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: keyType})
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestLayoutCycleCtrlW(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	want := []LayoutMode{LayoutChat, LayoutList, LayoutFocus, LayoutSplit}
	for i, mode := range want {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
		if got := m.LayoutMode(); got != mode {
			t.Fatalf("cycle step %d: LayoutMode=%q, want %q", i+1, got, mode)
		}
	}
}

func TestLayoutIndependentToggles(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("start LayoutMode=%q, want split", m.LayoutMode())
	}

	m = sendLayoutKey(t, m, tea.KeyCtrlH) // hide list → chat only
	if m.LayoutMode() != LayoutChat {
		t.Fatalf("after Ctrl+H: %q, want chat", m.LayoutMode())
	}

	m = sendLayoutKey(t, m, tea.KeyCtrlL) // hide chat → focus
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("after Ctrl+L: %q, want focus", m.LayoutMode())
	}

	m = sendLayoutKey(t, m, tea.KeyCtrlH) // show list → list only
	if m.LayoutMode() != LayoutList {
		t.Fatalf("after Ctrl+H again: %q, want list", m.LayoutMode())
	}

	m = sendLayoutKey(t, m, tea.KeyCtrlL) // show chat → split
	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("after Ctrl+L again: %q, want split", m.LayoutMode())
	}
}

func TestLayoutPersistsInSessionAndStatusBar(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	m = sendLayoutKey(t, m, tea.KeyCtrlW) // chat
	if m.LayoutMode() != LayoutChat {
		t.Fatalf("LayoutMode=%q", m.LayoutMode())
	}

	// Unrelated navigation must keep the layout.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	if m.LayoutMode() != LayoutChat {
		t.Fatalf("layout lost after j: %q", m.LayoutMode())
	}

	// Chat-only drops the focus chrome, so 120 columns keep the layout token.
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "layout: chat") {
		t.Fatalf("status missing layout: chat:\n%s", plain)
	}
	status := ansi.Strip(m.StatusBar())
	if !strings.Contains(status, "layout: chat") {
		t.Fatalf("StatusBar missing layout: chat: %q", status)
	}
}

func TestLayoutFocusEscDoesNotRestorePanels(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	for i := 0; i < 3; i++ {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
	}
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("LayoutMode=%q, want focus", m.LayoutMode())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("Esc changed layout to %q", m.LayoutMode())
	}

	// Ctrl+H / Ctrl+L bring panels back from focus.
	m = sendLayoutKey(t, m, tea.KeyCtrlH)
	if m.LayoutMode() != LayoutList {
		t.Fatalf("Ctrl+H from focus: %q, want list", m.LayoutMode())
	}
	m = sendLayoutKey(t, m, tea.KeyCtrlL)
	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("Ctrl+L from list: %q, want split", m.LayoutMode())
	}
}

func TestLayoutHiddenChatDoesNotShowNarrowNotice(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)

	m = sendLayoutKey(t, m, tea.KeyCtrlL) // list only at wide width
	if m.LayoutMode() != LayoutList {
		t.Fatalf("LayoutMode=%q, want list", m.LayoutMode())
	}
	plainWide := ansi.Strip(m.View())
	if strings.Contains(plainWide, "Chat hidden (<80 cols)") {
		t.Fatalf("layout-hidden chat must not show narrow notice at 120:\n%s", plainWide)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 79, Height: 14})
	m = updated.(Model)
	plainNarrowList := ansi.Strip(m.View())
	if strings.Contains(plainNarrowList, "Chat hidden (<80 cols)") {
		t.Fatalf("layout-hidden chat must not show narrow notice at 79:\n%s", plainNarrowList)
	}

	// Back to the split through the cycle; Ctrl+L would open chat-only here.
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("LayoutMode=%q, want split", m.LayoutMode())
	}
	plainNarrow := ansi.Strip(m.View())
	if !strings.Contains(plainNarrow, "Chat hidden (<80 cols)") {
		t.Fatalf("width-hidden chat must still show narrow notice:\n%s", plainNarrow)
	}
}

func TestLayoutViewsAt80And120(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)

	for _, width := range []int{80, 120} {
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 14})
		m = updated.(Model)
		m.layout.showList, m.layout.showChat = true, true

		cases := []struct {
			name           string
			list, chat     bool
			wantSep        bool
			wantChatHeader bool
			wantListTitle  bool
			wantFocusHint  bool
		}{
			{"split", true, true, true, true, true, false},
			{"chat", false, true, false, true, false, false},
			{"list", true, false, false, false, true, false},
			{"focus", false, false, false, false, false, true},
		}
		for _, tc := range cases {
			m.layout.showList, m.layout.showChat = tc.list, tc.chat
			m.normalizeFocusAfterLayout()
			plain := ansi.Strip(m.View())
			lines := strings.Split(plain, "\n")
			if len(lines) != 14 {
				t.Fatalf("%s@%d: got %d lines:\n%s", tc.name, width, len(lines), plain)
			}
			for i, line := range lines {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s@%d line %d overflow (%d): %q", tc.name, width, i, ansi.StringWidth(line), line)
				}
			}
			hasSep := strings.Contains(plain, "│")
			if hasSep != tc.wantSep {
				t.Fatalf("%s@%d sep=%v want %v:\n%s", tc.name, width, hasSep, tc.wantSep, plain)
			}
			hasChat := strings.Contains(plain, "Chat — #gaules")
			if hasChat != tc.wantChatHeader {
				t.Fatalf("%s@%d chat header=%v want %v:\n%s", tc.name, width, hasChat, tc.wantChatHeader, plain)
			}
			hasList := strings.Contains(plain, "Live Channels") || strings.Contains(plain, "Categories")
			if hasList != tc.wantListTitle {
				t.Fatalf("%s@%d list title=%v want %v:\n%s", tc.name, width, hasList, tc.wantListTitle, plain)
			}
			hasHint := strings.Contains(plain, "Focus mode")
			if hasHint != tc.wantFocusHint {
				t.Fatalf("%s@%d focus hint=%v want %v:\n%s", tc.name, width, hasHint, tc.wantFocusHint, plain)
			}
			if m.LayoutMode() != LayoutMode(tc.name) {
				t.Fatalf("%s@%d LayoutMode=%q", tc.name, width, m.LayoutMode())
			}
			// Non-default modes announce layout: in the bar; split is the default
			// and stays silent so volume/category keep their columns.
			token := "layout: " + tc.name
			if tc.name == "split" {
				if strings.Contains(ansi.Strip(m.StatusBar()), "layout:") {
					t.Fatalf("split@%d should omit layout token:\n%s", width, m.StatusBar())
				}
			} else if width >= 120 {
				if !strings.Contains(plain, token) {
					t.Fatalf("%s@%d missing %q in status:\n%s", tc.name, width, token, plain)
				}
			}
			printScreen(t, "layout "+tc.name+" @ "+strconv.Itoa(width), m.View(), width)
		}
	}
}

func TestLayoutFocusHasNoPanels(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)
	m.layout.showList, m.layout.showChat = false, false
	m.normalizeFocusAfterLayout()

	listW, chatW := m.panelWidths()
	if listW != 0 || chatW != 0 {
		t.Fatalf("focus panelWidths=(%d,%d), want 0,0", listW, chatW)
	}
	plain := ansi.Strip(m.View())
	if strings.Contains(plain, "│") || strings.Contains(plain, "Chat — #gaules") || strings.Contains(plain, "gaules (gaules)") {
		t.Fatalf("focus view still shows panels:\n%s", plain)
	}
	if !strings.Contains(plain, "Focus mode") {
		t.Fatalf("focus view missing hint:\n%s", plain)
	}
}

func TestHelpDocumentsLayoutKeys(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	m := New(catEN)
	m.layout.helpVisible = true
	m.layout.helpSection = sectionWindow
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Ctrl+W", "Ctrl+H", "Ctrl+L", m.helpSectionName(sectionWindow)} {
		if !strings.Contains(plain, want) {
			t.Fatalf("help/status missing %q:\n%s", want, plain)
		}
	}
}
