package ui

import (
	"context"
	"fmt"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// O usuário desce uma tela de categorias e vê o novo destaque.
func TestCenarioPaginaCategorias(t *testing.T) {
	eachSize(t, "pagina-categorias", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m.list.categories = nil
		for i := range 90 {
			m.list.categories = append(m.list.categories, catalog.Category{ID: fmt.Sprint(i), Name: fmt.Sprintf("Categoria %02d", i)})
		}
		m.reconcileHighlight()
		m = play(t, m, "pgdown")
		if m.list.highlightIndex != height-2 {
			t.Fatalf("PgDn: índice %d, quer %d", m.list.highlightIndex, height-2)
		}
		return m
	})
}

// Home, End, g e G param no primeiro e no último item carregado, e g e G digitados na busca viram texto.
func TestListaNavegacaoLonga(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "pgup home g end")
	if m.list.highlightIndex != len(m.VisibleItems())-1 {
		t.Fatal("End não chegou ao fim")
	}
	m = play(t, m, "pgdown G pgup home")
	if m.list.highlightIndex != 0 {
		t.Fatal("Home não chegou ao início")
	}
	m = play(t, m, "ctrl+f gG")
	if m.list.search.input != "gG" {
		t.Fatal("atalhos consumiram a busca")
	}
}

func TestEndCarregaSemPularItens(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	old := m.VisibleItems()
	m.browser = &fakeSource{categoriesFn: func(_ context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
		if page.Cursor != "proxima" {
			t.Fatalf("cursor = %q", page.Cursor)
		}
		return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "nova", Name: "Nova"}}}, nil
	}}
	m.list.hasMore, m.list.nextCursor = true, "proxima"
	m = play(t, m, "end")
	if len(m.VisibleItems()) != len(old)+1 || m.list.highlightedID != old[len(old)-1].ID() {
		t.Fatal("End pulou itens ou não carregou a página")
	}
	m = play(t, m, "j")
	if m.list.highlightIndex != len(old) {
		t.Fatal("j não alcançou o primeiro item novo")
	}
}

// O espectador vai ao início do chat, desce uma página com PgDn e End retoma as mensagens ao vivo.
func TestCenarioChatPaginaAbaixo(t *testing.T) {
	eachSize(t, "chat-pagina-abaixo", func(t *testing.T, width, height int) Model {
		m := scrollFixture(t, "pt-BR", 100)
		m.width, m.height = width, height
		m = play(t, m, "home")
		before, _ := m.SelectedChatMessage()
		m = play(t, m, "pgdown")
		after, _ := m.SelectedChatMessage()
		if after.Body == before.Body {
			t.Fatal("PgDn não rolou o chat")
		}
		live := play(t, m, "end")
		if !live.ChatFollowLive() {
			t.Fatal("End não retomou o ao vivo")
		}
		return m
	})
}

func TestPaginaComSecoesECampos(t *testing.T) {
	m, _ := newScenario(t, 80, 24, WithFavorites(scenarioChannels()))
	m = play(t, m, "end pgup")
	if m.list.highlightIndex != 0 {
		t.Fatal("PgUp não chegou ao primeiro item depois do cabeçalho")
	}
	m = play(t, m, "/ gG")
	if m.list.filter != "gG" {
		t.Fatal("atalhos consumiram o filtro")
	}
	m = play(t, m, "esc enter enter i gG")
	if m.chat.composeDraft != "gG" {
		t.Fatalf("rascunho = %q", m.chat.composeDraft)
	}
}

// Uma página que termina no cabeçalho escolhe o item vizinho na direção da tecla.
func TestPaginaSaltaCabecalho(t *testing.T) {
	favoritos := make([]catalog.Channel, 30)
	for i := range favoritos {
		favoritos[i] = catalog.Channel{Login: fmt.Sprintf("favorito%02d", i), DisplayName: fmt.Sprintf("Favorito %02d", i)}
	}
	for _, tc := range []struct {
		tecla       string
		inicio, fim int
	}{
		{"pgdown", 8, 30},
		{"pgup", 51, 29},
	} {
		t.Run(tc.tecla, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24, WithFavorites(favoritos))
			m.list.categories = nil
			for i := range 40 {
				m.list.categories = append(m.list.categories, catalog.Category{ID: fmt.Sprint(i), Name: fmt.Sprintf("Categoria %02d", i)})
			}
			m.reconcileHighlight()
			m.moveHighlight(tc.inicio)
			m = play(t, m, tc.tecla)
			if m.list.highlightIndex != tc.fim {
				t.Fatalf("%s: índice %d, quer vizinho %d", tc.tecla, m.list.highlightIndex, tc.fim)
			}
		})
	}
}

// Enquanto digita o filtro, o usuário desce uma tela dos resultados.
func TestFiltroPagina(t *testing.T) {
	eachSize(t, "filtro-pagina", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m.list.categories = nil
		for i := range 60 {
			m.list.categories = append(m.list.categories, catalog.Category{ID: fmt.Sprint(i), Name: fmt.Sprintf("Categoria %02d", i)})
		}
		m.reconcileHighlight()
		m = play(t, m, "/ C")
		antes := m.list.highlightIndex
		m = play(t, m, "pgdown")
		if m.list.highlightIndex <= antes {
			t.Fatal("PgDn não moveu o destaque no filtro")
		}
		m = play(t, m, "pgup")
		if m.list.highlightIndex != antes {
			t.Fatal("PgUp não voltou ao início do filtro")
		}
		return m
	})
}

func TestAjudaNavegacaoCabeEm80Colunas(t *testing.T) {
	for _, lang := range []string{"pt-BR", "en"} {
		m, _ := newScenario(t, 80, 24)
		m.catalog = testCatalog(t, lang)
		for _, k := range []string{"help.nav", "help.chat"} {
			largura := ansi.StringWidth("  " + m.t(k))
			t.Logf("%s %s: %d colunas", lang, k, largura)
			if largura > 80 {
				t.Fatalf("%s: %d colunas", k, largura)
			}
		}
	}
}

// No modo foco, as teclas de navegação avisam como reabrir a lista.
func TestCenarioPaginaListaOculta(t *testing.T) {
	eachSize(t, "pagina-lista-oculta", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "enter enter")
		m.layout.showList, m.layout.showChat = false, false
		m.normalizeFocusAfterLayout()
		m = play(t, m, "pgup pgdown home end g G")
		if m.notices.status != m.t("layout.hidden_list_key") {
			t.Fatal("aviso da lista oculta ausente")
		}
		return m
	})
}
