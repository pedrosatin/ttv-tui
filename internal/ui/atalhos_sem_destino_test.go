package ui

// Cenários do ticket 08 do ux-audit-chat-recents: o atalho que não tem para
// onde ir diz por quê e qual o próximo passo, em vez de parecer quebrado.

import (
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// assertWarns confere que a barra avisa o texto key do catálogo, com a tecla
// do mapa ativo.
func assertWarns(t *testing.T, m Model, key string) {
	t.Helper()
	want := m.t(key)
	if strings.Contains(want, "{") {
		t.Fatalf("%s ficou com uma tecla sem resolver: %q", key, want)
	}
	if !m.notices.statusWarns || m.notices.status != want {
		t.Fatalf("aviso = %q (alerta = %v), quer %q", m.notices.status, m.notices.statusWarns, want)
	}
	if !strings.Contains(statusLine(m), want) {
		t.Fatalf("a barra não mostra %q: %q", want, statusLine(m))
	}
}

// assertStill confere que a tecla não mexeu na lista: mesmo modo, mesmo
// destaque, mesmo foco e nenhum carregamento novo.
func assertStill(t *testing.T, before, after Model) {
	t.Helper()
	if after.Mode() != before.Mode() || after.list.highlightedID != before.list.highlightedID ||
		after.Focus() != before.Focus() || after.list.generation != before.list.generation {
		t.Fatalf("a tecla mexeu na tela: modo %s→%s, destaque %q→%q, foco %v→%v, geração %d→%d",
			before.Mode(), after.Mode(), before.list.highlightedID, after.list.highlightedID,
			before.Focus(), after.Focus(), before.list.generation, after.list.generation)
	}
}

// Sem favorito na lista, g f não sai do lugar: a barra diz que não há
// favoritos e que f num canal adiciona um.
func TestCenarioGFSemFavoritos(t *testing.T) {
	eachSize(t, "g-f-sem-favoritos", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "down")
		got := play(t, m, "g f")
		assertStill(t, m, got)
		assertWarns(t, got, "feedback.no_favorites")
		if !strings.Contains(got.notices.status, "f num canal") {
			t.Fatalf("o aviso não cita a tecla de favoritar: %q", got.notices.status)
		}
		return got
	})
}

// Sem recente na lista, g r também não sai do lugar, mesmo dentro de uma
// categoria: a barra diz que não há recentes e que Enter num canal abre um.
func TestCenarioGRSemRecentes(t *testing.T) {
	eachSize(t, "g-r-sem-recentes", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "enter")
		if m.Mode() != ModeChannels {
			t.Fatalf("preparação: modo %s, quer os canais da categoria", m.Mode())
		}
		got := play(t, m, "g r")
		assertStill(t, m, got)
		assertWarns(t, got, "feedback.no_recents")
		if !strings.Contains(got.notices.status, "Enter") {
			t.Fatalf("o aviso não cita a tecla de abrir: %q", got.notices.status)
		}
		return got
	})
}

// Com os favoritos fora do ar e o filtro de audiência ligado, g f vai à tela
// inicial, mas não há favorito à vista: a barra diz que os filtros os escondem.
func TestCenarioGFFavoritosFiltrados(t *testing.T) {
	eachSize(t, "g-f-favoritos-filtrados", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height,
			WithFavorites([]catalog.Channel{{ID: "ch-off", Login: "fora_do_ar"}}),
			WithChannelFilters(nil, 1000, nil))
		m = play(t, m, "g f")
		if m.Mode() != ModeCategories || m.Focus() != FocusList {
			t.Fatalf("g f: modo %s, foco %v; quer a tela inicial com foco na lista", m.Mode(), m.Focus())
		}
		assertWarns(t, m, "feedback.favorites_filtered")
		return m
	})
}

// O mesmo para os recentes.
func TestCenarioGRRecentesFiltrados(t *testing.T) {
	eachSize(t, "g-r-recentes-filtrados", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height,
			WithRecents([]catalog.Channel{{ID: "ch-off", Login: "fora_do_ar"}}),
			WithChannelFilters(nil, 1000, nil))
		m = play(t, m, "g r")
		assertWarns(t, m, "feedback.recents_filtered")
		return m
	})
}

// Um recente que também é favorito só aparece entre os favoritos: para g r,
// não há recente, e o aviso é o da lista vazia.
func TestGRComRecenteQueEhFavorito(t *testing.T) {
	ch := scenarioChannels()[0]
	m, _ := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{ch}), WithRecents([]catalog.Channel{ch}))
	got := play(t, m, "g r")
	assertStill(t, m, got)
	assertWarns(t, got, "feedback.no_recents")
}

// No limite de 50, abrir um favorito tira dos pendentes o último recente sem
// estrela. A tela inicial mostra os pendentes, então g r diz que não há
// recente, e não que os filtros o escondem.
func TestGRContaOsRecentesPendentes(t *testing.T) {
	chs := scenarioChannels()
	fav, antigo := chs[0], chs[1]
	m, _ := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{fav}), WithRecents([]catalog.Channel{antigo}))
	m.list.pendingRecents = []catalog.Channel{fav}
	assertWarns(t, play(t, m, "g r"), "feedback.no_recents")
}

// Uma tecla remapeada longa não é cortada da barra em 80 colunas, em
// português e em inglês: a tecla vem primeiro, e só a primeira tecla da ação.
func TestAvisoDeFiltroComTeclaLongaCabe(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		for _, kind := range []struct {
			keys, msg string
			opt       Option
		}{
			{"g f", "feedback.favorites_filtered", WithFavorites([]catalog.Channel{{ID: "ch-off", Login: "fora_do_ar"}})},
			{"g r", "feedback.recents_filtered", WithRecents([]catalog.Channel{{ID: "ch-off", Login: "fora_do_ar"}})},
		} {
			m, _ := newScenario(t, 80, 24, kind.opt, WithChannelFilters(nil, 1000, nil))
			m.catalog = mustCatalog(t, lang)
			m.keys = newKeyRegistry(defaultActions(), map[actionID][]string{actChannelFilter: {"ctrl+w alt+shift+tab", "L"}})
			assertWarns(t, play(t, m, kind.keys), kind.msg)
		}
	}
}

// Os recentes de outra plataforma não contam na lista da Twitch.
func TestGRSoContaRecentesDaLista(t *testing.T) {
	kick := kickCatalog()
	m, _ := newScenario(t, 80, 24, WithPlatformCatalog(platform.Kick, kick, kick),
		WithRecents([]catalog.Channel{{Platform: platform.Kick, Login: "kick_recente"}}),
		WithFavorites([]catalog.Channel{{Platform: platform.Kick, Login: "kick_favorito"}}))
	assertWarns(t, play(t, m, "g r"), "feedback.no_recents")
	assertWarns(t, play(t, m, "g f"), "feedback.no_favorites")

	// Na Kick, os dois existem e g f e g r andam até eles, sem aviso.
	m = play(t, m, "p")
	got := play(t, m, "g f")
	if hi, ok := got.HighlightedItem(); !ok || hi.Type != ItemTypeFavorite || got.notices.statusWarns {
		t.Fatalf("g f na Kick: destaque %+v, aviso %q", hi, got.notices.status)
	}
	got = play(t, m, "g r")
	if hi, ok := got.HighlightedItem(); !ok || hi.Type != ItemTypeRecent || got.notices.statusWarns {
		t.Fatalf("g r na Kick: destaque %+v, aviso %q", hi, got.notices.status)
	}
}

// Com uma plataforma só, p não troca nada: a barra diz que não há outra.
func TestCenarioPComUmaPlataforma(t *testing.T) {
	eachSize(t, "p-uma-plataforma", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		got := play(t, m, "p")
		assertStill(t, m, got)
		assertWarns(t, got, "feedback.single_platform")
		return got
	})
}

// abreGaules busca e abre gaules e passa o foco ao chat, com as mensagens na
// tela.
func abreGaules(t *testing.T, m Model) Model {
	t.Helper()
	m = play(t, m, "ctrl+f gaules enter enter")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = chatLines(t, m, [2]string{"Fulano", "salve, chat"})
	if m.Focus() != FocusChat {
		m = play(t, m, "tab")
	}
	if m.Focus() != FocusChat {
		t.Fatal("preparação: o foco não foi ao chat")
	}
	return m
}

// Com o foco no chat e duas plataformas, p não troca a lista por trás do
// chat: a barra diz que Tab leva à lista, onde p troca.
func TestCenarioPComFocoNoChat(t *testing.T) {
	eachSize(t, "p-foco-no-chat", func(t *testing.T, width, height int) Model {
		kick := kickCatalog()
		m, _ := newScenario(t, width, height, WithPlatformCatalog(platform.Kick, kick, kick))
		m = abreGaules(t, m)
		got := play(t, m, "p")
		assertStill(t, m, got)
		if got.ListPlatform() != platform.Twitch {
			t.Fatalf("p com foco no chat trocou a lista para %s", got.ListPlatform())
		}
		assertWarns(t, got, "feedback.platform_list")
		if !strings.HasPrefix(got.notices.status, "Tab ") {
			t.Fatalf("o aviso não começa pela tecla da lista: %q", got.notices.status)
		}

		// Na lista, p continua trocando.
		if next := play(t, got, "tab p"); next.ListPlatform() != platform.Kick {
			t.Fatalf("p na lista ficou em %s", next.ListPlatform())
		}
		return got
	})
}

// Com o foco no chat, L não abre o menu de filtros por trás do chat: a barra
// diz que Tab leva à lista, onde ele abre.
func TestCenarioFiltroDeCanaisComFocoNoChat(t *testing.T) {
	eachSize(t, "filtro-canais-foco-no-chat", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = abreGaules(t, m)
		got := play(t, m, "L")
		assertStill(t, m, got)
		if got.list.prefs.menuOpen {
			t.Fatal("L com foco no chat abriu o menu de filtros")
		}
		assertWarns(t, got, "feedback.channel_filter_list")
		if next := play(t, got, "tab L"); !next.list.prefs.menuOpen {
			t.Fatal("L na lista não abriu o menu de filtros")
		}
		return got
	})
}

// Com o foco no chat, g f e g r sem coleção avisam e deixam o foco no chat,
// como p e L. As teclas que já agiam no chat continuam: g r com recente e g f
// com favorito levam à lista, X pergunta, f e x avisam como antes.
func TestAtalhosDeListaComFocoNoChat(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = abreGaules(t, m)
	for _, tc := range []struct{ keys, warn string }{
		{"g f", "feedback.no_favorites"},
		{"p", "feedback.single_platform"},
		{"L", "feedback.channel_filter_list"},
		{"f", "feedback.favorite_list"},
		{"x", "feedback.forget_recent"},
	} {
		got := play(t, m, tc.keys)
		assertStill(t, m, got)
		assertWarns(t, got, tc.warn)
	}

	// Depois de abrir gaules, ele é recente: g r leva a ele e X pergunta, como
	// antes. Limpos os recentes, g r avisa e o foco fica no chat.
	if got := play(t, m, "g r"); got.Focus() != FocusList || got.notices.statusWarns {
		t.Fatalf("g r com recente: foco %v, aviso %q", got.Focus(), got.notices.status)
	}
	cleared := play(t, m, "X")
	if !cleared.list.confirmClear {
		t.Fatal("X com foco no chat deixou de perguntar")
	}
	cleared = play(t, cleared, "y")
	got := play(t, cleared, "g r")
	assertStill(t, cleared, got)
	assertWarns(t, got, "feedback.no_recents")

	// g f com favorito ainda leva à lista.
	fav := play(t, m, "tab f tab")
	if fav.Focus() != FocusChat || len(fav.Favorites()) != 1 {
		t.Fatalf("preparação: foco %v, favoritos %v", fav.Focus(), fav.Favorites())
	}
	got = play(t, fav, "g f")
	if hi, ok := got.HighlightedItem(); !ok || hi.Type != ItemTypeFavorite || got.Focus() != FocusList || got.notices.statusWarns {
		t.Fatalf("g f com favorito: destaque %+v, foco %v, aviso %q", hi, got.Focus(), got.notices.status)
	}
}

// No mapa clássico, que não tem g f nem g r (g é o topo), p e l avisam com as
// teclas dele.
func TestAtalhosSemDestinoNoClassico(t *testing.T) {
	m, _ := newScenarioClassic(t, 80, 24)
	if _, ok := m.keys.keys[actFavorites]; ok {
		t.Fatal("o mapa clássico ganhou g f")
	}
	got := play(t, m, "p")
	assertStill(t, m, got)
	assertWarns(t, got, "feedback.single_platform")

	m = abreGaules(t, m)
	got = play(t, m, "l")
	assertStill(t, m, got)
	assertWarns(t, got, "feedback.channel_filter_list")
	if !strings.HasPrefix(got.notices.status, "Tab ") {
		t.Fatalf("aviso no clássico: %q", got.notices.status)
	}

	kick := kickCatalog()
	two, _ := newScenarioClassic(t, 80, 24, WithPlatformCatalog(platform.Kick, kick, kick))
	two = abreGaules(t, two)
	got = play(t, two, "p")
	assertStill(t, two, got)
	assertWarns(t, got, "feedback.platform_list")
}
