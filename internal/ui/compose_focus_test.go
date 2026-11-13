package ui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestComposeFocusHostedListReturn(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m.hosted.host = &recordingHost{}
	m = resize(m, 212, 49)
	m = sendKeys(m, keyI)
	m = typeRunes(m, "oi")
	// Na escrita o Ctrl+H é do campo: sair dela com Esc devolve a tecla ao layout.
	m = sendKeys(m, keyEsc, keyCtrlH, keyCtrlH)
	if m.Focus() != FocusList || m.Composing() {
		t.Errorf("after Esc and Ctrl+H twice: focus=%q composing=%v", m.Focus(), m.Composing())
	}
	before := m.HighlightedID()
	m = typeRunes(m, "j")
	if m.HighlightedID() == before {
		t.Error("j did not move the list selection")
	}
	m = typeRunes(m, "k")
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fc.sent) != 0 || m.ComposeDraft() != "oi" {
		t.Fatalf("after Enter: sent=%v draft=%q", fc.sent, m.ComposeDraft())
	}
	m = sendKeys(m, keyI)
	if !m.Composing() || m.Focus() != FocusChat || m.ComposeDraft() != "oi" {
		t.Fatalf("i did not resume draft: focus=%q composing=%v draft=%q", m.Focus(), m.Composing(), m.ComposeDraft())
	}
}

func TestComposeFocusNarrowListInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"filter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}},
		{"search", tea.KeyMsg{Type: tea.KeyCtrlF}},
		{"channel_filter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, fc := setupTwoPanelModel(t)
			m = resize(m, 80, 24)
			m = sendKeys(m, keyI)
			m = typeRunes(m, "oi")
			m = resize(m, 79, 24)
			m = sendKeys(m, tc.key)
			m = resize(m, 80, 24)
			if m.Focus() != FocusList || m.Composing() {
				t.Errorf("after widening: focus=%q composing=%v", m.Focus(), m.Composing())
			}
			// Close the list input so movement and Enter belong to the list.
			m = sendKeys(m, keyEsc)
			before := m.HighlightedID()
			m = typeRunes(m, "j")
			if m.HighlightedID() == before {
				t.Error("j did not move the list selection")
			}
			m = typeRunes(m, "k")
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
			if len(fc.sent) != 0 || m.ComposeDraft() != "oi" {
				t.Fatalf("after Enter: sent=%v draft=%q", fc.sent, m.ComposeDraft())
			}
		})
	}
}

func TestComposeFocusNarrowResumesDraft(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	m = sendKeys(m, keyI)
	m = typeRunes(m, "oi")
	m = resize(m, 79, 24)
	m = typeRunes(m, "/")
	m = resize(m, 80, 24)
	m = sendKeys(m, keyEsc, keyI)
	if !m.Composing() || m.Focus() != FocusChat || m.ComposeDraft() != "oi" {
		t.Fatalf("i did not resume draft: focus=%q composing=%v draft=%q", m.Focus(), m.Composing(), m.ComposeDraft())
	}
}

func TestComposeFocusDispatchRequiresChat(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	// A stale composing flag must not let chat consume list keys.
	m.chat.composing = true
	m.chat.composeDraft = "oi"
	m.layout.focus = FocusList
	before := m.HighlightedID()
	m = typeRunes(m, "j")
	if m.HighlightedID() == before {
		t.Error("j did not move the list selection")
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fc.sent) != 0 || m.ComposeDraft() != "oi" {
		t.Fatalf("after Enter: sent=%v draft=%q", fc.sent, m.ComposeDraft())
	}
}

// The model is already initialized by setupTwoPanelModel.
type composeFocusRuntime struct{ Model }

func (m composeFocusRuntime) Init() tea.Cmd { return nil }

func (m composeFocusRuntime) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	return composeFocusRuntime{next.(Model)}, cmd
}

func TestComposeFocusRuntime(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m.hosted.host = &recordingHost{}
	m = resize(m, 212, 49)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := tea.NewProgram(composeFocusRuntime{m}, tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("ioi\x08x ab\x17\x11")),
		tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	result, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	final := result.(composeFocusRuntime).Model
	// O leitor do Bubble Tea entrega 0x08 como Ctrl+H e 0x17 como Ctrl+W: na
	// escrita, o primeiro não faz nada e o segundo apaga a palavra.
	if len(fc.sent) != 0 || final.ComposeDraft() != "oix " || final.Focus() != FocusChat || !final.Composing() || !final.listVisible() {
		t.Fatalf("sent=%v draft=%q focus=%q composing=%v list=%v", fc.sent, final.ComposeDraft(), final.Focus(), final.Composing(), final.listVisible())
	}
}
