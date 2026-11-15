package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Os testes anteriores à migração continuam cobrindo o contrato do classic.
func newClassicModel(cat *i18n.Catalog, opts ...Option) Model {
	return New(cat, append([]Option{WithKeymap(true, "")}, opts...)...)
}
func newScenarioClassic(t *testing.T, w, h int, opts ...Option) (Model, *scenarioWorld) {
	return newScenario(t, w, h, append([]Option{WithKeymap(true, "")}, opts...)...)
}
func hostedScenarioClassic(t *testing.T, w, h int, opts ...Option) (Model, *scenarioWorld) {
	return hostedScenario(t, w, h, append([]Option{WithKeymap(true, "")}, opts...)...)
}

func TestCenarioMapaLogicoPopupEAjuda(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		t.Run(lang, func(t *testing.T) {
			eachSize(t, "mapa-logico-popup-"+lang, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m.catalog = mustCatalog(t, lang)
				m = play(t, m, "ctrl+w")
				if m.prefixVisible {
					t.Fatal("popup apareceu sem esperar")
				}
				m = feed(t, m, prefixPopupMsg{m.prefixGeneration})
				if !strings.Contains(screen(m), "Ctrl+W h") {
					t.Fatal("popup sem as opções")
				}
				return m
			})
			eachSize(t, "mapa-logico-navegacao-"+lang, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m.catalog = mustCatalog(t, lang)
				return play(t, m, "?")
			})
			eachSize(t, "mapa-logico-ajuda-"+lang, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m.catalog = mustCatalog(t, lang)
				return play(t, m, "? tab tab tab tab")
			})
		})
	}
}

func TestMapaLogicoPrefixoCancelamentoECampo(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+w")
	gen := m.prefixGeneration
	m = play(t, m, "h")
	if m.listVisible() || m.prefix != "" {
		t.Fatal("sequência rápida não foi aplicada")
	}
	m = feed(t, m, prefixPopupMsg{gen})
	if m.prefixVisible {
		t.Fatal("temporizador antigo reabriu popup")
	}
	m = play(t, m, "ctrl+w esc h")
	if m.prefix != "" || m.listVisible() {
		t.Fatal("Esc não cancelou")
	}
	m = play(t, m, "ctrl+w h ctrl+f fix space ab ctrl+w")
	if m.list.search.input != "fix " || m.prefix != "" {
		t.Fatal("Ctrl+W deixou de editar o campo")
	}
}

func TestMapaLogicoJanelaELados(t *testing.T) {
	t.Cleanup(func() { exactCellWidths = false })
	m, _ := newScenario(t, 160, 40, WithVideoHost(&recordingHost{}))
	m = play(t, m, "ctrl+f gaules enter enter ctrl+w x ctrl+w h")
	if m.layout.showChat || !m.listVisible() {
		t.Fatal("h não acompanhou o painel da esquerda")
	}
	m = play(t, m, "ctrl+w h ctrl+w l")
	if m.listVisible() || !m.layout.showChat {
		t.Fatal("l não acompanhou o painel da direita")
	}
	m = play(t, m, "ctrl+b")
	if !m.listVisible() {
		t.Fatal("Ctrl+B não recuperou lista")
	}
	m = play(t, m, "ctrl+w o ctrl+w -")
	if !m.hosted.chatOverlay || m.hosted.overlayPreferences.Rows == 0 {
		t.Fatal("alias da altura não aplicado")
	}
	before := m.hosted.chatCols
	m = play(t, m, "ctrl+w <")
	if before == m.hosted.chatCols {
		t.Fatal("alias da largura não aplicado")
	}
}

func TestMapaLogicoNavegacaoETeclasLivres(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, m, "enter g c")
	if m.Mode() != ModeCategories {
		t.Fatal("g c não voltou")
	}
	m = play(t, m, "l ctrl+h ctrl+l ctrl+p ctrl+b ctrl+s")
	if m.ChannelFilterMenuOpen() || m.list.search.active || m.prefix != "" {
		t.Fatal("atalho antigo ainda ativo")
	}
	m = play(t, m, "L")
	if !m.ChannelFilterMenuOpen() {
		t.Fatal("L não abriu filtro")
	}
	for _, a := range classicActions() {
		classic := newClassicModel(nil)
		if !slices.Equal(a.Default, classic.keys.keys[a.ID]) {
			t.Fatalf("classic mudou %s", a.ID)
		}
	}
	// O terminal deixa Ctrl+B para o tmux; nenhum aviso ou layout muda.
	m, _ = newScenario(t, 120, 40)
	m = play(t, m, "ctrl+b")
	if m.notices.status != "" || m.LayoutMode() != LayoutSplit {
		t.Fatal("Ctrl+B capturado no terminal")
	}

}

func TestMapaLogicoFavoritosERecentes(t *testing.T) {
	channels := scenarioChannels()
	m, _ := newScenario(t, 120, 40, WithFavorites(channels[:1]), WithRecents(channels[1:]))
	for _, step := range []struct {
		keys string
		kind ListItemType
	}{{"g f", ItemTypeFavorite}, {"g r", ItemTypeRecent}} {
		m = play(t, m, step.keys)
		items := m.VisibleItems()
		i := m.list.indexOfHighlighted(items)
		if i < 0 || items[i].Type != step.kind {
			t.Fatalf("%s destacou %v", step.keys, m.list.highlightedID)
		}
	}
}

func TestCenarioMapaLogicoMigracao(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			eachSize(t, "mapa-logico-migracao-"+lang, func(t *testing.T, w, h int) Model {
				cat := mustCatalog(t, lang)
				notice := fmt.Sprintf(CatalogText(cat, "keymap.migration"), "/fixture/config.toml")
				m, _ := newScenario(t, w, h, WithKeymap(false, notice))
				m.catalog = cat
				if !strings.Contains(screen(m), `keymap = "classic"`) {
					t.Fatal("caminho de volta truncado")
				}
				return m
			})
		})
	}
}
