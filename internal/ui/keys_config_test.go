package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestResolveKeyOverridesDesconhecidoInvalidoConflitoEImprimivel(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][]string{
		"action.nao_existe":  {"ctrl+b"},
		"action.toggle_list": {"ctrl+b", "nao-e-tecla"},
		"action.toggle_chat": {"x"}, // imprimível em modo global
		"action.mute":        {"f"}, // f já é favorito no modo normal
	}
	overrides, notices := ResolveKeyOverrides(true, raw, cat)
	if _, ok := overrides[actionID("action.nao_existe")]; ok {
		t.Fatal("ação desconhecida entrou nos overrides")
	}
	// ctrl+b vale; a inválida foi descartada. Sem conflito no classic (ctrl+b livre).
	if got := overrides[actToggleList]; !slices.Equal(got, []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v, quer [ctrl+b]", got)
	}
	if _, ok := overrides[actToggleChat]; ok {
		t.Fatal("tecla imprimível em ação global não deve aplicar")
	}
	if _, ok := overrides[actMute]; ok {
		t.Fatal("conflito com favorito deveria manter o padrão do mute")
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{"action.nao_existe", "nao-e-tecla", "x", "action.mute"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("aviso sem %q:\n%s", want, joined)
		}
	}
}

func TestResolveKeyOverridesListaVaziaDesliga(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"action.help": {},
	}, cat)
	if got := overrides[actHelp]; got == nil || len(got) != 0 {
		t.Fatalf("help = %#v, quer desligada", got)
	}
	if len(notices) != 0 {
		t.Fatalf("avisos inesperados: %v", notices)
	}
	reg, _ := EffectiveKeymap(false, map[string][]string{"action.help": {}}, cat)
	if reg.actionFor(modeNormal, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}) != "" {
		t.Fatal("? ainda dispara a ajuda desligada")
	}
}

// Com action.toggle_list = ["ctrl+b"], a ajuda e o rodapé mostram Ctrl+B e a
// tecla esconde a lista (mapa classic, onde o padrão é Ctrl+H).
func TestCenarioTeclaCustomToggleList(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"action.toggle_list": {"ctrl+b"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}

	m, _ := newScenarioClassic(t, 120, 40, WithKeymapOverrides(true, overrides, ""))
	if !slices.Equal(m.keys.keys[actToggleList], []string{"ctrl+b"}) {
		t.Fatalf("teclas efetivas = %v", m.keys.keys[actToggleList])
	}

	// Abre um canal para o modo foco existir (sem chat, a lista não some).
	m = play(t, m, "enter enter")

	m = play(t, m, "?")
	// A ajuda abre na seção da lista; Tab leva até a das janelas, onde está a ação.
	for i := 0; i < len(helpSections) && m.layout.helpSection != sectionWindow; i++ {
		m = play(t, m, "tab")
	}
	if m.layout.helpSection != sectionWindow {
		t.Fatalf("seção da ajuda = %v, quer janelas", m.layout.helpSection)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Ctrl+B") {
		t.Fatalf("ajuda sem Ctrl+B:\n%s", view)
	}
	m = play(t, m, "q")

	// Modo foco: o rodapé cita a tecla da lista.
	for i := 0; i < 4 && m.LayoutMode() != LayoutFocus; i++ {
		m = play(t, m, "ctrl+w")
	}
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("layout = %v, quer foco", m.LayoutMode())
	}
	footer := m.Footer()
	if !strings.Contains(footer, "Ctrl+B") {
		t.Fatalf("rodapé sem Ctrl+B: %q", footer)
	}

	// Volta ao split e esconde a lista com Ctrl+B.
	for i := 0; i < 4 && m.LayoutMode() != LayoutSplit; i++ {
		m = play(t, m, "ctrl+w")
	}
	if !m.layout.showList {
		t.Fatal("lista já escondida antes de Ctrl+B")
	}
	m = play(t, m, "ctrl+b")
	if m.layout.showList {
		t.Fatal("Ctrl+B não escondeu a lista")
	}
}
