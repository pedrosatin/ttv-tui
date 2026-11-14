package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestCenarioRodapePorModo(t *testing.T) {
	for _, tc := range []struct{ name, keys string }{
		{"categorias", ""}, {"canais", "enter"}, {"busca", "ctrl+f gau"},
		{"resultados", "ctrl+f gaules enter"}, {"filtro", "/ gau"},
		{"chat", "ctrl+f gaules enter enter tab"},
		{"escrevendo", "ctrl+f gaules enter enter i"},
		{"menu", "l"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachSize(t, "rodape-"+tc.name, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m = play(t, m, tc.keys)
				if tc.name == "escrevendo" && !m.chat.composing {
					t.Fatal("não entrou na escrita")
				}
				if !strings.Contains(m.Footer(), "ajuda") {
					t.Fatal("rodapé sem ajuda")
				}
				return m
			})
		})
	}
	eachSize(t, "rodape-janela", func(t *testing.T, w, h int) Model {
		m, _ := hostedScenario(t, w, h)
		return m
	})
}

func TestCenarioTeclasSemEfeitoAvisam(t *testing.T) {
	for _, tc := range []struct{ keys, want string }{
		{"i", "Abra um canal"}, {"tab", "Abra um canal"}, {"ctrl+t", "Abra um canal"},
		{"ctrl+o", "Sobreposição só na janela"},
		{"ctrl+f gaules enter enter tab f", "Tab para favoritar na lista"},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24)
			m = play(t, m, tc.keys)
			if !strings.Contains(ansi.Strip(m.StatusBar()), tc.want) {
				t.Fatalf("barra %q sem %q", ansi.Strip(m.StatusBar()), tc.want)
			}
		})
	}
}

func TestCenarioFavoritoSobreErro(t *testing.T) {
	eachSize(t, "favorito-sobre-erro", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h, WithStore(&feedbackStore{}))
		m = play(t, m, "ctrl+f gaules enter")
		m.player.state, m.player.failure = PlayerStateError, PlayerFailureNetwork
		m = play(t, m, "f")
		if !strings.Contains(ansi.Strip(m.StatusBar()), "★ Gaules nos favoritos") {
			t.Fatalf("aviso escondido: %s", ansi.Strip(m.StatusBar()))
		}
		m = play(t, m, "down")
		if !strings.Contains(ansi.Strip(m.StatusBar()), m.playerFailureText()) {
			t.Fatal("o erro não voltou depois do aviso")
		}
		return play(t, m, "up f f")
	})
}

func TestRodapeLarguraEMapaEfetivo(t *testing.T) {
	for _, width := range []int{60, 79, 80, 100, 120} {
		m, _ := newScenario(t, width, 24)
		if got := ansi.StringWidth(m.Footer()); got > width {
			t.Fatalf("rodapé com %d colunas em %d", got, width)
		}
		if !strings.Contains(m.Footer(), "? ajuda") {
			t.Fatal("ajuda sumiu")
		}
		m.keys = newKeyRegistry(defaultActions(), map[actionID][]string{actFavorite: {"z"}, actFilter: {}})
		if !strings.Contains(m.Footer(), "z ★") || strings.Contains(m.Footer(), "/ filtrar") {
			t.Fatal(m.Footer())
		}
	}
}

func TestBarraNaoRepeteCanalAntigo(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+f gaules enter enter")
	m.player.state = PlayerStateNone
	if strings.Contains(ansi.Strip(m.StatusBar()), "Canal selecionado") {
		t.Fatal(m.StatusBar())
	}
}

func TestCenarioRodapeEmIngles(t *testing.T) {
	for _, tc := range []struct{ name, keys string }{
		{"categorias", ""}, {"canais", "enter"}, {"busca", "ctrl+f gau"}, {"filtro", "/ gau"},
		{"chat", "ctrl+f gaules enter enter tab"}, {"escrevendo", "ctrl+f gaules enter enter i"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachSize(t, "rodape-en-"+tc.name, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m.catalog = mustCatalog(t, i18n.LangEN)
				return play(t, m, tc.keys)
			})
		})
	}
}
