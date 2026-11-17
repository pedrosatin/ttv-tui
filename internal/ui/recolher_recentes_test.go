package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// countType conta as linhas de um tipo na lista.
func countType(items []ListItem, typ ListItemType) int {
	n := 0
	for _, it := range items {
		if it.Type == typ {
			n++
		}
	}
	return n
}

// expandedScenario é a tela inicial com 2 favoritos e 20 recentes, já com
// "mais…" aberto e o destaque na linha "menos…", no fim da seção.
func expandedScenario(t *testing.T, width, height int, opts ...Option) Model {
	t.Helper()
	favs, recs := savedForScenario()
	m, _ := newScenario(t, width, height, append([]Option{WithFavorites(favs[:2]), WithRecents(recs)}, opts...)...)
	m = play(t, m, strings.Repeat("down ", 2+homeRecentsLimit)+"enter")
	if !m.list.recentsExpanded {
		t.Fatal("preparação: Enter no \"mais…\" não expandiu")
	}
	m = play(t, m, strings.Repeat("down ", len(recs)-homeRecentsLimit))
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeFewerRecents {
		t.Fatalf("preparação: destaque %+v, quer a linha \"menos…\"", it)
	}
	return m
}

// Com 2 favoritos e 20 recentes, o usuário abre "mais…" e desce até o fim da
// seção: a última linha é "menos… (-14)", logo antes das categorias.
func TestCenarioRecentesExpandidosTerminamEmMenos(t *testing.T) {
	eachSize(t, "recentes-expandidos-com-menos", func(t *testing.T, width, height int) Model {
		m := expandedScenario(t, width, height)
		fewer := fmt.Sprintf(m.t("recents.fewer"), 20-homeRecentsLimit)
		if line := lineWith(m, fewer); line < 0 || line != highlightedLine(m) {
			t.Fatalf("a linha %q não está destacada na tela:\n%s", fewer, screen(m))
		}
		return m
	})
}

// Enter em "menos…" volta aos 6 primeiros recentes. O destaque fica na linha
// "mais…", que reaparece, e as categorias logo abaixo dela já estão na tela.
func TestCenarioRecolherRecentes(t *testing.T) {
	eachSize(t, "recentes-recolhidos", func(t *testing.T, width, height int) Model {
		m := expandedScenario(t, width, height)
		m = play(t, m, "enter")
		if m.list.recentsExpanded {
			t.Fatal("Enter no \"menos…\" não recolheu")
		}
		items := m.VisibleItems()
		if n := countType(items, ItemTypeRecent); n != homeRecentsLimit {
			t.Fatalf("depois de recolher a lista mostra %d recentes; quer %d", n, homeRecentsLimit)
		}
		if n := countType(items, ItemTypeFewerRecents); n != 0 {
			t.Fatalf("a linha \"menos…\" ficou depois de recolher")
		}
		more := fmt.Sprintf(m.t("recents.more"), 20-homeRecentsLimit)
		if line := lineWith(m, more); line < 0 || line != highlightedLine(m) {
			t.Fatalf("o destaque não está na linha %q:\n%s", more, screen(m))
		}
		for _, cat := range scenarioCategories() {
			if lineWith(m, cat.Name) < 0 {
				t.Fatalf("a categoria %q não está na tela depois de recolher:\n%s", cat.Name, screen(m))
			}
		}
		return m
	})
}

// A linha "menos…" só existe com a lista expandida e com mais recentes do que
// cabem recolhidos; o filtro de texto, que já mostra todos os que casam, não a
// mostra.
func TestLinhaMenosSoComRecentesExpandidos(t *testing.T) {
	recs := func(n int) []catalog.Channel {
		out := make([]catalog.Channel, n)
		for i := range out {
			out[i] = catalog.Channel{ID: fmt.Sprintf("rec-%d", i), Login: fmt.Sprintf("recente%d", i)}
		}
		return out
	}
	cases := []struct {
		name       string
		recents    int
		filter     string
		expanded   bool
		wantFewer  int
		wantHidden int
	}{
		{name: "recolhida", recents: 20},
		{name: "expandida", recents: 20, expanded: true, wantFewer: 1, wantHidden: 20 - homeRecentsLimit},
		{name: "um a mais", recents: homeRecentsLimit + 1, expanded: true, wantFewer: 1, wantHidden: 1},
		{name: "cabem todos", recents: homeRecentsLimit, expanded: true},
		{name: "com filtro", recents: 20, filter: "recente1", expanded: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newClassicModel(mustCatalog(t, i18n.LangPTBR), WithRecents(recs(tc.recents)))
			m.list.filter = tc.filter
			m.list.recentsExpanded = tc.expanded
			items := m.VisibleItems()
			fewer := slices.IndexFunc(items, func(it ListItem) bool { return it.Type == ItemTypeFewerRecents })
			if n := countType(items, ItemTypeFewerRecents); n != tc.wantFewer {
				t.Fatalf("%d linhas \"menos…\", quer %d", n, tc.wantFewer)
			}
			if fewer < 0 {
				return
			}
			if items[fewer].Hidden != tc.wantHidden {
				t.Fatalf("\"menos…\" esconderia %d; quer %d", items[fewer].Hidden, tc.wantHidden)
			}
			if items[fewer-1].Type != ItemTypeRecent || slices.ContainsFunc(items[fewer+1:], func(it ListItem) bool { return it.Type == ItemTypeRecent }) {
				t.Fatalf("\"menos…\" não fecha a seção Recentes: %v", visibleLogins(items))
			}
		})
	}
}

// Recolher num terminal baixo: a janela da lista se centra na linha "mais…",
// então ela e a primeira categoria ficam à vista, sem a rolagem que a lista
// expandida pedia.
func TestRecolherCentraAJanelaNoMais(t *testing.T) {
	m := expandedScenario(t, 80, 16)
	m = play(t, m, "enter")
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeMoreRecents {
		t.Fatalf("destaque depois de recolher = %+v, quer a linha \"mais…\"", it)
	}
	hi, cat := highlightedLine(m), lineWith(m, scenarioCategories()[0].Name)
	if hi < 0 || cat < 0 || cat <= hi {
		t.Fatalf("destaque na linha %d, primeira categoria na %d:\n%s", hi, cat, screen(m))
	}
	// Uma descida chega à primeira categoria.
	m = play(t, m, "down")
	if c, ok := m.HighlightedCategory(); !ok || c.ID != scenarioCategories()[0].ID {
		t.Fatalf("depois de recolher, ↓ foi a %+v; quer a primeira categoria", c)
	}
}

// Expandir e recolher várias vezes não perde o lugar: Enter no "mais…" leva
// ao sétimo recente e Enter no "menos…" devolve o destaque ao "mais…".
func TestExpandirERecolherIdaEVolta(t *testing.T) {
	m := expandedScenario(t, 120, 40)
	_, recs := savedForScenario()
	for range 2 {
		m = play(t, m, "enter")
		if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeMoreRecents {
			t.Fatalf("destaque depois de recolher = %+v", it)
		}
		m = play(t, m, "enter")
		if got := highlightedLogin(m); got != recs[homeRecentsLimit].Login {
			t.Fatalf("destaque depois de expandir = %q, quer %q", got, recs[homeRecentsLimit].Login)
		}
		m = play(t, m, "G")
		m = play(t, m, strings.Repeat("up ", len(scenarioCategories())))
		if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeFewerRecents {
			t.Fatalf("preparação: destaque %+v, quer \"menos…\"", it)
		}
	}
}

// x e f não fazem nada na linha "menos…": não é canal.
func TestXEFNaLinhaMenosNaoMexem(t *testing.T) {
	m := expandedScenario(t, 120, 40)
	before := logins(m.Recents())
	m = play(t, m, "x f")
	if !slices.Equal(logins(m.Recents()), before) || len(m.Favorites()) != 2 {
		t.Fatalf("x/f na linha \"menos…\" mexeu: recentes %q, favoritos %q", logins(m.Recents()), logins(m.Favorites()))
	}
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeFewerRecents || !m.list.recentsExpanded {
		t.Fatalf("destaque %+v, expandida %v", it, m.list.recentsExpanded)
	}
}

// x no último recente da lista expandida passa o destaque ao recente de cima,
// não à linha "menos…", que agora conta um a menos.
func TestXNoUltimoRecenteExpandidoFicaNosRecentes(t *testing.T) {
	m := expandedScenario(t, 120, 40)
	_, recs := savedForScenario()
	m = play(t, m, "up x")
	if got := highlightedLogin(m); got != recs[len(recs)-2].Login {
		t.Fatalf("destaque depois do x = %q, quer %q", got, recs[len(recs)-2].Login)
	}
	items := m.VisibleItems()
	idx := slices.IndexFunc(items, func(it ListItem) bool { return it.Type == ItemTypeFewerRecents })
	if idx < 0 || items[idx].Hidden != len(recs)-1-homeRecentsLimit {
		t.Fatalf("a linha \"menos…\" não conta um a menos: %+v", items)
	}
}

// Com 7 recentes expandidos, x tira um e os 6 que sobram cabem sem "mais…": a
// linha "menos…" some e a expansão se desfaz. O recente que entrar depois
// volta a ficar atrás do "mais…".
func TestXQueDeixaCaberRecolhe(t *testing.T) {
	favs, recs := savedForScenario()
	recs = recs[:homeRecentsLimit+1]
	m, _ := newScenario(t, 120, 40, WithFavorites(favs[:2]), WithRecents(recs))
	m = play(t, m, strings.Repeat("down ", 2+homeRecentsLimit)+"enter")
	if got := highlightedLogin(m); got != recs[homeRecentsLimit].Login || !m.list.recentsExpanded {
		t.Fatalf("preparação: destaque %q, expandida %v", got, m.list.recentsExpanded)
	}
	m = play(t, m, "x")
	if m.list.recentsExpanded {
		t.Fatal("a expansão ficou sem nada a recolher")
	}
	items := m.VisibleItems()
	if countType(items, ItemTypeFewerRecents)+countType(items, ItemTypeMoreRecents) != 0 {
		t.Fatalf("sobrou linha \"mais…\" ou \"menos…\": %v", visibleLogins(items))
	}
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeRecent || it.Login() != recs[homeRecentsLimit-1].Login {
		t.Fatalf("destaque depois do x = %+v, quer o último recente", it)
	}
	m.list.recents = append([]catalog.Channel{{ID: "ch-novo", Login: "novo"}}, m.list.recents...)
	if n := countType(m.VisibleItems(), ItemTypeMoreRecents); n != 1 {
		t.Fatal("o recente novo abriu a lista em vez de entrar atrás do \"mais…\"")
	}
}

// X com o destaque no "menos…" limpa tudo: a seção some, a expansão se desfaz
// e o destaque vai ao primeiro favorito.
func TestLimparComDestaqueNoMenos(t *testing.T) {
	m := expandedScenario(t, 120, 40)
	m = play(t, m, "X y")
	if m.list.recentsExpanded || len(m.Recents()) != 0 {
		t.Fatalf("depois de limpar: expandida %v, recentes %q", m.list.recentsExpanded, logins(m.Recents()))
	}
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeFavorite || it.Login() != "gaules" {
		t.Fatalf("destaque = %+v, quer o primeiro favorito", it)
	}
}

// Trocar de plataforma com o destaque no "menos…" leva a uma tela inicial
// recolhida, com o destaque numa linha dela; voltar encontra a da Twitch
// recolhida também.
func TestTrocarPlataformaComDestaqueNoMenos(t *testing.T) {
	kick := kickCatalog()
	m := expandedScenario(t, 120, 40, WithPlatformCatalog(platform.Kick, kick, kick))
	m = play(t, m, "p")
	if m.list.recentsExpanded {
		t.Fatal("a Kick herdou a expansão")
	}
	if _, ok := m.HighlightedItem(); !ok {
		t.Fatalf("sem destaque visível na Kick:\n%s", screen(m))
	}
	m = play(t, m, "p")
	items := m.VisibleItems()
	if m.list.recentsExpanded || countType(items, ItemTypeFewerRecents) != 0 || countType(items, ItemTypeMoreRecents) != 1 {
		t.Fatalf("a Twitch voltou expandida: %v", visibleLogins(items))
	}
	if _, ok := m.HighlightedItem(); !ok {
		t.Fatalf("sem destaque visível ao voltar à Twitch:\n%s", screen(m))
	}
}
