package ui

import (
	"fmt"
	"strings"
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

// O usuário destaca cada homônimo, chega às categorias e favorita só o recente escolhido.
func TestCenarioSalvosHomonimosPorPlataforma(t *testing.T) {
	for _, tc := range []struct {
		name             string
		favorite, recent catalog.Channel
	}{
		{"salvos-homonimos-por-plataforma", catalog.Channel{Platform: platform.Twitch, Login: "gaules"}, catalog.Channel{Platform: "vimeo.com", Login: "gaules"}},
		{"salvos-homonimos-id-por-plataforma", catalog.Channel{Login: "gaules", ID: "1"}, catalog.Channel{Platform: "vimeo.com", Login: "gaules", ID: "1"}},
		{"salvos-homonimos-youtube-twitch", catalog.Channel{Platform: platform.YouTube, Login: "gaules"}, catalog.Channel{Login: "gaules"}},
	} {
		eachSize(t, tc.name, func(t *testing.T, width, height int) Model {
			m, _ := newScenario(t, width, height, WithFavorites([]catalog.Channel{tc.favorite}), WithRecents([]catalog.Channel{tc.recent}))
			assertHighlight := func(m Model, want catalog.Channel) {
				t.Helper()
				it, ok := m.HighlightedItem()
				if !ok || it.Channel == nil || !catalog.SameChannel(*it.Channel, want) {
					t.Fatalf("destaque = %+v; esperado canal %+v", it, want)
				}
				if n := strings.Count(m.View(), "▸"); n != 1 {
					t.Fatalf("marcadores de destaque = %d; esperado 1", n)
				}
			}
			assertHighlight(m, tc.favorite)
			m = play(t, m, "down")
			assertHighlight(m, tc.recent)
			recentHighlighted := m
			m = play(t, m, "down")
			it, ok := m.HighlightedItem()
			if !ok || it.Category == nil || it.Category.ID != m.list.categories[0].ID {
				t.Fatalf("down após recente não chegou à primeira categoria: %+v", it)
			}
			m = play(t, m, "up f")
			assertHighlight(m, tc.recent)
			favorites := m.Favorites()
			if len(favorites) != 2 || !catalog.SameChannel(favorites[0], tc.favorite) || !catalog.SameChannel(favorites[1], tc.recent) {
				t.Fatalf("favoritos = %+v; esperados %+v e %+v", favorites, tc.favorite, tc.recent)
			}
			return recentHighlighted
		})
	}
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

// Após a troca de ID, o destaque procura o login só na plataforma original.
func TestDestaquePorLoginRespeitaPlataforma(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original catalog.Channel
		channels []catalog.Channel
		want     int
	}{
		{"Twitch", catalog.Channel{ID: "antigo", Login: "Gaules"}, []catalog.Channel{{Platform: "vimeo.com", ID: "outro", Login: "gaules"}, {ID: "novo", Login: "gaules"}}, 1},
		{"Vimeo", catalog.Channel{Platform: "vimeo.com", ID: "antigo", Login: "Gaules"}, []catalog.Channel{{Login: "gaules"}, {Platform: "vimeo.com", Login: "gaules"}}, 1},
		{"YouTube distingue maiúsculas", catalog.Channel{Platform: platform.YouTube, Login: "Video"}, []catalog.Channel{{Platform: platform.YouTube, Login: "video"}, {Platform: platform.YouTube, Login: "Video"}}, 1},
		{"homônimo de outra plataforma", catalog.Channel{Platform: "vimeo.com", Login: "gaules"}, []catalog.Channel{{Login: "gaules"}}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24)
			m.setHighlightToItem([]ListItem{{Type: ItemTypeChannel, Channel: &tc.original}}, 0)
			m.list.highlightedID = "id-que-saiu-da-lista"
			items := make([]ListItem, len(tc.channels))
			for i := range tc.channels {
				items[i] = ListItem{Type: ItemTypeChannel, Channel: &tc.channels[i]}
			}
			if got := m.list.indexOfHighlighted(items); got != tc.want {
				t.Fatalf("índice = %d; esperado %d", got, tc.want)
			}
			m.clearHighlight()
			if m.list.highlightedPlatform != "" {
				t.Fatal("plataforma permaneceu após limpar destaque")
			}
		})
	}
}
