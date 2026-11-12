package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChannelSwitchPreservesDraftPerChannel(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m = resize(m, 120, 24)

	// 1. Inicia composição em A (gaules) e digita mensagem.
	m = sendKeys(m, keyTab, keyI)
	m = typeRunes(m, "mensagem do gaules")
	if m.ComposeDraft() != "mensagem do gaules" {
		t.Fatalf("draft em A = %q, want %q", m.ComposeDraft(), "mensagem do gaules")
	}

	// 2. Troca para o canal B (alanzoka) via seleção na lista.
	m = pressTab(m) // sai de compose para a lista
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if id := m.HighlightedID(); id != "ch-alan" {
		t.Fatalf("HighlightedID = %q, want ch-alan", id)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Enter na lista precisa emitir comando de seleção")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.SelectedChannel() == nil || m.SelectedChannel().Login != "alanzoka" {
		t.Fatalf("SelectedChannel = %+v, want alanzoka", m.SelectedChannel())
	}

	// 3. Confere campo vazio em B e que Enter não envia o texto de A em B.
	if m.ComposeDraft() != "" {
		t.Fatalf("draft ao entrar em B = %q, want vazio", m.ComposeDraft())
	}
	fc.sent = nil
	m = pressTab(m) // lista -> chat
	updated, sendCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if sendCmd != nil {
		updated, _ = m.Update(sendCmd())
		m = updated.(Model)
	}
	if len(fc.sent) != 0 {
		t.Fatalf("Enter em B enviou mensagem de A: %+v", fc.sent)
	}

	// 4. Volta ao canal A (gaules) e confere o rascunho de volta.
	m = pressTab(m) // volta para a lista
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if id := m.HighlightedID(); id != "ch-gau" {
		t.Fatalf("HighlightedID = %q, want ch-gau", id)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Enter na lista precisa emitir comando de seleção")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.SelectedChannel() == nil || m.SelectedChannel().Login != "gaules" {
		t.Fatalf("SelectedChannel = %+v, want gaules", m.SelectedChannel())
	}

	if m.ComposeDraft() != "mensagem do gaules" {
		t.Fatalf("ao voltar a A, draft = %q, want %q", m.ComposeDraft(), "mensagem do gaules")
	}
}
