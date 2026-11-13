package ui

// Invariantes de tela que valem em toda largura e em todo modo principal. Um
// golden congela uma tela; estas regras pegam o que nenhum golden cobre: a
// largura que ninguém gravou.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// invariantWidths cobre abaixo do mínimo (60), a borda dele (79 e 80) e dois
// terminais largos.
var invariantWidths = []int{60, 79, 80, 100, 120}

const invariantHeight = 24

// uxMode é um modo principal da tela: como o usuário chega nele e onde está o
// foco, que tem de aparecer.
type uxMode struct {
	name string
	// reach leva o modelo recém-aberto ao modo.
	reach func(t *testing.T, m Model) Model
	// focusVisible diz se a tela mostra onde está o foco: a linha destacada, o
	// cursor de texto ou o título da ajuda.
	focusVisible func(m Model, rows []string) bool
}

// openGaulesWithChat abre gaules pela lista de canais e põe duas mensagens no chat.
func openGaulesWithChat(t *testing.T, m Model) Model {
	m = play(t, m, "enter enter")
	return chatLines(t, m, [2]string{"Fulano", "salve, chat"}, [2]string{"Ciclana", "que jogada"})
}

// focusChat leva o foco ao chat pelo caminho que a largura oferece: Tab ao
// lado da lista e, abaixo de 80 colunas, Ctrl+L, que abre só o chat.
func focusChat(t *testing.T, m Model) Model {
	if m.width < minSplitColumns {
		return play(t, m, "ctrl+l")
	}
	return play(t, m, "tab")
}

var uxModes = []uxMode{
	{name: "categorias", reach: func(t *testing.T, m Model) Model { return m }, focusVisible: highlightOnScreen},
	{name: "canais", reach: func(t *testing.T, m Model) Model { return play(t, m, "enter") }, focusVisible: highlightOnScreen},
	{name: "prompt de busca", reach: func(t *testing.T, m Model) Model { return play(t, m, "ctrl+f g a u") }, focusVisible: textOnScreen("gau")},
	{name: "busca", reach: func(t *testing.T, m Model) Model { return play(t, m, "ctrl+f gaules enter") }, focusVisible: highlightOnScreen},
	{name: "filtro", reach: func(t *testing.T, m Model) Model { return play(t, m, "enter / g a") }, focusVisible: func(m Model, rows []string) bool {
		return textOnScreen("ga")(m, rows) && highlightOnScreen(m, rows)
	}},
	{name: "chat", reach: func(t *testing.T, m Model) Model { return focusChat(t, openGaulesWithChat(t, m)) }, focusVisible: textOnScreen("▌ ")},
	{name: "escrita", reach: func(t *testing.T, m Model) Model {
		return play(t, focusChat(t, openGaulesWithChat(t, m)), "i o i")
	}, focusVisible: textOnScreen("oi" + composeCursor)},
	{name: "ajuda", reach: func(t *testing.T, m Model) Model { return play(t, m, "?") }, focusVisible: func(m Model, rows []string) bool {
		return textOnScreen(m.t("help.title"))(m, rows)
	}},
}

// highlightOnScreen diz se a linha destacada da lista está na tela, com o
// marcador ▸ e o nome do item.
func highlightOnScreen(m Model, rows []string) bool {
	it, ok := m.HighlightedItem()
	if !ok {
		return false
	}
	var name string
	switch {
	case it.Channel != nil:
		name = it.Channel.DisplayName
	case it.Category != nil:
		name = it.Category.Name
	}
	for _, row := range rows {
		if strings.Contains(row, "▸") && strings.Contains(row, name) {
			return true
		}
	}
	return false
}

func textOnScreen(text string) func(Model, []string) bool {
	return func(_ Model, rows []string) bool {
		for _, row := range rows {
			if strings.Contains(row, text) {
				return true
			}
		}
		return false
	}
}

func TestInvariantesDeTelaEmCadaLarguraEModo(t *testing.T) {
	for _, width := range invariantWidths {
		for _, mode := range uxModes {
			t.Run(fmt.Sprintf("%s/%d", mode.name, width), func(t *testing.T) {
				m, _ := newScenario(t, width, invariantHeight)
				m = mode.reach(t, m)
				rows := strings.Split(ansi.Strip(m.View()), "\n")
				dump := strings.Join(rows, "\n")

				if len(rows) != invariantHeight {
					t.Errorf("a tela tem %d linhas, o terminal %d:\n%s", len(rows), invariantHeight, dump)
				}
				for i, row := range rows {
					if got := ansi.StringWidth(row); got > width {
						t.Errorf("linha %d tem %d colunas em %d: %q", i+1, got, width, row)
					}
				}
				if !mode.focusVisible(m, rows) {
					t.Errorf("o foco do modo %s não aparece na tela:\n%s", mode.name, dump)
				}
				if hint := m.t("status.help_hint"); !strings.Contains(rows[len(rows)-1], hint) {
					t.Errorf("o rodapé não mostra %q: %q", hint, rows[len(rows)-1])
				}
			})
		}
	}
}
