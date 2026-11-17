package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// homeCheia são as opções de uma tela inicial com muitos itens salvos: 5
// categorias fixadas, 10 favoritos e 20 recentes.
func homeCheia() []Option {
	_, recs := savedForScenario()
	var favs []catalog.Channel
	for i := 1; i <= 10; i++ {
		favs = append(favs, catalog.Channel{ID: fmt.Sprintf("ch-fav%02d", i), Login: fmt.Sprintf("favorito%02d", i)})
	}
	var pinned []store.Category
	for _, name := range []string{"Minecraft", "Grand Theft Auto V", "League of Legends", "Fortnite", "Dota 2"} {
		pinned = append(pinned, store.Category{Platform: "twitch", ID: "pin-" + strings.ToLower(strings.ReplaceAll(name, " ", "-")), Name: name})
	}
	return []Option{WithFavoriteCategories(pinned), WithFavorites(favs), WithRecents(recs)}
}

// footerLine é a linha de dicas, logo acima da barra de status.
func footerLine(m Model) string {
	rows := strings.Split(strings.TrimSuffix(screen(m), "\n"), "\n")
	return rows[len(rows)-2]
}

// catalogoNaTela diz se a primeira categoria ao vivo do cenário está na tela.
func catalogoNaTela(m Model) bool {
	return lineWith(m, "Just Chatting") >= 0
}

// Com 5 categorias fixadas, 10 favoritos e 20 recentes, a tela inicial em 80x24
// já não tem espaço para o catálogo ao vivo; em 120x40 ele ainda cabe, e o
// rodapé fica como sempre foi. Com os 20 recentes à mostra, o catálogo sai da
// tela nos dois tamanhos, olhando do topo: o rodapé oferece g c no lugar da
// busca, que o filtro ainda cita ao ser aberto.
func TestCenarioInicioCheioMostraCaminhoParaCategorias(t *testing.T) {
	eachSize(t, "inicio-cheio-caminho-categorias", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height, homeCheia()...)
		hint := "g c " + m.t("footer.categories")
		check := func(m Model, below bool) {
			t.Helper()
			if m.liveCatalogBelow() != below || catalogoNaTela(m) == below {
				t.Fatalf("liveCatalogBelow = %v e catálogo na tela = %v; quer o catálogo fora = %v:\n%s", m.liveCatalogBelow(), catalogoNaTela(m), below, screen(m))
			}
			if got := strings.Contains(footerLine(m), hint); got != below {
				t.Fatalf("rodapé com %q = %v; quer %v: %q", hint, got, below, footerLine(m))
			}
		}
		check(m, width == 80)

		// g r vai ao primeiro recente; a sétima linha dali é o "mais…". Depois
		// dele, g g volta ao topo da tela inicial.
		m = play(t, m, "g r"+strings.Repeat(" down", homeRecentsLimit)+" enter g g")
		if !m.list.recentsExpanded {
			t.Fatal("preparação: o \"mais…\" não mostrou os recentes")
		}
		if hi, ok := m.HighlightedItem(); !ok || hi.Type != ItemTypePinnedCategory {
			t.Fatalf("preparação: destaque %+v, quer a primeira categoria fixada", hi)
		}
		check(m, true)
		if strings.Contains(footerLine(m), "Ctrl+F") {
			t.Fatalf("a dica de categorias entra no lugar da busca: %q", footerLine(m))
		}
		return m
	})
}

// Na mesma tela inicial cheia, g c leva o destaque à primeira categoria ao
// vivo, que aparece na tela com o cabeçalho da seção, e a dica sai do rodapé.
// As seções salvas continuam inteiras acima, ao alcance de g g, g f e g r.
func TestCenarioGCVaiAoCatalogoAoVivo(t *testing.T) {
	eachSize(t, "inicio-cheio-g-c", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height, homeCheia()...)
		before := m.visibleItemIDs()
		m = play(t, m, "g c")
		if hi, ok := m.HighlightedCategory(); !ok || hi.ID != "cat-jc" {
			t.Fatalf("destaque = %+v, quer Just Chatting, a primeira categoria ao vivo:\n%s", hi, screen(m))
		}
		if line := highlightedLine(m); line < 0 || !strings.Contains(screenLines(m)[line], "Just Chatting") {
			t.Fatalf("a primeira categoria ao vivo não está à vista:\n%s", screen(m))
		}
		if lineWith(m, "── "+m.t("categories.title")+" ──") < 0 {
			t.Fatalf("o cabeçalho do catálogo ao vivo não está na tela:\n%s", screen(m))
		}
		if strings.Contains(footerLine(m), "g c "+m.t("footer.categories")) {
			t.Fatalf("com o catálogo à vista, o rodapé ainda oferece g c: %q", footerLine(m))
		}
		if got := m.visibleItemIDs(); strings.Join(got, ",") != strings.Join(before, ",") {
			t.Fatalf("g c mudou os itens da tela inicial: %q, era %q", got, before)
		}

		m = play(t, m, "g f")
		if hi, ok := m.HighlightedItem(); !ok || hi.Type != ItemTypeFavorite {
			t.Fatalf("g f depois de g c: destaque %+v, quer o primeiro favorito", hi)
		}
		m = play(t, m, "g r")
		if hi, ok := m.HighlightedItem(); !ok || hi.Type != ItemTypeRecent {
			t.Fatalf("g r depois de g c: destaque %+v, quer o primeiro recente", hi)
		}
		m = play(t, m, "g g")
		if hi, ok := m.HighlightedItem(); !ok || hi.Type != ItemTypePinnedCategory {
			t.Fatalf("g g depois de g c: destaque %+v, quer a primeira categoria fixada", hi)
		}
		return play(t, m, "g c")
	})
}

// O atalho de categorias leva à primeira categoria ao vivo nos dois mapas, da
// tela inicial e de dentro de uma categoria, mesmo quando a categoria de onde se
// saiu é outra: Esc é que volta a ela. A recarga do catálogo, que chega depois,
// mantém o destaque no topo dele.
func TestAtalhoDeCategoriasVaiAoTopoDoCatalogo(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		new  func(*testing.T, int, int, ...Option) (Model, *scenarioWorld)
	}{
		{"padrão g c", "g c", newScenario},
		{"padrão ctrl+r", "ctrl+r", newScenario},
		{"clássico ctrl+r", "ctrl+r", newScenarioClassic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := tc.new(t, 80, 24, homeCheia()...)
			// O rodapé cita a tecla principal de cada mapa: g c no padrão, Ctrl+R no clássico.
			if hint := m.footerLabel(actCategories, "") + " " + m.t("footer.categories"); !strings.Contains(footerLine(m), hint) {
				t.Fatalf("rodapé sem %q: %q", hint, footerLine(m))
			}
			m = play(t, m, tc.key)
			if hi, ok := m.HighlightedCategory(); !ok || hi.ID != "cat-jc" {
				t.Fatalf("da tela inicial: destaque %+v, quer Just Chatting", hi)
			}

			// Abre VALORANT, a terceira categoria ao vivo, e volta pelo atalho.
			m = play(t, m, "down down enter")
			if m.Mode() != ModeChannels || m.SelectedCategory() == nil || m.SelectedCategory().ID != "cat-val" {
				t.Fatalf("preparação: modo %s, categoria %+v; quer os canais de VALORANT", m.Mode(), m.SelectedCategory())
			}
			m = play(t, m, tc.key)
			if m.Mode() != ModeCategories {
				t.Fatalf("o atalho deixou a lista em %s", m.Mode())
			}
			if hi, ok := m.HighlightedCategory(); !ok || hi.ID != "cat-jc" {
				t.Fatalf("de dentro de VALORANT: destaque %+v, quer Just Chatting", hi)
			}

			// Esc, que só volta um nível, continua devolvendo o destaque à categoria aberta.
			m = play(t, m, "down down enter esc")
			if hi, ok := m.HighlightedCategory(); !ok || hi.ID != "cat-val" {
				t.Fatalf("Esc: destaque %+v, quer VALORANT", hi)
			}
		})
	}
}

// Uma página nova do catálogo que chega depois do atalho destaca a primeira
// categoria dela, mesmo que antes fosse outra. Uma seta antes de a página
// chegar é escolha do usuário e fica.
func TestAtalhoDeCategoriasSegueARecarga(t *testing.T) {
	m, _ := newScenario(t, 80, 24, homeCheia()...)
	// As teclas sem resolver os comandos: a recarga fica a caminho.
	for _, k := range runes("gc") {
		m, _ = pressKey(m, k)
	}
	if !m.list.loading {
		t.Fatal("preparação: o atalho não recarregou o catálogo")
	}
	page := []catalog.Category{{ID: "cat-new", Name: "Nova no topo", Viewers: catalog.IntPtr(900000)}}
	page = append(page, scenarioCategories()...)
	loaded := CategoriesLoadedMsg{Generation: m.list.generation, Page: catalog.Page[catalog.Category]{Items: page}}

	got := feed(t, m, loaded)
	if hi, ok := got.HighlightedCategory(); !ok || hi.ID != "cat-new" {
		t.Fatalf("depois da recarga: destaque %+v, quer a nova primeira categoria", hi)
	}

	moved := play(t, m, "down")
	movedID := moved.list.highlightedID
	moved = feed(t, moved, loaded)
	if moved.list.highlightedID != movedID {
		t.Fatalf("a recarga passou por cima da seta: destaque %q, era %q", moved.list.highlightedID, movedID)
	}
}
