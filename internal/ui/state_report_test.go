package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Sem WithStateReport o modelo não monta nem conta nada.
func TestRelatorioDeEstadoDesligadoPorPadrao(t *testing.T) {
	m := New(nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if got := next.(Model); got.keysSeen != 0 || got.reported != (StateReport{}) {
		t.Fatalf("modelo sem relatório contou %d teclas e guardou %+v", got.keysSeen, got.reported)
	}
}

// O relatório sai quando o estado muda, conta uma tecla por caractere e não se
// repete quando nada mudou.
func TestRelatorioDeEstadoSoQuandoMuda(t *testing.T) {
	var got []StateReport
	m := New(nil, WithStateReport(func(r StateReport) { got = append(got, r) }))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	next, _ = next.(Model).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if len(got) != 1 || got[0].Width != 100 || got[0].Mode != string(ModeCategories) || got[0].Layout != string(LayoutSplit) {
		t.Fatalf("relatórios depois de dois tamanhos iguais: %+v", got)
	}
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if last := got[len(got)-1]; last.Keys != 3 {
		t.Fatalf("Keys = %d depois de \"ab\" e Esc, quer 3", last.Keys)
	}
	_ = next
}
