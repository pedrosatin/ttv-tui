package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// setupBuscaFixture cria um modelo com duas categorias na tela inicial e
// suporte a busca por "zelda", com duas categorias na tela inicial; a busca por "zelda" acha uma categoria e um canal.
func setupBuscaFixture(t *testing.T) Model {
	t.Helper()
	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "cat-cs", Name: "Counter-Strike"},
				{ID: "cat-jc", Name: "Just Chatting"},
			}}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			if categoryID == "cat-zelda" {
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
					{ID: "ch-link", Login: "hero_of_time", DisplayName: "HeroOfTime", Availability: catalog.AvailabilityLive},
				}}, nil
			}
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
				{ID: "ch-cs1", Login: "fallen", DisplayName: "FalleN", Availability: catalog.AvailabilityLive},
			}}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			if query == "zelda" {
				return []catalog.Category{
					{ID: "cat-zelda", Name: "The Legend of Zelda: Tears of the Kingdom"},
				}, nil
			}
			return nil, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			if query == "zelda" {
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
					{ID: "ch-zelda-fan", Login: "zeldafan", DisplayName: "ZeldaFan"},
				}}, nil
			}
			return catalog.Page[catalog.Channel]{}, nil
		},
	}
	m := New(catPT, WithSource(src))
	m.list.loading = false
	m.list.categories = []catalog.Category{
		{ID: "cat-cs", Name: "Counter-Strike"},
		{ID: "cat-jc", Name: "Just Chatting"},
	}
	m.list.highlightedID = "cat-cs"
	return m
}

func TestEscVoltaABuscaFluxos(t *testing.T) {
	cases := []struct {
		name string
		test func(t *testing.T, m Model)
	}{
		{
			name: "abrir categoria pela busca e Esc volta aos resultados",
			test: func(t *testing.T, m Model) {
				updated, cmd := m.startSearch("zelda")
				m = run(t, updated.(Model), cmd)
				if m.Mode() != ModeSearch || m.HighlightedID() != "cat-zelda" {
					t.Fatalf("após busca: modo=%q destaque=%q", m.Mode(), m.HighlightedID())
				}

				// Enter abre a categoria
				m, cmd = pressEnter(t, m)
				if m.Mode() != ModeChannels || m.SelectedCategory() == nil || m.SelectedCategory().ID != "cat-zelda" {
					t.Fatalf("após Enter: modo=%q cat=%+v", m.Mode(), m.SelectedCategory())
				}
				m = run(t, m, cmd)
				if m.HighlightedID() != "ch-link" {
					t.Fatalf("após carregar categoria: destaque=%q, quer ch-link", m.HighlightedID())
				}

				// Esc volta à busca com o mesmo termo e a categoria destacada
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeSearch {
					t.Fatalf("após Esc: modo=%q, quer ModeSearch", m.Mode())
				}
				if m.SearchQuery() != "zelda" {
					t.Fatalf("após Esc: busca=%q, quer \"zelda\"", m.SearchQuery())
				}
				if m.HighlightedID() != "cat-zelda" {
					t.Fatalf("após Esc: destaque=%q, quer cat-zelda", m.HighlightedID())
				}

				// Esc de novo leva às categorias ao vivo
				updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeCategories {
					t.Fatalf("após segundo Esc: modo=%q, quer ModeCategories", m.Mode())
				}
				if cmd == nil {
					t.Fatal("esperava recarga de categorias após segundo Esc")
				}
			},
		},
		{
			name: "Esc direto dos resultados leva às categorias",
			test: func(t *testing.T, m Model) {
				updated, cmd := m.startSearch("zelda")
				m = run(t, updated.(Model), cmd)

				updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeCategories {
					t.Fatalf("após Esc direto dos resultados: modo=%q, quer ModeCategories", m.Mode())
				}
				if cmd == nil {
					t.Fatal("esperava comando de recarga")
				}
			},
		},
		{
			name: "busca iniciada dentro de categoria volta à categoria de origem no segundo Esc",
			test: func(t *testing.T, m Model) {
				// Abre cat-cs primeiro
				m, cmd := pressEnter(t, m)
				m = run(t, m, cmd)
				if m.Mode() != ModeChannels || m.SelectedCategory().ID != "cat-cs" {
					t.Fatalf("preparação: modo=%q cat=%+v", m.Mode(), m.SelectedCategory())
				}

				// Busca zelda a partir de cat-cs
				updated, cmd := m.startSearch("zelda")
				m = run(t, updated.(Model), cmd)
				if m.Mode() != ModeSearch || m.list.search.origin != ModeChannels {
					t.Fatalf("após busca: modo=%q origin=%q", m.Mode(), m.list.search.origin)
				}

				// Abre cat-zelda
				m, cmd = pressEnter(t, m)
				m = run(t, m, cmd)
				if m.Mode() != ModeChannels || m.SelectedCategory().ID != "cat-zelda" {
					t.Fatalf("após abrir zelda: modo=%q cat=%+v", m.Mode(), m.SelectedCategory())
				}

				// Primeiro Esc: volta aos resultados de zelda
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeSearch || m.SearchQuery() != "zelda" || m.HighlightedID() != "cat-zelda" {
					t.Fatalf("após primeiro Esc: modo=%q busca=%q destaque=%q", m.Mode(), m.SearchQuery(), m.HighlightedID())
				}

				// Segundo Esc: volta aos canais da categoria de origem (cat-cs)
				updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeChannels || m.SelectedCategory().ID != "cat-cs" {
					t.Fatalf("após segundo Esc: modo=%q cat=%+v", m.Mode(), m.SelectedCategory())
				}
				if cmd == nil {
					t.Fatal("esperava comando de recarga dos canais da categoria de origem")
				}
			},
		},
		{
			name: "Ctrl+R na categoria aberta pela busca vai direto às categorias",
			test: func(t *testing.T, m Model) {
				updated, cmd := m.startSearch("zelda")
				m = run(t, updated.(Model), cmd)
				m, cmd = pressEnter(t, m)
				m = run(t, m, cmd)
				if m.Mode() != ModeChannels {
					t.Fatalf("modo=%q, quer ModeChannels", m.Mode())
				}

				updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
				m = updated.(Model)
				if m.Mode() != ModeCategories {
					t.Fatalf("após Ctrl+R: modo=%q, quer ModeCategories", m.Mode())
				}
				if m.canReturnToSearch() {
					t.Fatal("canReturnToSearch deve ser falso após Ctrl+R")
				}
				if cmd == nil {
					t.Fatal("esperava comando de recarga de categorias")
				}
			},
		},
		{
			name: "filtro na categoria aberta pela busca limpa antes do Esc voltar à busca",
			test: func(t *testing.T, m Model) {
				updated, cmd := m.startSearch("zelda")
				m = run(t, updated.(Model), cmd)
				m, cmd = pressEnter(t, m)
				m = run(t, m, cmd)

				// Digita filtro no canal
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
				m = updated.(Model)
				m.list.filter = "hero"

				// Primeiro Esc: limpa o filtro, permanece em ModeChannels
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeChannels || m.list.filter != "" {
					t.Fatalf("após limpar filtro: modo=%q filtro=%q", m.Mode(), m.list.filter)
				}

				// Segundo Esc: volta à busca
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeSearch || m.HighlightedID() != "cat-zelda" {
					t.Fatalf("após segundo Esc: modo=%q destaque=%q", m.Mode(), m.HighlightedID())
				}

				// Terceiro Esc: volta às categorias
				updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				m = updated.(Model)
				if m.Mode() != ModeCategories {
					t.Fatalf("após terceiro Esc: modo=%q, quer ModeCategories", m.Mode())
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := setupBuscaFixture(t)
			tc.test(t, m)
		})
	}
}
