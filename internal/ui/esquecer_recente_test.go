package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// recentsStore grava recs num state.json temporário e devolve o store já
// carregado, como o app faz ao abrir.
func recentsStore(t *testing.T, recs []catalog.Channel) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := store.SaveTo(path, store.State{Recents: store.FromChannels(recs)}); err != nil {
		t.Fatal(err)
	}
	s := store.New(path)
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	return s
}

// storedRecents são os logins dos recentes como um novo processo os leria.
func storedRecents(t *testing.T, s *store.Store) []string {
	t.Helper()
	st, err := store.New(s.Path()).Load()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ch := range st.Recents {
		out = append(out, ch.Login)
	}
	return out
}

func logins(channels []catalog.Channel) []string {
	out := make([]string, len(channels))
	for i, ch := range channels {
		out[i] = ch.Login
	}
	return out
}

func highlightedLogin(m Model) string {
	ch, _ := m.HighlightedChannel()
	return ch.Login
}

// Com 2 favoritos e 8 recentes, o usuário desce até o terceiro recente e
// aperta x. O canal sai da seção, o destaque passa ao vizinho de baixo, o
// recente que estava escondido entra na tela, a linha "mais…" conta um a menos
// e a barra confirma. O arquivo de estado já não tem o canal.
func TestCenarioEsquecerRecente(t *testing.T) {
	eachSize(t, "esquecer-recente", func(t *testing.T, width, height int) Model {
		favs, recs := savedForScenario()
		recs = recs[:8]
		s := recentsStore(t, recs)
		m, _ := newScenario(t, width, height, WithStore(s), WithFavorites(favs[:2]), WithRecents(recs))
		m = play(t, m, "down down down down")
		if got := highlightedLogin(m); got != "recente03" {
			t.Fatalf("preparação: destaque em %q, quer recente03", got)
		}
		line := highlightedLine(m)

		m = play(t, m, "x")
		if got := highlightedLogin(m); got != "recente04" {
			t.Fatalf("destaque depois do x = %q, quer o vizinho recente04", got)
		}
		if after := highlightedLine(m); after != line {
			t.Fatalf("o destaque foi da linha %d para a %d", line, after)
		}
		// Só a barra ainda cita o canal.
		if n := strings.Count(screen(m), "recente03"); n != 1 {
			t.Fatalf("recente03 aparece %d vezes; quer só na barra:\n%s", n, screen(m))
		}
		if lineWith(m, "recente07") < 0 || lineWith(m, "mais… (+1)") < 0 {
			t.Fatalf("o recente escondido não entrou ou a linha \"mais…\" não diminuiu:\n%s", screen(m))
		}
		if lineWith(m, "recente03 removido dos recentes") < 0 {
			t.Fatalf("a barra não confirma a remoção:\n%s", screen(m))
		}
		want := slices.DeleteFunc(logins(recs), func(l string) bool { return l == "recente03" })
		if got := storedRecents(t, s); !slices.Equal(got, want) {
			t.Fatalf("recentes no disco = %q, quer %q", got, want)
		}
		return m
	})
}

// O último recente da seção sai e o destaque sobe para o recente de cima, sem
// pular para as categorias.
func TestEsquecerUltimoRecenteDestacaODeCima(t *testing.T) {
	_, recs := savedForScenario()
	recs = recs[:3]
	m, _ := newScenario(t, 80, 24, WithStore(recentsStore(t, recs)), WithRecents(recs))
	m = play(t, m, "down down x")
	if got := highlightedLogin(m); got != "recente02" {
		t.Fatalf("destaque = %q, quer recente02", got)
	}
	if got := logins(m.Recents()); !slices.Equal(got, []string{"recente01", "recente02"}) {
		t.Fatalf("recentes = %q", got)
	}
}

// Sem recente nenhum depois do x, o destaque vai à seção seguinte, a primeira
// categoria, e a seção Recentes some.
func TestEsquecerUnicoRecenteDestacaASecaoSeguinte(t *testing.T) {
	_, recs := savedForScenario()
	recs = recs[:1]
	s := recentsStore(t, recs)
	m, _ := newScenario(t, 80, 24, WithStore(s), WithRecents(recs))
	m = play(t, m, "x")
	it, ok := m.HighlightedItem()
	if !ok || it.Type != ItemTypeCategory || it.Category.ID != scenarioCategories()[0].ID {
		t.Fatalf("destaque = %+v, quer a primeira categoria", it)
	}
	if lineWith(m, m.t("section.recents")) >= 0 {
		t.Fatalf("a seção Recentes ficou vazia na tela:\n%s", screen(m))
	}
	if got := storedRecents(t, s); len(got) != 0 {
		t.Fatalf("recentes no disco = %q", got)
	}
}

// Fora de um recente, o x não tira nada: num favorito, numa categoria ou com o
// foco no chat, a barra diz onde ele funciona. Os favoritos ficam como estavam.
func TestEsquecerForaDosRecentesAvisa(t *testing.T) {
	favs, recs := savedForScenario()
	recs = recs[:2]
	for _, keys := range []string{"", "down down down", "enter tab"} {
		t.Run(keys, func(t *testing.T) {
			m, _ := newScenario(t, 120, 40, WithStore(recentsStore(t, recs)), WithFavorites(favs[:1]), WithRecents(recs))
			if keys != "" {
				m = play(t, m, keys)
			}
			m = play(t, m, "x")
			if !slices.Equal(logins(m.Recents()), logins(recs)) || !slices.Equal(logins(m.Favorites()), logins(favs[:1])) {
				t.Fatalf("x mudou os salvos: recentes %q, favoritos %q", logins(m.Recents()), logins(m.Favorites()))
			}
			want := CatalogText(m.catalog, "feedback.forget_recent")
			if !m.notices.statusWarns || m.notices.status != want {
				t.Fatalf("aviso = %q, quer %q", m.notices.status, want)
			}
		})
	}
}

// No mapa classic a tecla é a mesma, e a ajuda dos dois mapas mostra a ação na
// seção de navegação.
func TestEsquecerRecenteNosDoisMapas(t *testing.T) {
	_, recs := savedForScenario()
	recs = recs[:2]
	for name, build := range map[string]func(*testing.T, int, int, ...Option) (Model, *scenarioWorld){
		"padrao":  newScenario,
		"classic": newScenarioClassic,
	} {
		t.Run(name, func(t *testing.T) {
			s := recentsStore(t, recs)
			m, _ := build(t, 120, 40, WithStore(s), WithRecents(recs))
			m = play(t, m, "x")
			if got := storedRecents(t, s); !slices.Equal(got, []string{"recente02"}) {
				t.Fatalf("recentes no disco = %q", got)
			}
			m = play(t, m, "?")
			help := screen(m)
			if !strings.Contains(help, m.t("action.forget_recent")) {
				t.Fatalf("a ajuda não mostra a ação:\n%s", help)
			}
		})
	}
}

// Esquecer não mexe nos favoritos e não desliga o "mais…": com o resto já
// aberto, a lista continua inteira; fechada, ela ainda abre.
func TestEsquecerRecenteMantemFavoritosEMais(t *testing.T) {
	favs, recs := savedForScenario()
	recs = recs[:9]
	s := recentsStore(t, recs)
	m, _ := newScenario(t, 120, 40, WithStore(s), WithFavorites(favs[:2]), WithRecents(recs))
	// Desce até o primeiro recente e o tira; a linha "mais…" segue lá.
	m = play(t, m, "down down x")
	if !slices.Equal(logins(m.Favorites()), logins(favs[:2])) {
		t.Fatalf("favoritos = %q", logins(m.Favorites()))
	}
	m = play(t, m, strings.Repeat("down ", homeRecentsLimit)+"enter")
	if !m.list.recentsExpanded {
		t.Fatalf("o \"mais…\" não abriu:\n%s", screen(m))
	}
	shown := 0
	for _, it := range m.VisibleItems() {
		if it.Type == ItemTypeRecent {
			shown++
		}
	}
	if shown != len(recs)-1 {
		t.Fatalf("a lista aberta mostra %d recentes, quer %d", shown, len(recs)-1)
	}
	// Com a lista aberta, tirar outro recente não a fecha.
	m = play(t, m, "x")
	if !m.list.recentsExpanded || lineWith(m, "mais…") >= 0 {
		t.Fatalf("a lista aberta fechou depois do x:\n%s", screen(m))
	}
	if got := storedRecents(t, s); len(got) != len(recs)-2 {
		t.Fatalf("recentes no disco = %q", got)
	}
}
