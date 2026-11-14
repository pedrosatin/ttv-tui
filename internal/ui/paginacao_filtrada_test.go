package ui

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// O menu informa que conta canais na lista nos dois idiomas da interface.
func TestCenarioMenuIdiomaContaCanaisDaLista(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		eachSize(t, "filtro-idioma-menu-"+lang, func(t *testing.T, width, height int) Model {
			m, _ := newScenario(t, width, height)
			m.catalog = mustCatalog(t, lang)
			return play(t, m, "enter l")
		})
	}
}

// A rodada para no fim, no erro, no cursor que não avança e sem filtro ativo.
func TestRodadaFiltradaParaNoFimErroOuCursorRepetido(t *testing.T) {
	for _, mode := range []string{"fim", "erro", "cursor", "sem-filtro"} {
		t.Run(mode, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24, WithChannelFilters([]string{"pt"}, 0, nil))
			if mode == "sem-filtro" {
				m.list.prefs.languages = nil
			}
			calls := 0
			m.browser = &fakeSource{channelsFn: func(ctx context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
				calls++
				if mode == "erro" {
					return catalog.Page[catalog.Channel]{}, errors.New("falha de teste")
				}
				ch := catalog.Channel{ID: "en", Login: "english", Language: catalog.LanguagePtr("en")}
				cursor := "proxima"
				if mode == "cursor" {
					cursor = p.Cursor
				}
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{ch}, Cursor: cursor, HasMore: mode != "fim"}, nil
			}}
			msg := runCmd(t, m.fetchChannelsCmd(m.list.generation, catalog.Category{ID: "cat"}, "anterior", true)).(ChannelsLoadedMsg)
			if calls != 1 {
				t.Fatalf("consultas=%d", calls)
			}
			if mode == "erro" && msg.Err == nil {
				t.Fatal("erro perdido")
			}
			if mode == "cursor" && msg.Page.HasMore {
				t.Fatal("cursor repetido deve encerrar paginação")
			}
		})
	}
}

// Mudar idiomas descarta o cursor anterior e envia a seleção ao catálogo.
func TestTrocarIdiomaReiniciaCategoria(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	requests := []catalog.PageRequest{}
	m = play(t, m, "enter")
	m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
		requests = append(requests, p)
		return catalog.Page[catalog.Channel]{Items: scenarioChannels()}, nil
	}}
	m.list.nextCursor = "antigo"
	m = play(t, m, "l j enter esc")
	if len(requests) != 1 || requests[0].Cursor != "" || !slices.Equal(requests[0].Languages, []string{"pt"}) {
		t.Fatalf("consulta depois de selecionar PT: %+v", requests)
	}
	if !slices.Equal(m.FilterLanguages(), []string{"pt"}) {
		t.Fatal("seleção perdida")
	}
}

// Uma falha posterior preserva os canais e o cursor da última consulta boa.
func TestRodadaParcialPreservaCursor(t *testing.T) {
	m, _ := newScenario(t, 80, 24, WithChannelFilters([]string{"pt"}, 0, nil))
	m = play(t, m, "enter")
	var cursors []string
	m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
		cursors = append(cursors, p.Cursor)
		if len(cursors) > 1 {
			return catalog.Page[catalog.Channel]{}, errors.New("falha de teste")
		}
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{{ID: "en", Login: "english", Language: catalog.LanguagePtr("en")}}, Cursor: "2", HasMore: true}, nil
	}}
	msg := runCmd(t, m.fetchChannelsCmd(m.list.generation, *m.list.selectedCategory, "", false)).(ChannelsLoadedMsg)
	if msg.Err != nil || len(msg.Page.Items) != 1 || msg.Page.Cursor != "2" || !msg.Page.HasMore {
		t.Fatalf("página parcial: %+v", msg)
	}
	m = feed(t, m, msg)
	m = play(t, m, "j")
	if len(m.list.channels) != 1 || !slices.Equal(cursors, []string{"", "2", "2"}) {
		t.Fatalf("canais=%v, cursores=%v", m.list.channels, cursors)
	}
}

// Texto e idioma precisam passar juntos para a rodada encerrar cedo.
func TestRodadaFiltradaConsideraTexto(t *testing.T) {
	m, _ := newScenario(t, 80, 24, WithChannelFilters([]string{"pt"}, 0, nil))
	m.list.filter = "alvo"
	calls := 0
	m.browser = &fakeSource{channelsFn: func(_ context.Context, _ string, p catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
		calls++
		login := "outro"
		if calls == 2 {
			login = "alvo"
		}
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{{ID: login, Login: login, Language: catalog.LanguagePtr("pt")}}, Cursor: login, HasMore: true}, nil
	}}
	msg := runCmd(t, m.fetchChannelsCmd(m.list.generation, catalog.Category{ID: "cat"}, "", false)).(ChannelsLoadedMsg)
	if calls != 2 || len(msg.Page.Items) != 2 {
		t.Fatalf("consultas=%d, página=%+v", calls, msg.Page)
	}
}
