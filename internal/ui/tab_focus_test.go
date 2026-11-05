package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func draftModel(t *testing.T, draft string, composing bool, focus Focus) Model {
	t.Helper()
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	m.composeDraft = draft
	m.composing = composing
	m.focus = focus
	m.helpVisible = false
	return m
}

func pressTab(m Model) Model {
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	return updated.(Model)
}

func TestTabSwitchesFocusInFourStatesAndPreservesDraft(t *testing.T) {
	const draft = "bruno diz oi"

	cases := []struct {
		name        string
		composing   bool
		focus       Focus
		helpOpen    bool
		wantFocus   Focus
		wantHelp    bool
		wantCompose bool
	}{
		{
			name: "list_focus", focus: FocusList, wantFocus: FocusChat,
		},
		{
			name: "chat_focus", focus: FocusChat, wantFocus: FocusList,
		},
		{
			name: "composing", focus: FocusChat, composing: true, wantFocus: FocusList, wantCompose: false,
		},
		{
			name: "help_open_on_list", focus: FocusList, helpOpen: true, wantFocus: FocusList, wantHelp: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := draftModel(t, draft, tc.composing, tc.focus)
			m.helpVisible = tc.helpOpen
			selected := m.HighlightedID()

			m = pressTab(m)

			if m.Focus() != tc.wantFocus {
				t.Fatalf("Focus=%q, want %q", m.Focus(), tc.wantFocus)
			}
			if m.HelpVisible() != tc.wantHelp {
				t.Fatalf("HelpVisible=%v, want %v", m.HelpVisible(), tc.wantHelp)
			}
			if m.Composing() != tc.wantCompose {
				t.Fatalf("Composing=%v, want %v", m.Composing(), tc.wantCompose)
			}
			if m.ComposeDraft() != draft {
				t.Fatalf("ComposeDraft=%q, want %q", m.ComposeDraft(), draft)
			}
			if m.HighlightedID() != selected {
				t.Fatalf("HighlightedID=%q, want %q", m.HighlightedID(), selected)
			}
		})
	}
}

func TestTabRoundTripKeepsDraftAndSelection(t *testing.T) {
	const draft = "rascunho ida e volta"
	m := draftModel(t, draft, true, FocusChat)
	// Leave compose via Tab to list, move highlight, Tab back to chat, Tab to list again.
	before := m.HighlightedID()
	m = pressTab(m) // composing → list
	if m.Focus() != FocusList || m.Composing() || m.ComposeDraft() != draft {
		t.Fatalf("after Tab#1: Focus=%q Composing=%v Draft=%q", m.Focus(), m.Composing(), m.ComposeDraft())
	}
	if m.HighlightedID() != before {
		t.Fatalf("selection changed on Tab out of compose: %q → %q", before, m.HighlightedID())
	}

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	moved := m.HighlightedID()
	if moved == before {
		t.Fatal("expected j to move highlight while list focused")
	}

	m = pressTab(m) // list → chat
	if m.Focus() != FocusChat || m.ComposeDraft() != draft {
		t.Fatalf("after Tab#2: Focus=%q Draft=%q", m.Focus(), m.ComposeDraft())
	}
	if m.HighlightedID() != moved {
		t.Fatalf("selection lost after Tab to chat: %q, want %q", m.HighlightedID(), moved)
	}

	m = pressTab(m) // chat → list
	if m.Focus() != FocusList || m.ComposeDraft() != draft || m.HighlightedID() != moved {
		t.Fatalf("after Tab#3: Focus=%q Draft=%q Highlight=%q", m.Focus(), m.ComposeDraft(), m.HighlightedID())
	}
}

func TestEnterAfterTabOpensChannelOrStartsCompose(t *testing.T) {
	m := draftModel(t, "texto intacto", false, FocusChat)
	// Ensure list has a second channel selected path: focus list, highlight alanzoka, Enter.
	m = pressTab(m) // chat → list
	if m.Focus() != FocusList {
		t.Fatalf("Focus=%q, want list", m.Focus())
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if id := m.HighlightedID(); id != "ch-alan" {
		t.Fatalf("HighlightedID=%q, want ch-alan", id)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Enter on list must select the highlighted channel")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.SelectedChannel() == nil || m.SelectedChannel().Login != "alanzoka" {
		t.Fatalf("SelectedChannel=%+v, want alanzoka", m.SelectedChannel())
	}
	if m.ComposeDraft() != "texto intacto" {
		t.Fatalf("draft lost after opening channel: %q", m.ComposeDraft())
	}

	m = pressTab(m) // list → chat (may already be chat after select; force list then chat)
	if m.Focus() != FocusChat {
		m = pressTab(m)
	}
	if m.Focus() != FocusChat {
		t.Fatalf("Focus=%q, want chat before Enter→compose", m.Focus())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.Composing() || m.Focus() != FocusChat {
		t.Fatalf("Enter on chat: Composing=%v Focus=%q", m.Composing(), m.Focus())
	}
	if m.ComposeDraft() != "texto intacto" {
		t.Fatalf("draft lost entering compose: %q", m.ComposeDraft())
	}
}

func TestTabNeverDropsTypedDraftAcrossCombinations(t *testing.T) {
	drafts := []string{"a", "olá Bruno", "emoji 🎉 e ç", strings.Repeat("x", 40)}
	for _, draft := range drafts {
		m := draftModel(t, "", true, FocusChat)
		m = typeRunes(m, draft)
		if m.ComposeDraft() != draft {
			t.Fatalf("typed draft=%q, want %q", m.ComposeDraft(), draft)
		}

		// Help open while composing: Tab belongs to the help, nothing moves.
		m.helpVisible = true
		m = pressTab(m)
		if !m.HelpVisible() || m.Focus() != FocusChat || !m.Composing() || m.ComposeDraft() != draft {
			t.Fatalf("help+Tab: help=%v focus=%q composing=%v draft=%q", m.HelpVisible(), m.Focus(), m.Composing(), m.ComposeDraft())
		}

		// Help closed, Tab leaves composition and moves to list.
		m = sendKeys(m, keyEsc)
		m = pressTab(m)
		if m.HelpVisible() || m.Focus() != FocusList || m.Composing() || m.ComposeDraft() != draft {
			t.Fatalf("Tab after help: help=%v focus=%q composing=%v draft=%q", m.HelpVisible(), m.Focus(), m.Composing(), m.ComposeDraft())
		}

		// Tab to chat, Enter into compose, Esc out, Tab around again.
		m = pressTab(m)
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		m = sendKeys(m, keyEsc)
		m = pressTab(m)
		m = pressTab(m)
		if m.ComposeDraft() != draft {
			t.Fatalf("draft lost after focus churn: got %q want %q", m.ComposeDraft(), draft)
		}
	}
}

func TestTabAsRuneAlsoSwitchesFocus(t *testing.T) {
	m := draftModel(t, "tab rune", true, FocusChat)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\t'}})
	m = updated.(Model)
	if m.Focus() != FocusList || m.Composing() || m.ComposeDraft() != "tab rune" {
		t.Fatalf("Focus=%q Composing=%v Draft=%q", m.Focus(), m.Composing(), m.ComposeDraft())
	}
}

func TestHelpAndStatusShowFocusedPanel(t *testing.T) {
	m := draftModel(t, "visível", false, FocusList)
	m = resize(m, 120, 24)

	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, m.t("status.focus_list")) {
		t.Fatalf("status bar missing list focus label:\n%s", bar)
	}

	m = pressTab(m)
	bar = ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, m.t("status.focus_chat")) {
		t.Fatalf("status bar missing chat focus label:\n%s", bar)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	want := "Focused: chat — after closing help, Tab switches panels and keeps draft"
	if !strings.Contains(view, want) {
		t.Fatalf("help missing current focus line %q:\n%s", want, view)
	}

	// Esc still only closes help; focus stays on chat.
	m = sendKeys(m, keyEsc)
	if m.HelpVisible() || m.Focus() != FocusChat || m.ComposeDraft() != "visível" {
		t.Fatalf("Esc on help: help=%v focus=%q draft=%q", m.HelpVisible(), m.Focus(), m.ComposeDraft())
	}
}
