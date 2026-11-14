package ui

import (
	"context"
	"fmt"
	"slices"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"strconv"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Com PT ativo, o espectador ainda encontra seu favorito EN ao vivo na tela inicial.
func TestCenarioFavoritoDeOutroIdiomaNaTelaInicial(t *testing.T) {
	eachSize(t, "favorito-de-outro-idioma", func(t *testing.T, width, height int) Model {
		fav := catalog.Channel{ID: "en", Login: "englishlive", DisplayName: "EnglishLive", Language: catalog.LanguagePtr("en"), Availability: catalog.AvailabilityLive}
		m, _ := newScenario(t, width, height, WithFavorites([]catalog.Channel{fav}), WithChannelFilters([]string{"pt"}, 0, nil))
		m = play(t, m, "j home")
		if !strings.Contains(m.View(), "EnglishLive") {
			t.Fatal("favorito EN ao vivo sumiu com filtro PT")
		}
		return m
	})
}

// O espectador aperta j na lista vazia e encontra PT depois de páginas EN.
func TestCenarioPaginaVaziaBuscaAProxima(t *testing.T) {
	eachSize(t, "filtro-idioma-pagina-vazia", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height, WithChannelFilters([]string{"pt"}, 0, nil))
		calls := 0
		m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			calls++
			n := 0
			if p.Cursor != "" {
				n, _ = strconv.Atoi(p.Cursor)
			}
			lang := "en"
			if n == 5 {
				lang = "pt"
			}
			ch := catalog.Channel{ID: strconv.Itoa(n), Login: "canal" + strconv.Itoa(n), DisplayName: "Canal " + strconv.Itoa(n), Language: catalog.LanguagePtr(lang)}
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{ch}, Cursor: strconv.Itoa(n + 1), HasMore: n < 5}, nil
		}}
		m.list.mode = ModeChannels
		m.list.selectedCategory = &catalog.Category{ID: "cat-jc", Name: "Just Chatting"}
		m.list.channels = []catalog.Channel{{ID: "primeiro", Login: "primeiro", Language: catalog.LanguagePtr("en")}}
		m.list.hasMore = true
		m.list.nextCursor = "1"
		m.reconcileHighlight()
		m = play(t, m, "j")
		if calls != 3 {
			t.Fatalf("rodada fez %d consultas, quer 3", calls)
		}
		m = play(t, m, "j")
		if calls != 5 || len(m.VisibleChannels()) != 1 {
			t.Fatalf("não encontrou PT: consultas=%d, visíveis=%d", calls, len(m.VisibleChannels()))
		}
		return m
	})
}

// O espectador marca PT, fecha e reabre o menu sem perder EN e ES.
func TestCenarioMenuIdiomaReaberto(t *testing.T) {
	eachSize(t, "filtro-idioma-menu-reaberto", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			var channels []catalog.Channel
			for _, lang := range []string{"en", "es", "pt"} {
				if len(p.Languages) == 0 || slices.Contains(p.Languages, lang) {
					channels = append(channels, catalog.Channel{ID: lang, Login: lang, Language: catalog.LanguagePtr(lang)})
				}
			}
			return catalog.Page[catalog.Channel]{Items: channels}, nil
		}}
		m = play(t, m, "enter l j j j enter esc l")
		langs := m.languagesInCurrentList()
		if len(langs) != 3 || langs[0].Code != "en" || langs[0].Count != 0 || langs[1].Code != "es" || langs[1].Count != 0 {
			t.Errorf("idiomas após reabrir: %+v", langs)
		}
		if m.list.prefs.menuIndex != 0 {
			t.Errorf("cursor após reabrir: %d", m.list.prefs.menuIndex)
		}
		return m
	})
}

// Com três páginas EN e PT ativo, o espectador vê como buscar a próxima rodada.
func TestCenarioRodadaIdiomaVazia(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		eachSize(t, "filtro-idioma-rodada-vazia-"+lang, func(t *testing.T, width, height int) Model {
			m, _ := newScenario(t, width, height, WithChannelFilters([]string{"pt"}, 0, nil))
			m.catalog = mustCatalog(t, lang)
			calls := 0
			m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, _ catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
				calls++
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{{ID: fmt.Sprint(calls), Login: "english", Language: catalog.LanguagePtr("en")}}, Cursor: fmt.Sprint(calls), HasMore: true}, nil
			}}
			m = play(t, m, "enter")
			if calls != 3 || !m.list.hasMore {
				t.Fatalf("rodada: consultas=%d, hasMore=%v", calls, m.list.hasMore)
			}
			hint := "j busca mais canais"
			if lang == i18n.LangEN {
				hint = "j loads more channels"
			}
			if !strings.Contains(m.View(), hint) {
				t.Errorf("dica ausente: %s", hint)
			}
			return m
		})
	}
}

func TestMenuIdiomaContaFavoritoLegadoUmaVez(t *testing.T) {
	for _, id := range []string{"", "1"} {
		t.Run("id-"+id, func(t *testing.T) {
			saved := catalog.Channel{ID: id, Login: "CANAL", Language: catalog.LanguagePtr("pt")}
			m, _ := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{saved}))
			m.list.channels = []catalog.Channel{{Platform: "twitch", ID: "1", Login: "canal", Language: catalog.LanguagePtr("pt")}}
			got := m.languagesInCurrentList()
			if len(got) != 1 || got[0].Count != 1 {
				t.Fatalf("contagem: %+v", got)
			}
		})
	}
}
