package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

// A categoria fixada ganha audiência atual, abre canais e pode ser desfixada.
func TestCategoriasFixadasCatalogoEEnter(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "f")
	it := m.VisibleItems()[0]
	if it.Type != ItemTypePinnedCategory || it.Category.Viewers == nil || *it.Category.Viewers != 410000 {
		t.Fatalf("categoria = %+v", it)
	}
	m.list.categories[0].Viewers = catalog.IntPtr(123)
	if got := *m.VisibleItems()[0].Category.Viewers; got != 123 {
		t.Fatalf("audiência = %d", got)
	}
	m = play(t, m, "enter")
	if m.Mode() != ModeChannels || m.list.selectedCategory.ID != "cat-jc" {
		t.Fatal("Enter não abriu canais")
	}
	m = play(t, m, "esc f")
	if len(m.list.favoriteCategories) != 0 {
		t.Fatal("f não desfixou")
	}
}

// Sem dado fresco, a categoria salva mantém a identidade e mostra não verificado.
func TestCenarioCategoriaFixadaSemDadoFresco(t *testing.T) {
	eachSize(t, "categoria-fixada-nao-verificada", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height, WithFavoriteCategories([]store.Category{{Platform: "twitch", ID: "antiga", Name: "Antiga"}, {Platform: "kick", ID: "antiga", Name: "KickCat"}}))
		if !strings.Contains(screen(m), "não verificado") || strings.Contains(screen(m), "KickCat") {
			t.Fatal(screen(m))
		}
		return m
	})
}

func TestCategoriaFixadaNaBuscaEPersistida(t *testing.T) {
	s := store.New(filepath.Join(t.TempDir(), "state.json"))
	m, _ := newScenario(t, 80, 24, WithStore(s))
	m.list.mode = ModeSearch
	m.list.search.categories = []catalog.Category{{ID: "cat-val", Name: "VALORANT", Viewers: catalog.IntPtr(10)}}
	m.list.channels = nil
	m.reconcileHighlight()
	m = play(t, m, "f")
	if it := m.VisibleItems()[0]; !it.Favorite || it.Type != ItemTypeCategory {
		t.Fatalf("resultado = %+v", it)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FavoriteCategories) != 1 || got.FavoriteCategories[0].Name != "VALORANT" {
		t.Fatal(got.FavoriteCategories)
	}
	m.list.platform = platform.Kick
	if m.categoryPinned("cat-val") {
		t.Fatal("categoria Twitch vazou para Kick")
	}
	m.list.platform = platform.Twitch
	m = play(t, m, "f")
	got, err = s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.FavoriteCategories) != 0 {
		t.Fatal(got.FavoriteCategories)
	}
}

// Na busca, f fixa o resultado no lugar e conserva a estrela após repetir a busca.
func TestCenarioFixaCategoriaNaBusca(t *testing.T) {
	eachSize(t, "fixa-categoria-na-busca", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "ctrl+f zelda enter f ctrl+f zelda enter")
		it := m.VisibleItems()[0]
		if it.Category == nil || !it.Favorite || !strings.Contains(screen(m), "★") {
			t.Fatalf("categoria sem estrela: %+v", it)
		}
		return m
	})
}

// O usuário fixa duas categorias seguidas sem o cursor saltar para o topo.
func TestCenarioFixaCategoriasSeguidas(t *testing.T) {
	eachSize(t, "fixa-categorias-seguidas", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m.list.categories = append(m.list.categories, catalog.Category{ID: "cat-mc", Name: "Minecraft", Viewers: catalog.IntPtr(100)})
		m = play(t, m, "down down f down f")
		if !m.categoryPinned("cat-val") || !m.categoryPinned("cat-mc") || len(m.list.favoriteCategories) != 2 {
			t.Fatalf("categorias fixadas = %+v", m.list.favoriteCategories)
		}
		return m
	})
}

// Na busca sem audiência, fixar conserva o resultado fresco com sua estrela.
func TestCenarioFixaCategoriaSemAudienciaNaBusca(t *testing.T) {
	eachSize(t, "fixa-categoria-sem-audiencia-na-busca", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m.list.mode = ModeSearch
		m.list.search.categories = []catalog.Category{{ID: "sem-live", Name: "SemLive"}}
		m.list.channels = nil
		m.reconcileHighlight()
		m = play(t, m, "f")
		view := screen(m)
		if !strings.Contains(view, "★") || !strings.Contains(view, "SemLive — sem transmissão") || strings.Contains(view, "não verificado") {
			t.Fatal(view)
		}
		return m
	})
}

// Na lista estreita, a categoria salva mantém o nome antes da marca à direita.
func TestCategoriaFixadaEstreita(t *testing.T) {
	m, _ := newScenario(t, 80, 24, WithFavoriteCategories([]store.Category{{ID: "antiga", Name: "Grand Theft Auto V"}}))
	row := m.formatListItemRow(m.VisibleItems()[0], false, 35, 0)
	if !strings.Contains(row, "Grand") || !strings.Contains(row, "[não verificado]") || strings.Index(row, "Grand") > strings.Index(row, "[não verificado]") {
		t.Fatalf("linha estreita = %q", row)
	}
}
