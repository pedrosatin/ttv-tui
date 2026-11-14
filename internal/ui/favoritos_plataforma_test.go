package ui

import (
	"fmt"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// O usuário favorita xQc na Kick e volta à Twitch, que mostra só os salvos dela.
func TestCenarioFavoritosPorPlataforma(t *testing.T) {
	eachSize(t, "favoritos-por-plataforma", func(t *testing.T, width, height int) Model {
		kick := kickCatalog()
		kick.hits = []catalog.Channel{{Platform: platform.Kick, ID: "xqc", Login: "xqc", DisplayName: "xQc"}}
		m, _ := newScenario(t, width, height,
			WithPlatformCatalog(platform.Kick, kick, kick),
			WithFavorites([]catalog.Channel{{Login: "twitch_favorito"}}),
			WithRecents([]catalog.Channel{{Login: "twitch_recente"}, {Platform: platform.Kick, Login: "kick_recente"}}))
		m = play(t, m, "p")
		assertSavedPlatform(t, m, platform.Kick)
		m = play(t, m, "ctrl+f xqc enter f esc")
		if len(m.Favorites()) != 2 {
			t.Fatalf("favoritos = %v", m.Favorites())
		}
		assertSavedPlatform(t, m, platform.Kick)
		if lineWith(m, "xQc") < 0 || lineWith(m, "kick_recente") < 0 {
			t.Fatal("salvos da Kick ausentes na Kick")
		}
		m = play(t, m, "p")
		assertSavedPlatform(t, m, platform.Twitch)
		if lineWith(m, "xQc") >= 0 {
			t.Fatal("xQc da Kick apareceu na Twitch")
		}
		return m
	})
}

func assertSavedPlatform(t *testing.T, m Model, plat string) {
	t.Helper()
	for _, it := range m.VisibleItems() {
		if it.Channel != nil && it.Channel.CanonicalPlatform() != plat {
			t.Fatalf("canal de outra plataforma em %s: %+v", plat, *it.Channel)
		}
	}
}

func TestBuscaKickNaoIncluiSalvosTwitch(t *testing.T) {
	kick := kickCatalog()
	m, _ := newScenario(t, 80, 24, WithPlatformCatalog(platform.Kick, kick, kick),
		WithFavorites([]catalog.Channel{{Login: "twitch_favorito"}}),
		WithRecents([]catalog.Channel{{Login: "twitch_recente"}}))
	m = play(t, m, "p ctrl+f gaules enter")
	assertSavedPlatform(t, m, platform.Kick)
}

// O usuário expande os recentes na Twitch e encontra a Kick com a lista recolhida.
func TestCenarioRecentesRecolhidosAoTrocarPlataforma(t *testing.T) {
	eachSize(t, "recentes-recolhidos-por-plataforma", func(t *testing.T, width, height int) Model {
		var recents []catalog.Channel
		for _, plat := range []string{platform.Twitch, platform.Kick} {
			for i := 0; i < homeRecentsLimit+2; i++ {
				recents = append(recents, catalog.Channel{Platform: plat, Login: fmt.Sprintf("recente_%s_%d", plat, i)})
			}
		}
		kick := kickCatalog()
		m, _ := newScenario(t, width, height, WithPlatformCatalog(platform.Kick, kick, kick), WithRecents(recents))
		m = play(t, m, "down down down down down down enter")
		if !m.list.recentsExpanded {
			t.Fatal("recentes da Twitch não expandiram")
		}
		m = play(t, m, "p")
		if m.list.recentsExpanded {
			t.Fatal("Kick herdou expansão da Twitch")
		}
		for _, it := range m.VisibleItems() {
			if it.Type == ItemTypeMoreRecents && it.Hidden == 2 {
				return m
			}
		}
		t.Fatal("Kick não mostra dois recentes restantes")
		return m
	})
}

func TestContagemRecentesSoDaListaAtiva(t *testing.T) {
	var recents []catalog.Channel
	for i := 0; i < homeRecentsLimit+1; i++ {
		recents = append(recents, catalog.Channel{Login: fmt.Sprintf("twitch_%d", i)})
	}
	for i := 0; i < 3; i++ {
		recents = append(recents, catalog.Channel{Platform: platform.Kick, Login: fmt.Sprintf("kick_%d", i)})
	}
	kick := kickCatalog()
	m, _ := newScenario(t, 80, 24, WithPlatformCatalog(platform.Kick, kick, kick), WithRecents(recents))
	check := func(m Model, wantRecent, wantHidden int) {
		t.Helper()
		recent, hidden := 0, 0
		for _, it := range m.VisibleItems() {
			if it.Type == ItemTypeRecent {
				recent++
			}
			if it.Type == ItemTypeMoreRecents {
				hidden += it.Hidden
			}
		}
		if recent != wantRecent || hidden != wantHidden {
			t.Fatalf("recentes %d, ocultos %d; esperados %d e %d", recent, hidden, wantRecent, wantHidden)
		}
	}
	check(m, homeRecentsLimit, 1)
	m = play(t, m, "p")
	check(m, 3, 0)
}

func TestFiltrosIgnoramSalvosDaOutraPlataforma(t *testing.T) {
	pt := "pt"
	kick := kickCatalog()
	m, _ := newScenario(t, 80, 24, WithPlatformCatalog(platform.Kick, kick, kick),
		WithFavorites([]catalog.Channel{{Login: "favorito", Language: &pt}}),
		WithRecents([]catalog.Channel{{Login: "recente", Language: &pt}}))
	m = play(t, m, "p")
	m.list.channels, m.list.categories = nil, nil
	m.list.pendingRecents = []catalog.Channel{{Login: "pendente", Language: &pt}}
	if langs := m.languagesInCurrentList(); len(langs) != 0 {
		t.Fatalf("idiomas da Twitch na Kick = %+v", langs)
	}
	m.list.prefs.languages = []string{"en"}
	m.list.filter = "ausente"
	if m.preferenceFilterHidesChannels() || m.hasData() || m.filterHasNoMatches() {
		t.Fatal("salvos da Twitch contaram como dados ocultos na Kick")
	}
}

func TestSalvoSemCatalogoUsaListaInicial(t *testing.T) {
	ch := catalog.Channel{Platform: platform.YouTube, Login: "video"}
	kick := kickCatalog()
	m := New(mustCatalog(t, "en"), WithPlatformCatalog(platform.Kick, kick, kick))
	if m.ListPlatform() != platform.Kick || !m.savedOnList(ch) {
		t.Fatal("sem Twitch, salvo sem catálogo deve ficar no primeiro catálogo")
	}
	twitch := kickCatalog()
	m.setCatalog(platform.Twitch, twitch, twitch)
	m.openInitialCatalog()
	if m.ListPlatform() != platform.Twitch || !m.savedOnList(ch) {
		t.Fatal("Twitch deve ser preferida mesmo registrada depois")
	}
}

// O usuário encontra gaules da Twitch nos favoritos e o homônimo do Vimeo nos recentes.
func TestCenarioSalvosHomonimosPorPlataforma(t *testing.T) {
	eachSize(t, "salvos-homonimos-por-plataforma", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height,
			WithFavorites([]catalog.Channel{{Platform: platform.Twitch, Login: "gaules"}}),
			WithRecents([]catalog.Channel{{Platform: "vimeo.com", Login: "gaules"}}))
		m = play(t, m, "down")
		var favorites, recents int
		for _, it := range m.VisibleItems() {
			if it.Type == ItemTypeFavorite && it.Channel.CanonicalPlatform() == platform.Twitch {
				favorites++
			}
			if it.Type == ItemTypeRecent && it.Channel.CanonicalPlatform() == "vimeo.com" {
				recents++
			}
		}
		if favorites != 1 || recents != 1 {
			t.Fatalf("favoritos Twitch = %d, recentes Vimeo = %d; esperados 1 e 1", favorites, recents)
		}
		if lineWith(m, "[vimeo.com] gaules") < 0 {
			t.Fatal("recente sem marca de plataforma")
		}
		return m
	})
}

func TestDeduplicacaoSalvosPorPlataforma(t *testing.T) {
	for _, tc := range []struct {
		name             string
		favorite, recent catalog.Channel
		want             int
	}{
		{"login na mesma plataforma", catalog.Channel{Login: "Gaules", ID: "1"}, catalog.Channel{Platform: platform.Twitch, Login: "gaules", ID: "2"}, 1},
		{"ID na mesma plataforma", catalog.Channel{Login: "antigo", ID: "1"}, catalog.Channel{Login: "novo", ID: "1"}, 1},
		{"ID em plataformas distintas", catalog.Channel{Login: "gaules", ID: "1"}, catalog.Channel{Platform: "vimeo.com", Login: "gaules", ID: "1"}, 2},
		{"favorito YouTube e recente Twitch", catalog.Channel{Platform: platform.YouTube, Login: "gaules"}, catalog.Channel{Login: "gaules"}, 2},
		{"YouTube distingue maiúsculas", catalog.Channel{Platform: platform.YouTube, Login: "Video"}, catalog.Channel{Platform: platform.YouTube, Login: "video"}, 2},
		{"prefixo do identificador", catalog.Channel{Platform: "vimeo.com", Login: "vimeo.com:gaules"}, catalog.Channel{Platform: "vimeo.com", Login: "gaules"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{tc.favorite}), WithRecents([]catalog.Channel{tc.recent}))
			count := 0
			for _, it := range m.VisibleItems() {
				if it.Channel != nil {
					count++
				}
			}
			if count != tc.want {
				t.Fatalf("canais = %d; esperado %d", count, tc.want)
			}
		})
	}
}
