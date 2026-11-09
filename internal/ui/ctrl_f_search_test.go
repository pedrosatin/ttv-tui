package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Ticket 10: Ctrl+F is the primary global-search key; Ctrl+S is an alias.
// Both are delivered because the app runs the terminal in raw mode.
func TestCtrlFAndCtrlSOpenSearchFromList(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
	}{
		{"ctrl+f", tea.KeyCtrlF},
		{"ctrl+s", tea.KeyCtrlS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var searched []string
			m := newCategoriesModel(t, i18n.LangEN, 80, &searched)
			m, cmd := pressKey(m, tea.KeyMsg{Type: tc.key})
			if !m.Searching() || m.list.search.input != "" || cmd != nil {
				t.Fatalf("from list: searching=%v input=%q cmd=%v", m.Searching(), m.list.search.input, cmd != nil)
			}
			m = typeRunes(m, "gaules")
			m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.Mode() != ModeSearch || m.SearchQuery() != "gaules" || cmd == nil {
				t.Fatalf("submit: mode=%q query=%q cmd=%v", m.Mode(), m.SearchQuery(), cmd != nil)
			}
			m, _ = pressKeyMsg(m, cmd())
			if len(searched) != 1 || searched[0] != "gaules" {
				t.Fatalf("searched = %v, want [gaules]", searched)
			}
			if !strings.Contains(ansi.Strip(m.View()), "Gaules") {
				t.Fatalf("search results not shown:\n%s", ansi.Strip(m.View()))
			}
		})
	}
}

func TestCtrlFAndCtrlSFromFilterUseFilterText(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
	}{
		{"ctrl+f", tea.KeyCtrlF},
		{"ctrl+s", tea.KeyCtrlS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var searched []string
			m := newCategoriesModel(t, i18n.LangPTBR, 80, &searched)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			m = typeRunes(m, "gaules")
			if !m.Filtering() || m.Filter() != "gaules" {
				t.Fatalf("filter not active: filtering=%v filter=%q", m.Filtering(), m.Filter())
			}

			m, cmd := pressKey(m, tea.KeyMsg{Type: tc.key})
			if m.Mode() != ModeSearch || m.SearchQuery() != "gaules" || cmd == nil {
				t.Fatalf("%s must search filter text: mode=%q query=%q cmd=%v",
					tc.name, m.Mode(), m.SearchQuery(), cmd != nil)
			}
			if m.Filtering() || m.Filter() != "" {
				t.Fatalf("filter must be consumed: filtering=%v filter=%q", m.Filtering(), m.Filter())
			}
			m, _ = pressKeyMsg(m, cmd())
			if len(searched) != 1 || searched[0] != "gaules" {
				t.Fatalf("searched = %v, want [gaules]", searched)
			}
			if !strings.Contains(ansi.Strip(m.View()), "Gaules") {
				t.Fatalf("search results not shown:\n%s", ansi.Strip(m.View()))
			}
		})
	}
}

func TestCtrlFAndCtrlSSubmitSearchPrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
	}{
		{"ctrl+f", tea.KeyCtrlF},
		{"ctrl+s", tea.KeyCtrlS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var searched []string
			m := newCategoriesModel(t, i18n.LangEN, 80, &searched)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, "gaules")
			m, cmd := pressKey(m, tea.KeyMsg{Type: tc.key})
			if m.Mode() != ModeSearch || m.SearchQuery() != "gaules" || cmd == nil {
				t.Fatalf("%s must submit prompt: mode=%q query=%q cmd=%v",
					tc.name, m.Mode(), m.SearchQuery(), cmd != nil)
			}
			m, _ = pressKeyMsg(m, cmd())
			if len(searched) != 1 || searched[0] != "gaules" {
				t.Fatalf("searched = %v, want [gaules]", searched)
			}
			if !strings.Contains(ansi.Strip(m.View()), "Gaules") {
				t.Fatalf("search results not shown:\n%s", ansi.Strip(m.View()))
			}
		})
	}
}

func TestFilterEmptyHintAndHelpCiteCtrlF(t *testing.T) {
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
			if !strings.Contains(view, hint) {
				t.Fatalf("empty filter view missing hint %q:\n%s", hint, view)
			}
			if strings.Contains(view, "Ctrl+S busca") || strings.Contains(view, "Ctrl+S searches") {
				t.Fatalf("empty filter hint must not lead with Ctrl+S:\n%s", view)
			}

			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyF1})
			help := ansi.Strip(m.View())
			helpLine := m.t("help.search")
			if !strings.Contains(helpLine, "Ctrl+F") {
				t.Fatalf("help.search must cite Ctrl+F: %q", helpLine)
			}
			if !strings.Contains(help, helpLine) {
				t.Fatalf("help overlay missing %q:\n%s", helpLine, help)
			}
		})
	}
}
