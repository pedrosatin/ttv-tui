package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// O usuário copia "❤ Live Music" da tela inicial, que tira o VS16 do nome da
// categoria, digita esse texto no filtro e a categoria aparece.
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

// A tela inicial mostra pelo login um favorito de nome feito só de U+3164 e
// mostra como "(sem nome)" uma categoria com o mesmo tipo de nome, e nenhuma
// das duas linhas fica vazia.
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

// Quando o usuário abre uma categoria de nome só de U+3164, o cabeçalho e o
// título da seção mostram "(sem nome)", e o canal de título só de U+3164
// aparece sem o separador do título.
func TestCenarioCategoriaSoDePreenchimentoAberta(t *testing.T) {
	eachSize(t, "categoria-so-de-preenchimento-aberta", func(t *testing.T, width, height int) Model {
		m, _ := newScenarioClassic(t, width, height)
		m.list.categories = append(m.list.categories, catalog.Category{ID: "cat-vazia", Name: "\u3164\u3164", Viewers: catalog.IntPtr(900)})
		src := m.browser.(*fakeSource)
		src.channelsFn = func(context.Context, string, catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch-fantasma", Login: "fantasma", DisplayName: "Fantasma", Viewers: catalog.IntPtr(300), Title: catalog.StringPtr("\u3164\u3164"), Availability: catalog.AvailabilityLive},
			}}, nil
		}
		m = play(t, m, "G")
		if it, ok := m.HighlightedItem(); !ok || it.Category == nil || it.Category.ID != "cat-vazia" {
			t.Fatalf("destaque = %q, quer a categoria cat-vazia:\n%s", m.HighlightedID(), screen(m))
		}
		m = play(t, m, "enter")

		if m.Mode() != ModeChannels || lineWith(m, "(sem nome)") < 0 || lineWith(m, "\u3164") >= 0 {
			t.Fatalf("a categoria aberta precisa de (sem nome) no cabeçalho:\n%s", screen(m))
		}
		if l := lineWith(m, "Fantasma"); l < 0 || strings.Contains(screenLines(m)[l], listRowEmDash) {
			t.Fatalf("o canal de título vazio não pode ter separador:\n%s", screen(m))
		}
		return m
	})
}
