package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Antes do registro de ações, três teclas eram reconhecidas pelo tipo, com
// qualquer modificador. Estes testes montam a mensagem à mão, sem passar pelo
// registro, para que uma tecla que some dele não suma também do teste.

func TestAltSpaceTogglesTheChannelFilterItem(t *testing.T) {
	m := channelFilterFixture(t)
	m.list.prefs.persist = func([]string, int) error { return nil }
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if !m.ChannelFilterMenuOpen() {
		t.Fatal("l must open the channel filter menu")
	}
	for i := 0; i < len(m.list.prefs.menuItems); i++ {
		if item := m.list.prefs.menuItems[m.list.prefs.menuIndex]; item.Kind == filterMenuLang && item.Language == "pt" {
			break
		}
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if item := m.list.prefs.menuItems[m.list.prefs.menuIndex]; item.Kind != filterMenuLang || item.Language != "pt" {
		t.Fatalf("failed to highlight the PT row: %+v", item)
	}
	before := m.View()
	plain, _ := pressKey(m, tea.KeyMsg{Type: tea.KeySpace})
	alt, _ := pressKey(m, tea.KeyMsg{Type: tea.KeySpace, Alt: true})
	if plain.View() == before {
		t.Fatal("Space did not toggle the menu item")
	}
	if alt.View() != plain.View() {
		t.Fatalf("Alt+Space differs from Space:\n%s\n---\n%s", alt.View(), plain.View())
	}
}

func TestAltTabTypeSwitchesFocusAndKeepsTheDraft(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 14})
	m = updated.(Model)
	altTab := tea.KeyMsg{Type: tea.KeyTab, Alt: true}

	m, _ = pressKey(m, altTab)
	if m.Focus() != FocusChat {
		t.Fatalf("Focus = %q after Alt+Tab, want %q", m.Focus(), FocusChat)
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	for _, r := range "oi" {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = pressKey(m, altTab)
	if m.Focus() != FocusList {
		t.Fatalf("Focus = %q after Alt+Tab while composing, want %q", m.Focus(), FocusList)
	}
	if m.ComposeDraft() != "oi" {
		t.Fatalf("ComposeDraft = %q, want %q", m.ComposeDraft(), "oi")
	}
}

func TestAltCtrlCShowsTheHint(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlC, Alt: true})
	if m.Quitting() || cmdQuits(cmd) {
		t.Fatal("Alt+Ctrl+C quit the app")
	}
	if want := m.t("status.ctrl_c_hint"); m.CtrlCNotice() != want {
		t.Fatalf("CtrlCNotice() = %q, want %q", m.CtrlCNotice(), want)
	}
}
