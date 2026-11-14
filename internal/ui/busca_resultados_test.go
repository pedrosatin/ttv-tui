package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Enter no filtro abre o item destacado só quando ele está na tela; nos outros
// casos o texto do filtro vai à busca da plataforma.
func TestEnterNoFiltroSoAbreDestaqueVisivel(t *testing.T) {
	cases := []struct {
		name       string
		keys       string
		prepare    func(m Model) Model
		wantOpened []string
		wantSearch []string
	}{
		{
			name:       "favorito destacado à vista abre",
			keys:       "/ gau",
			wantOpened: []string{"gaules"},
		},
		{
			name: "destaque que não está na lista vai à busca",
			keys: "/ gau",
			prepare: func(m Model) Model {
				m.list.highlightedID = "canal-que-saiu"
				m.list.highlightedLogin = ""
				return m
			},
			wantSearch: []string{"gau"},
		},
		{
			name:       "nada casa e o texto vai à busca",
			keys:       "/ zzz",
			wantSearch: []string{"zzz"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fav := scenarioChannels()[0]
			m, w := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{fav}))
			m = play(t, m, tc.keys)
			if tc.prepare != nil {
				m = tc.prepare(m)
			}
			play(t, m, "enter")
			if !slices.Equal(w.playback.started, tc.wantOpened) {
				t.Fatalf("player abriu %q, quer %q", w.playback.started, tc.wantOpened)
			}
			if !slices.Equal(w.searched, tc.wantSearch) {
				t.Fatalf("buscas = %q, quer %q", w.searched, tc.wantSearch)
			}
		})
	}
}

// A dica do filtro aparece só enquanto se digita nele e cita a plataforma da
// lista.
func TestDicaDoFiltroSoEnquantoSeDigita(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	hint := fmt.Sprintf(m.t("filter.typing_hint"), "Twitch")
	if lineWith(m, hint) >= 0 {
		t.Fatalf("a dica aparece sem o filtro aberto:\n%s", screen(m))
	}
	m = play(t, m, "/ just")
	if lineWith(m, hint) < 0 {
		t.Fatalf("a dica não aparece com o filtro aberto:\n%s", screen(m))
	}
	m = play(t, m, "esc")
	if lineWith(m, hint) >= 0 {
		t.Fatalf("a dica ficou depois de Esc:\n%s", screen(m))
	}
}

// A tela inicial mostra homeRecentsLimit recentes e a linha "mais…" com o
// resto; com o filtro aberto ou depois de "mais…", mostra todos.
func TestRecentesDaTelaInicial(t *testing.T) {
	recs := func(n int) []catalog.Channel {
		out := make([]catalog.Channel, n)
		for i := range out {
			out[i] = catalog.Channel{ID: fmt.Sprintf("rec-%02d", i), Login: fmt.Sprintf("recente%02d", i)}
		}
		return out
	}
	cases := []struct {
		name       string
		recents    int
		filter     string
		expanded   bool
		wantShown  int
		wantHidden int
	}{
		{name: "poucos recentes, sem mais", recents: homeRecentsLimit, wantShown: homeRecentsLimit},
		{name: "um a mais vira mais…", recents: homeRecentsLimit + 1, wantShown: homeRecentsLimit, wantHidden: 1},
		{name: "vinte recentes", recents: 20, wantShown: homeRecentsLimit, wantHidden: 20 - homeRecentsLimit},
		{name: "filtro mostra todos os que casam", recents: 20, filter: "recente1", wantShown: 10},
		{name: "depois de mais… mostra todos", recents: 20, expanded: true, wantShown: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangPTBR), WithRecents(recs(tc.recents)))
			m.list.filter = tc.filter
			m.list.recentsExpanded = tc.expanded
			shown, hidden, moreRows := 0, 0, 0
			for _, it := range m.VisibleItems() {
				switch it.Type {
				case ItemTypeRecent:
					shown++
				case ItemTypeMoreRecents:
					moreRows++
					hidden = it.Hidden
				}
			}
			if shown != tc.wantShown || hidden != tc.wantHidden {
				t.Fatalf("recentes à vista %d e escondidos %d; quer %d e %d", shown, hidden, tc.wantShown, tc.wantHidden)
			}
			if wantRows := min(tc.wantHidden, 1); moreRows != wantRows {
				t.Fatalf("%d linhas \"mais…\", quer %d", moreRows, wantRows)
			}
		})
	}
}

// f na linha "mais…" não favorita nada e não salva.
func TestFNaLinhaMaisNaoFavorita(t *testing.T) {
	recents := make([]catalog.Channel, 10)
	for i := range recents {
		recents[i] = catalog.Channel{ID: fmt.Sprintf("rec-%d", i), Login: fmt.Sprintf("recente%d", i)}
	}
	m := New(mustCatalog(t, i18n.LangPTBR), WithRecents(recents))
	m.list.highlightedID = moreRecentsID
	if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeMoreRecents {
		t.Fatalf("preparação: destaque %+v, quer a linha mais…", it)
	}
	m = play(t, m, "f")
	if len(m.Favorites()) != 0 {
		t.Fatalf("f na linha mais… favoritou %+v", m.Favorites())
	}
}

// W6: uma linha sem marca própria usa a largura inteira; a coluna das marcas
// só aperta a linha que tem marca.
func TestNomeSemMarcaUsaALarguraToda(t *testing.T) {
	const name = "deepins0x_oficial"
	cases := []struct {
		name     string
		tag      string
		maxWidth int
		wantName bool
	}{
		{name: "sem marca, painel de 31 colunas", maxWidth: 31, wantName: true},
		{name: "com marca, painel de 31 colunas", tag: "[offline]", maxWidth: 31, wantName: false},
		{name: "com marca, painel de 44 colunas", tag: "[offline]", maxWidth: 44, wantName: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := ansi.Strip(fitChannelRow("  ", "● 24.2k", name, tc.tag, "Live de hoje", false, tc.maxWidth, len("[offline]")))
			if w := ansi.StringWidth(row); w > tc.maxWidth {
				t.Fatalf("linha com %d colunas passa de %d: %q", w, tc.maxWidth, row)
			}
			if got := strings.Contains(row, name); got != tc.wantName {
				t.Fatalf("nome inteiro na linha = %v, quer %v: %q", got, tc.wantName, row)
			}
			if tc.tag != "" && !strings.HasSuffix(strings.TrimRight(row, " "), tc.tag) {
				t.Fatalf("a marca saiu da coluna da direita: %q", row)
			}
		})
	}
}
