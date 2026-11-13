package ui

// Cenários de UX de referência: o que o usuário faz (teclas) e o que ele vê (a
// tela inteira, num golden). Cada um é gravado em 80x24 e 120x40. Para escrever
// um novo, veja docs/cenarios-de-ux.md.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// eachSize roda o cenário em cada tamanho de terminal e grava o golden
// <name>-<largura>x<altura>.
func eachSize(t *testing.T, name string, run func(t *testing.T, width, height int) Model) {
	t.Helper()
	for _, size := range scenarioSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := run(t, size.width, size.height)
			assertGolden(t, fmt.Sprintf("%s-%dx%d", name, size.width, size.height), screen(m))
		})
	}
}

// O usuário abre o app, busca "gaules" com Ctrl+F e abre o primeiro resultado:
// o vídeo começa, o chat da sala aparece ao lado e a lista guarda a busca.
func TestCenarioBuscaEAbreCanal(t *testing.T) {
	eachSize(t, "busca-e-abre-canal", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas = %q, quer só \"gaules\"", w.searched)
		}
		if !slices.Equal(w.playback.started, []string{"gaules"}) {
			t.Fatalf("player abriu %q, quer gaules", w.playback.started)
		}
		if !slices.Equal(w.chat.joined, []string{"gaules"}) {
			t.Fatalf("chat entrou em %q, quer gaules", w.chat.joined)
		}

		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		return chatLines(t, m,
			[2]string{"Fulano", "salve, chat"},
			[2]string{"Ciclana", "que jogada"},
		)
	})
}

// Com gaules nos favoritos, o usuário digita "/gau" na tela inicial: o filtro
// deixa só o favorito, que fica destacado.
func TestCenarioFiltroComFavorito(t *testing.T) {
	eachSize(t, "filtro-com-favorito", func(t *testing.T, width, height int) Model {
		fav := scenarioChannels()[0]
		m, w := newScenario(t, width, height, WithFavorites([]catalog.Channel{fav}))
		m = play(t, m, "/ g a u")

		if len(w.searched) != 0 {
			t.Fatalf("o filtro buscou %q na plataforma; ele só filtra o que está na tela", w.searched)
		}
		if it, ok := m.HighlightedItem(); !ok || it.Channel == nil || it.Channel.Login != "gaules" {
			t.Fatalf("destaque = %q, quer o favorito gaules", m.HighlightedID())
		}
		return m
	})
}

// O usuário aperta ? na tela inicial: a ajuda abre embaixo da lista, com o
// título e as teclas da lista à vista.
func TestCenarioAjudaAberta(t *testing.T) {
	eachSize(t, "ajuda-aberta", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "?")

		view := ansi.Strip(m.View())
		if !strings.Contains(view, m.t("help.title")) {
			t.Fatalf("a ajuda não mostra o título %q:\n%s", m.t("help.title"), view)
		}
		return m
	})
}
