package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/store"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// clearScenario é a tela inicial com 2 favoritos e 8 recentes gravados, o
// destaque no terceiro recente.
func clearScenario(t *testing.T, width, height int) (Model, []catalog.Channel, func() []string) {
	t.Helper()
	favs, recs := savedForScenario()
	recs = recs[:8]
	s := recentsStore(t, recs)
	m, _ := newScenario(t, width, height, WithStore(s), WithFavorites(favs[:2]), WithRecents(recs))
	m = play(t, m, "down down down down")
	if got := highlightedLogin(m); got != "recente03" {
		t.Fatalf("preparação: destaque em %q, quer recente03", got)
	}
	return m, recs, func() []string { return storedRecents(t, s) }
}

// X põe a pergunta na barra e não mexe em nada: os recentes continuam na tela
// e no disco, o destaque fica onde estava e o rodapé diz como responder.
func TestCenarioLimparRecentesPergunta(t *testing.T) {
	eachSize(t, "limpar-recentes-pergunta", func(t *testing.T, width, height int) Model {
		m, recs, stored := clearScenario(t, width, height)
		m = play(t, m, "X")
		if !m.list.confirmClear {
			t.Fatal("X não abriu a pergunta")
		}
		if lineWith(m, "Limpar todos os recentes desta lista? y sim · Esc/n não") < 0 {
			t.Fatalf("a barra não pergunta:\n%s", screen(m))
		}
		if lineWith(m, "y limpar · Esc cancelar") < 0 {
			t.Fatalf("o rodapé não diz como responder:\n%s", screen(m))
		}
		if got := highlightedLogin(m); got != "recente03" || !slices.Equal(logins(m.Recents()), logins(recs)) {
			t.Fatalf("a pergunta mexeu na lista: destaque %q, recentes %q", got, logins(m.Recents()))
		}
		if got := stored(); !slices.Equal(got, logins(recs)) {
			t.Fatalf("a pergunta gravou: %q", got)
		}
		return m
	})
}

// y confirma: a seção Recentes some, o destaque vai ao primeiro favorito e a
// barra confirma. O disco já não tem nenhum recente, então reabrir o app não
// traz nenhum de volta.
func TestCenarioLimparRecentesConfirma(t *testing.T) {
	eachSize(t, "limpar-recentes-confirma", func(t *testing.T, width, height int) Model {
		m, _, stored := clearScenario(t, width, height)
		m = play(t, m, "X y")
		if m.list.confirmClear {
			t.Fatal("a pergunta ficou aberta depois do y")
		}
		// A barra diz "Recentes limpos"; a lista não tem seção nem canal.
		recentRows := slices.ContainsFunc(m.VisibleItems(), func(it ListItem) bool {
			return it.Type == ItemTypeRecent || it.Type == ItemTypeMoreRecents
		})
		if len(m.Recents()) != 0 || recentRows || lineWith(m, "recente0") >= 0 {
			t.Fatalf("ainda há recentes na tela: %q\n%s", logins(m.Recents()), screen(m))
		}
		if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeFavorite || it.Login() != "gaules" {
			t.Fatalf("destaque = %+v, quer o primeiro favorito", it)
		}
		if lineWith(m, "Recentes limpos") < 0 {
			t.Fatalf("a barra não confirma a limpeza:\n%s", screen(m))
		}
		if got := stored(); len(got) != 0 {
			t.Fatalf("recentes no disco depois de limpar = %q", got)
		}
		return m
	})
}

// Esc desfaz a pergunta sem mudar nada: a tela volta a ser a de antes do X, e
// o disco continua com todos os recentes.
func TestCenarioLimparRecentesCancela(t *testing.T) {
	eachSize(t, "limpar-recentes-cancela", func(t *testing.T, width, height int) Model {
		m, recs, stored := clearScenario(t, width, height)
		before := screen(m)
		m = play(t, m, "X esc")
		if m.list.confirmClear {
			t.Fatal("Esc não fechou a pergunta")
		}
		if after := screen(m); after != before {
			t.Fatalf("a tela mudou depois de cancelar:\nantes:\n%s\ndepois:\n%s", before, after)
		}
		if got := stored(); !slices.Equal(got, logins(recs)) {
			t.Fatalf("recentes no disco = %q, quer %q", got, logins(recs))
		}
		return m
	})
}

// n também cancela, e enquanto a pergunta está aberta as outras teclas não
// fazem nada: nem andar, nem abrir canal, nem tirar um recente com x.
func TestLimparRecentesOutrasTeclasEsperamResposta(t *testing.T) {
	m, recs, stored := clearScenario(t, 120, 40)
	m = play(t, m, "X j enter x ? f")
	if !m.list.confirmClear || m.helpOpen() {
		t.Fatalf("a pergunta não segurou as teclas: aberta %v, ajuda %v", m.list.confirmClear, m.helpOpen())
	}
	if got := highlightedLogin(m); got != "recente03" || m.SelectedChannel() != nil || !slices.Equal(logins(m.Recents()), logins(recs)) {
		t.Fatalf("uma tecla passou pela pergunta: destaque %q, recentes %q", got, logins(m.Recents()))
	}
	m = play(t, m, "n")
	if m.list.confirmClear || !slices.Equal(logins(m.Recents()), logins(recs)) {
		t.Fatalf("n não cancelou: aberta %v, recentes %q", m.list.confirmClear, logins(m.Recents()))
	}
	if got := stored(); !slices.Equal(got, logins(recs)) {
		t.Fatalf("recentes no disco = %q", got)
	}
}

// No mapa classic a tecla é a mesma. Com o destaque numa categoria, ele fica
// nela depois de limpar.
func TestLimparRecentesClassicMantemDestaqueForaDosRecentes(t *testing.T) {
	_, recs := savedForScenario()
	recs = recs[:3]
	s := recentsStore(t, recs)
	m, _ := newScenarioClassic(t, 120, 40, WithStore(s), WithRecents(recs))
	m = play(t, m, "down down down down")
	it, ok := m.HighlightedItem()
	if !ok || it.Category == nil {
		t.Fatalf("preparação: destaque em %+v, quer uma categoria", it)
	}
	m = play(t, m, "X y")
	if got, _ := m.HighlightedItem(); got.Category == nil || got.Category.ID != it.Category.ID {
		t.Fatalf("destaque = %+v, quer a mesma categoria %s", got, it.Category.ID)
	}
	if got := storedRecents(t, s); len(got) != 0 || len(m.Recents()) != 0 {
		t.Fatalf("recentes: disco %q, memória %q", got, logins(m.Recents()))
	}
}

// Sem favoritos, o destaque que estava num recente vai à primeira categoria.
// O canal aberto nesta sessão, ainda pendente, sai junto.
func TestLimparRecentesSemFavoritosDestacaCategoria(t *testing.T) {
	_, recs := savedForScenario()
	recs = recs[:2]
	s := recentsStore(t, recs)
	m, _ := newScenario(t, 80, 24, WithStore(s), WithRecents(recs))
	m = play(t, m, "enter")
	if len(m.list.pendingRecents) == 0 {
		t.Fatalf("preparação: abrir o recente não deixou pendentes")
	}
	if m.Focus() != FocusList {
		m = play(t, m, "tab")
	}
	m = play(t, m, "X y")
	it, ok := m.HighlightedItem()
	if !ok || it.Type != ItemTypeCategory || it.Category.ID != scenarioCategories()[0].ID {
		t.Fatalf("destaque = %+v, quer a primeira categoria", it)
	}
	if len(m.Recents()) != 0 || len(m.list.pendingRecents) != 0 {
		t.Fatalf("recentes = %q, pendentes = %q", logins(m.Recents()), logins(m.list.pendingRecents))
	}
	if got := storedRecents(t, s); len(got) != 0 {
		t.Fatalf("recentes no disco = %q", got)
	}
}

// Sem recente na lista, X não pergunta nada: a barra diz que não há o que
// limpar.
func TestLimparRecentesSemRecentesAvisa(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, m, "X")
	if m.list.confirmClear {
		t.Fatal("X perguntou sem recente para limpar")
	}
	if want := m.t("recents.clear_none"); !m.notices.statusWarns || m.notices.status != want {
		t.Fatalf("aviso = %q, quer %q", m.notices.status, want)
	}
}

// Cada plataforma tem a própria tela inicial com os próprios recentes: limpar
// na lista da Twitch deixa os da Kick, que continuam na lista dela e no disco.
func TestLimparRecentesSoDaPlataformaDaLista(t *testing.T) {
	recs := []catalog.Channel{
		{ID: "ch-a", Login: "alpha", Platform: platform.Twitch},
		{ID: "kickzinho", Login: "kickzinho", Platform: platform.Kick},
		{ID: "ch-b", Login: "beta", Platform: platform.Twitch},
	}
	s := recentsStore(t, recs)
	m := twoPlatforms(t, i18n.LangPTBR, &countingSource{}, &countingSource{}, WithStore(s), WithRecents(recs))
	m.width, m.height = 120, 40
	m = play(t, m, "X y")
	if got := logins(m.Recents()); !slices.Equal(got, []string{"kickzinho"}) {
		t.Fatalf("recentes na memória = %q, quer só o da Kick", got)
	}
	if got := storedRecents(t, s); !slices.Equal(got, []string{"kickzinho"}) {
		t.Fatalf("recentes no disco = %q, quer só o da Kick", got)
	}
	m = pressP(t, m)
	if m.ListPlatform() != platform.Kick || !strings.Contains(screen(m), "kickzinho") {
		t.Fatalf("a lista da Kick não mostra o recente dela:\n%s", screen(m))
	}
}

// Uma abertura que o limite tirou da lista em memória, mas que ainda não foi
// gravada, também sai: senão ela voltaria ao disco na gravação da limpeza. A
// abertura da Kick fica.
func TestLimparRecentesLevaAberturaExpulsaPeloLimite(t *testing.T) {
	recs := []catalog.Channel{
		{ID: "ch-a", Login: "alpha", Platform: platform.Twitch},
		{ID: "ch-b", Login: "beta", Platform: platform.Twitch},
	}
	s := recentsStore(t, recs)
	m := twoPlatforms(t, i18n.LangPTBR, &countingSource{}, &countingSource{}, WithStore(s), WithRecents(recs))
	m.width, m.height = 120, 40
	m.list.recentLog.Opened(store.FromChannel(catalog.Channel{ID: "ch-expulso", Login: "expulso", Platform: platform.Twitch}))
	m.list.recentLog.Opened(store.FromChannel(catalog.Channel{ID: "kickaberto", Login: "kickaberto", Platform: platform.Kick}))
	m = play(t, m, "X y")
	if got := storedRecents(t, s); !slices.Equal(got, []string{"kickaberto"}) {
		t.Fatalf("recentes no disco = %q, quer só a abertura da Kick", got)
	}
}

// A ajuda dos dois mapas mostra a ação e as respostas da pergunta.
func TestLimparRecentesNaAjuda(t *testing.T) {
	for name, build := range map[string]func(*testing.T, int, int, ...Option) (Model, *scenarioWorld){
		"padrao":  newScenario,
		"classic": newScenarioClassic,
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := build(t, 120, 40)
			m = play(t, m, "?")
			help := strings.Join(m.renderHelpLines(120), "\n")
			for _, key := range []string{"action.clear_recents", "help.ctx.confirm", "action.confirm", "action.cancel_confirm"} {
				if !strings.Contains(help, m.t(key)) {
					t.Errorf("a ajuda não mostra %s:\n%s", key, help)
				}
			}
		})
	}
}
