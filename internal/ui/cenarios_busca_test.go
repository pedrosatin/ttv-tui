package ui

// Cenários da busca e da lista com favoritos e recentes: os resultados vêm
// primeiro, o favorito fica no lugar dele com ★ e Enter no filtro só abre o que
// está à vista.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// savedForScenario são 7 favoritos (gaules e ohnePixel entre eles) e 20
// recentes, o bastante para encher a tela inicial de 24 linhas.
func savedForScenario() (favs, recs []catalog.Channel) {
	favs = append(favs,
		catalog.Channel{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules"},
		catalog.Channel{ID: "ch-pixel", Login: "ohnepixel", DisplayName: "ohnePixel"},
	)
	for i := 1; i <= 5; i++ {
		favs = append(favs, catalog.Channel{ID: fmt.Sprintf("ch-fav%d", i), Login: fmt.Sprintf("favorito%d", i)})
	}
	for i := 1; i <= 20; i++ {
		recs = append(recs, catalog.Channel{ID: fmt.Sprintf("ch-rec%02d", i), Login: fmt.Sprintf("recente%02d", i)})
	}
	return favs, recs
}

// screenLines é a tela sem cores, uma linha por item.
func screenLines(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

// highlightedLine devolve a linha da tela com o destaque ▸, ou -1.
func highlightedLine(m Model) int {
	return slices.IndexFunc(screenLines(m), func(l string) bool { return strings.HasPrefix(l, "▸") })
}

// lineWith devolve a primeira linha da tela que contém text, ou -1.
func lineWith(m Model, text string) int {
	return slices.IndexFunc(screenLines(m), func(l string) bool { return strings.Contains(l, text) })
}

// Com 7 favoritos e 20 recentes, o usuário busca "gaules" com Ctrl+F: a
// primeira tela mostra os resultados, com o cursor no primeiro. Gaules, que é
// favorito, aparece em "Canal exato" com ★, e os favoritos e recentes ficam na
// tela inicial.
func TestCenarioBuscaMostraResultadosPrimeiro(t *testing.T) {
	run := func(t *testing.T, width, height int) Model {
		favs, recs := savedForScenario()
		m, w := newScenario(t, width, height, WithFavorites(favs), WithRecents(recs))
		m = play(t, m, "ctrl+f gaules enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas = %q, quer só \"gaules\"", w.searched)
		}
		it, ok := m.HighlightedItem()
		if !ok || it.Channel == nil || it.Channel.Login != "gaules" || it.Type != ItemTypeExactChannel {
			t.Fatalf("destaque = %q (%v), quer gaules em Canal exato", m.HighlightedID(), it.Type)
		}
		if !it.Favorite {
			t.Fatalf("gaules é favorito e o item não diz isso: %+v", it)
		}
		exact := lineWith(m, m.t("search.section_exact"))
		hi := highlightedLine(m)
		if exact < 0 || hi != exact+1 {
			t.Fatalf("seção Canal exato na linha %d e destaque na %d; quer o destaque logo abaixo da seção:\n%s", exact, hi, screen(m))
		}
		if !strings.HasPrefix(screenLines(m)[hi], "▸★") {
			t.Fatalf("o resultado favorito não tem ★: %q", screenLines(m)[hi])
		}
		if lineWith(m, "GaulesTV") < 0 {
			t.Fatalf("o segundo resultado não está na primeira tela:\n%s", screen(m))
		}
		for _, section := range []string{"section.favorites", "section.recents"} {
			if l := lineWith(m, m.t(section)); l >= 0 && l < hi {
				t.Fatalf("a seção %q (linha %d) vem antes dos resultados:\n%s", m.t(section), l, screen(m))
			}
		}
		return m
	}
	eachSize(t, "busca-resultados-primeiro", run)
	t.Run("100x24", func(t *testing.T) {
		assertGolden(t, "busca-resultados-primeiro-100x24", screen(run(t, 100, 24)))
	})
}

// Com ohnePixel nos favoritos, o usuário digita "/pixel": a tela mostra o
// favorito destacado e a dica de como abrir e de como buscar na Twitch. Enter
// abre o que está destacado e à vista.
func TestCenarioFiltroMostraDicaEAbreSoOVisivel(t *testing.T) {
	eachSize(t, "filtro-dica-e-destaque", func(t *testing.T, width, height int) Model {
		favs, recs := savedForScenario()
		m, w := newScenario(t, width, height, WithFavorites(favs), WithRecents(recs))
		m = play(t, m, "/ pixel")

		hint := fmt.Sprintf(m.t("filter.typing_hint"), "Twitch")
		if lineWith(m, hint) < 0 {
			t.Fatalf("a dica %q não está na tela:\n%s", hint, screen(m))
		}
		hi := highlightedLine(m)
		if hi < 0 || !strings.Contains(screenLines(m)[hi], "ohnePixel") {
			t.Fatalf("destaque na linha %d; quer ohnePixel à vista:\n%s", hi, screen(m))
		}

		play(t, m, "enter")
		if len(w.searched) != 0 {
			t.Fatalf("Enter buscou %q com um item destacado à vista", w.searched)
		}
		if !slices.Equal(w.playback.started, []string{"ohnepixel"}) {
			t.Fatalf("player abriu %q, quer o ohnePixel destacado", w.playback.started)
		}
		return m
	})
}

// Dentro de uma categoria, com favoritos e recentes guardados, o usuário desce
// até alanzoka e aperta f: o canal ganha ★ na mesma linha e o cursor fica nele.
func TestCenarioFavoritarNaCategoriaNaoMoveOCanal(t *testing.T) {
	eachSize(t, "favoritar-na-categoria", func(t *testing.T, width, height int) Model {
		favs, recs := savedForScenario()
		m, _ := newScenario(t, width, height, WithFavorites(favs[2:4]), WithRecents(recs[:3]))
		// Desce além dos 2 favoritos e dos 3 recentes até a primeira categoria.
		m = play(t, m, "down down down down down enter down")
		before := highlightedLine(m)
		if hi, _ := m.HighlightedChannel(); hi.Login != "alanzoka" || before < 0 {
			t.Fatalf("preparação: destaque %q na linha %d; quer alanzoka à vista:\n%s", hi.Login, before, screen(m))
		}
		order := m.visibleItemIDs()

		m = play(t, m, "f")
		if after := highlightedLine(m); after != before {
			t.Fatalf("o destaque foi da linha %d para a %d:\n%s", before, after, screen(m))
		}
		if hi, _ := m.HighlightedChannel(); hi.Login != "alanzoka" {
			t.Fatalf("destaque = %q, quer alanzoka", hi.Login)
		}
		if got := m.visibleItemIDs(); !slices.Equal(got, order) {
			t.Fatalf("a ordem mudou ao favoritar: %q, era %q", got, order)
		}
		if !strings.HasPrefix(screenLines(m)[before], "▸★") {
			t.Fatalf("a linha favoritada não tem ★: %q", screenLines(m)[before])
		}
		return m
	})
}

// Com 2 favoritos e 20 recentes, a tela inicial mostra os favoritos, 6
// recentes e uma linha "mais…". Enter nela mostra o resto, e o cursor fica no
// sétimo recente.
func TestCenarioInicioLimitaRecentes(t *testing.T) {
	eachSize(t, "inicio-recentes-com-mais", func(t *testing.T, width, height int) Model {
		favs, recs := savedForScenario()
		m, _ := newScenario(t, width, height, WithFavorites(favs[:2]), WithRecents(recs))

		recents := func(m Model) int {
			n := 0
			for _, it := range m.VisibleItems() {
				if it.Type == ItemTypeRecent {
					n++
				}
			}
			return n
		}
		if n := recents(m); n != homeRecentsLimit {
			t.Fatalf("a tela inicial mostra %d recentes; quer %d", n, homeRecentsLimit)
		}
		more := fmt.Sprintf(m.t("recents.more"), len(recs)-homeRecentsLimit)
		if lineWith(m, more) < 0 {
			t.Fatalf("a linha %q não está na tela:\n%s", more, screen(m))
		}

		m = play(t, m, strings.Repeat("down ", 2+homeRecentsLimit)+"enter")
		if n := recents(m); n != len(recs) {
			t.Fatalf("depois de \"mais…\" a lista mostra %d recentes; quer %d", n, len(recs))
		}
		if hi, _ := m.HighlightedChannel(); hi.Login != recs[homeRecentsLimit].Login {
			t.Fatalf("destaque = %q, quer o sétimo recente %q", hi.Login, recs[homeRecentsLimit].Login)
		}
		return m
	})
}

// W6: com gaules tocando e a lista ao lado do chat, um favorito ao vivo de
// nome comprido usa a largura do painel, mesmo com outro favorito marcado
// [offline] na coluna da direita.
func TestCenarioNomeUsaLarguraDoPainel(t *testing.T) {
	const long = "deepins0x_oficial_brasil1"
	eachSize(t, "nome-usa-largura-do-painel", func(t *testing.T, width, height int) Model {
		viewers := 24200
		favs := []catalog.Channel{
			{ID: "ch-longo", Login: long, DisplayName: long, Availability: catalog.AvailabilityLive, Viewers: &viewers},
			{ID: "ch-off", Login: "dang88", DisplayName: "DanG88", Availability: catalog.AvailabilityOffline},
		}
		m, _ := newScenario(t, width, height, WithFavorites(favs))
		// Desce além dos dois favoritos, abre Just Chatting e o primeiro canal.
		m = play(t, m, "down down enter enter")
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 50})
		m = chatLines(t, m, [2]string{"Fulano", "salve, chat"})
		m = play(t, m, "tab ctrl+r")

		if !m.chatColumnVisible() || lineWith(m, "[offline]") < 0 {
			t.Fatalf("preparação: chat visível %v e o favorito offline na tela:\n%s", m.chatColumnVisible(), screen(m))
		}
		if lineWith(m, long) < 0 {
			t.Fatalf("o nome %s saiu cortado com espaço no painel:\n%s", long, screen(m))
		}
		return m
	})
}
