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
	// defaultOnly marca o modo que só o mapa padrão tem: o popup do prefixo e o
	// aviso da migração.
	defaultOnly bool
}

// openGaulesWithChat abre gaules pela lista de canais e põe duas mensagens no chat.
func openGaulesWithChat(t *testing.T, m Model) Model {
	m = play(t, m, "enter enter")
	return chatLines(t, m, [2]string{"Fulano", "salve, chat"}, [2]string{"Ciclana", "que jogada"})
}

// focusChat leva o foco ao chat pelo caminho que a largura oferece: Tab ao
// lado da lista e, abaixo de 80 colunas, a tecla do chat (Ctrl+W l no mapa
// padrão, Ctrl+L no clássico), que abre só o chat.
func focusChat(t *testing.T, m Model) Model {
	switch {
	case m.width >= minSplitColumns:
		return play(t, m, "tab")
	case m.classic:
		return play(t, m, "ctrl+l")
	}
	return play(t, m, "ctrl+w l")
}

var uxModes = []uxMode{
	{name: "categorias", reach: func(t *testing.T, m Model) Model { return m }, focusVisible: highlightOnScreen},
	{name: "canais", reach: func(t *testing.T, m Model) Model { return play(t, m, "enter") }, focusVisible: highlightOnScreen},
	{name: "prompt de busca", reach: func(t *testing.T, m Model) Model { return play(t, m, "ctrl+f g a u") }, focusVisible: textOnScreen("gau")},
	{name: "busca", reach: func(t *testing.T, m Model) Model { return play(t, m, "ctrl+f gaules enter") }, focusVisible: highlightOnScreen},
	{name: "filtro", reach: func(t *testing.T, m Model) Model { return play(t, m, "enter / g a") }, focusVisible: func(m Model, rows []string) bool {
		return textOnScreen("ga")(m, rows) && highlightOnScreen(m, rows)
	}},
	{name: "seletor rápido", reach: func(t *testing.T, m Model) Model { return play(t, m, "ctrl+k g a u") }, focusVisible: textOnScreen("gau")},
	{name: "chat", reach: func(t *testing.T, m Model) Model { return focusChat(t, openGaulesWithChat(t, m)) }, focusVisible: textOnScreen("▌ ")},
	{name: "escrita", reach: func(t *testing.T, m Model) Model {
		return play(t, focusChat(t, openGaulesWithChat(t, m)), "i o i")
	}, focusVisible: textOnScreen("oi" + composeCursor)},
	{name: "ajuda", reach: func(t *testing.T, m Model) Model { return play(t, m, "?") }, focusVisible: func(m Model, rows []string) bool {
		return textOnScreen(m.t("help.title"))(m, rows)
	}},
	// O popup aparece depois da espera, com o canal aberto, por cima da lista e do chat.
	{name: "popup do prefixo", reach: func(t *testing.T, m Model) Model {
		m = play(t, openGaulesWithChat(t, m), "ctrl+w")
		return feed(t, m, prefixPopupMsg{m.prefixGeneration})
	}, focusVisible: func(m Model, rows []string) bool {
		return m.prefixVisible && textOnScreen(keyLabel("ctrl+w h", m.catalog))(m, rows) && textOnScreen(m.t("keymap.cancel"))(m, rows)
	}, defaultOnly: true},
	// O aviso da migração, que a abertura mostra uma vez só, com o caminho de volta.
	{name: "aviso da migração", reach: func(t *testing.T, m Model) Model {
		m.keymapNotice = fmt.Sprintf(CatalogText(m.catalog, "keymap.migration"), "/fixture/config.toml")
		return m
	}, focusVisible: func(m Model, rows []string) bool {
		return textOnScreen(`keymap = "classic"`)(m, rows) && highlightOnScreen(m, rows)
	}, defaultOnly: true},
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

// As regras valem nos dois mapas: no padrão, com o popup do prefixo e o aviso
// da migração, e no clássico, que keymap = "classic" restaura.
func TestInvariantesDeTelaEmCadaLarguraEModo(t *testing.T) {
	keymaps := []struct {
		name    string
		classic bool
	}{{"padrão", false}, {"classic", true}}
	for _, keymap := range keymaps {
		for _, width := range invariantWidths {
			for _, mode := range uxModes {
				if mode.defaultOnly && keymap.classic {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%d", keymap.name, mode.name, width), func(t *testing.T) {
					checkScreenInvariants(t, keymap.classic, width, mode)
				})
			}
		}
	}
}

func checkScreenInvariants(t *testing.T, classic bool, width int, mode uxMode) {
	t.Helper()
	m, _ := newScenario(t, width, invariantHeight, WithKeymap(classic, ""))
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
}

// O seletor aberto respeita largura e altura em terminais baixos, inclusive 3 e 5 linhas.
func TestInvariantesSeletorRapidoEmAlturasBaixas(t *testing.T) {
	for _, height := range []int{3, 5, 8, 12, 24} {
		for _, width := range []int{60, 80, 120} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m, _ := newScenario(t, width, height)
				m = play(t, m, "ctrl+k g a u")
				rows := strings.Split(ansi.Strip(m.View()), "\n")
				if len(rows) != height {
					t.Fatalf("tela com %d linhas em terminal %d", len(rows), height)
				}
				for i, row := range rows {
					if got := ansi.StringWidth(row); got > width {
						t.Fatalf("linha %d tem %d colunas em %d: %q", i+1, got, width, row)
					}
				}
				if !m.list.quickOpen.active {
					t.Fatal("seletor não ficou aberto")
				}
				overlay := m.renderQuickOpenLines(width)
				available := height - 1 - m.footerRows()
				if available > 0 && len(overlay) > available {
					t.Fatalf("overlay %d > available %d", len(overlay), available)
				}
			})
		}
	}
}
