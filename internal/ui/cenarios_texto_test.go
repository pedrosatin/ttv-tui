package ui

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// O usuário copia "❤ Live Music" da tela inicial, onde a tela tira o VS16 do
// nome da categoria, e digita no filtro: a categoria aparece (ticket 26).
func TestCenarioFiltroComTextoDaTela(t *testing.T) {
	eachSize(t, "filtro-com-texto-da-tela", func(t *testing.T, width, height int) Model {
		m, _ := newScenarioClassic(t, width, height)
		m.list.categories = append(m.list.categories, catalog.Category{ID: "cat-music", Name: "❤️ Live Music", Viewers: catalog.IntPtr(9000)})
		m = play(t, m, "/ ❤ space live")

		if it, ok := m.HighlightedItem(); !ok || it.Category == nil || it.Category.ID != "cat-music" {
			t.Fatalf("destaque = %q, quer a categoria cat-music:\n%s", m.HighlightedID(), screen(m))
		}
		return m
	})
}

// Na tela inicial, um favorito de nome feito só de U+3164 aparece pelo login e
// uma categoria de nome só de U+3164 aparece como "(sem nome)", e nenhuma das
// duas linhas fica vazia (ticket 26).
func TestCenarioNomeSoDePreenchimento(t *testing.T) {
	eachSize(t, "nome-so-de-preenchimento", func(t *testing.T, width, height int) Model {
		fav := catalog.Channel{ID: "ch-fantasma", Login: "fantasma", DisplayName: "\u3164\u3164\u3164", Availability: catalog.AvailabilityOffline}
		m, _ := newScenarioClassic(t, width, height, WithFavorites([]catalog.Channel{fav}))
		m.list.categories = append(m.list.categories, catalog.Category{ID: "cat-vazia", Name: "\u3164\u3164", Viewers: catalog.IntPtr(900)})

		if lineWith(m, "fantasma") < 0 || lineWith(m, "(sem nome)") < 0 || lineWith(m, "\u3164") >= 0 {
			t.Fatalf("o favorito e a categoria sem nome visível precisam do login e de (sem nome):\n%s", screen(m))
		}
		return m
	})
}
