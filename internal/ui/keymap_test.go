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

// O espectador aperta Ctrl+W, espera o popup e desiste com Ctrl+C: o popup some
// e o j seguinte anda pela lista, em vez de ser engolido como fim da sequência.
func TestCenarioMapaLogicoCtrlCDesfazOPrefixo(t *testing.T) {
	eachSize(t, "mapa-logico-ctrl-c-desfaz-prefixo", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h)
		m = play(t, m, "ctrl+w")
		gen := m.prefixGeneration
		m = feed(t, m, prefixPopupMsg{gen})
		if !m.prefixVisible {
			t.Fatal("o popup não apareceu")
		}
		m = play(t, m, "ctrl+c")
		if m.prefix != "" || m.prefixVisible {
			t.Fatalf("Ctrl+C deixou o prefixo %q ativo (popup %v)", m.prefix, m.prefixVisible)
		}
		// O temporizador antigo não reabre o popup.
		m = feed(t, m, prefixPopupMsg{gen})
		before := m.list.highlightedID
		m = play(t, m, "j")
		if m.list.highlightedID == before {
			t.Fatal("o j depois do Ctrl+C foi engolido")
		}
		return m
	})
}

// As teclas que valem em qualquer situação desfazem o prefixo e fazem o seu
// trabalho: Ctrl+Q sai, F1 abre a ajuda e Ctrl+R volta às categorias.
func TestMapaLogicoTeclasGlobaisDesfazemOPrefixo(t *testing.T) {
	for _, tc := range []struct {
		key  string
		done func(Model) bool
	}{
		{"ctrl+q", func(m Model) bool { return m.Quitting() }},
		{"f1", func(m Model) bool { return m.HelpVisible() }},
		{"ctrl+r", func(m Model) bool { return m.Mode() == ModeCategories }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m, _ := newScenario(t, 120, 40)
			m = play(t, m, "enter ctrl+w")
			m = feed(t, m, prefixPopupMsg{m.prefixGeneration})
			m = play(t, m, tc.key)
			if m.prefix != "" || m.prefixVisible {
				t.Fatalf("%s deixou o prefixo %q ativo (popup %v)", tc.key, m.prefix, m.prefixVisible)
			}
			if !tc.done(m) {
				t.Fatalf("%s depois de Ctrl+W não fez o seu trabalho", tc.key)
			}
		})
	}
}

// hostedScenarioDefault abre o cenário na janela única com o mapa padrão.
func hostedScenarioDefault(t *testing.T, width, height int, opts ...Option) (Model, *scenarioWorld) {
	t.Helper()
	exactCellWidths = true
	t.Cleanup(func() { exactCellWidths = false })
	return newScenario(t, width, height, append([]Option{WithVideoHost(&recordingHost{})}, opts...)...)
}

// Na janela, com um canal aberto e o chat sobre o vídeo, as sequências do
// prefixo mexem na altura e na largura do chat: Ctrl+W +/- e Ctrl+W </>, cada
// uma em duas teclas, como o espectador as digita.
func TestMapaLogicoPrefixoMudaAlturaELarguraNaJanela(t *testing.T) {
	m, _ := hostedScenarioDefault(t, 160, 40)
	m = play(t, m, "enter enter ctrl+w o")
	if !m.hosted.chatOverlay {
		t.Fatal("Ctrl+W o não pôs o chat sobre o vídeo")
	}
	m = play(t, m, "ctrl+w -")
	shorter := m.hosted.overlayPreferences.Rows
	if shorter == 0 || m.prefix != "" {
		t.Fatalf("Ctrl+W - não encolheu o chat: rows=%d prefixo=%q", shorter, m.prefix)
	}
	m = play(t, m, "ctrl+w +")
	if m.hosted.overlayPreferences.Rows <= shorter {
		t.Fatalf("Ctrl+W + não aumentou a altura: %d -> %d", shorter, m.hosted.overlayPreferences.Rows)
	}
	_, cols, _ := m.hostedWidths()
	m = play(t, m, "ctrl+w <")
	_, wider, _ := m.hostedWidths()
	if wider <= cols {
		t.Fatalf("Ctrl+W < não alargou o chat: %d -> %d", cols, wider)
	}
	m = play(t, m, "ctrl+w >")
	if _, back, _ := m.hostedWidths(); back != cols {
		t.Fatalf("Ctrl+W > não estreitou o chat: %d -> %d, quer %d", wider, back, cols)
	}
}

// O Ctrl+O, anunciado com o chat sobreposto, continua apelido de Ctrl+W o no
// mapa padrão: põe o chat sobre o vídeo e o devolve para o lado.
func TestMapaLogicoCtrlOContinuaApelidoDoChatSobreposto(t *testing.T) {
	m, _ := hostedScenarioDefault(t, 160, 40)
	m = play(t, m, "enter enter ctrl+o")
	if !m.hosted.chatOverlay {
		t.Fatal("Ctrl+O não pôs o chat sobre o vídeo")
	}
	m = play(t, m, "ctrl+w o")
	if m.hosted.chatOverlay {
		t.Fatal("Ctrl+W o não devolveu o chat para o lado")
	}
	m = play(t, m, "ctrl+o ctrl+o")
	if m.hosted.chatOverlay {
		t.Fatal("o segundo Ctrl+O não devolveu o chat para o lado")
	}
}

// Ctrl+H, Ctrl+L e Ctrl+P saíram do mapa padrão: na janela, com um canal
// aberto, não mexem no layout nem nos lados e não deixam aviso na barra.
func TestMapaLogicoAtalhosAntigosSemEfeitoNaJanela(t *testing.T) {
	for _, key := range []string{"ctrl+h", "ctrl+l", "ctrl+p"} {
		t.Run(key, func(t *testing.T) {
			m, _ := hostedScenarioDefault(t, 160, 40)
			m = play(t, m, "enter enter")
			layout, swapped, overlay := m.LayoutMode(), m.hosted.panelsSwapped, m.hosted.chatOverlay
			m = play(t, m, key)
			if m.LayoutMode() != layout || m.hosted.panelsSwapped != swapped || m.hosted.chatOverlay != overlay {
				t.Fatalf("%s mudou o layout: %v -> %v, lados %v -> %v", key, layout, m.LayoutMode(), swapped, m.hosted.panelsSwapped)
			}
			if m.notices.status != "" {
				t.Fatalf("%s deixou aviso na barra: %q", key, m.notices.status)
			}
		})
	}
}

// No mapa padrão, g é prefixo só fora dos campos de texto: no filtro /, na
// busca (Ctrl+F) e na escrita do chat (i), ele entra como letra.
func TestCenarioMapaLogicoGNosCampos(t *testing.T) {
	for _, tc := range []struct {
		name, keys string
		text       func(Model) string
	}{
		{"filtro", "enter / g", func(m Model) string { return m.list.filter }},
		{"busca", "ctrl+f g", func(m Model) string { return m.list.search.input }},
		{"escrita", "", func(m Model) string { return m.chat.composeDraft }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eachSize(t, "mapa-logico-g-no-campo-"+tc.name, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				if tc.keys == "" {
					m = play(t, focusChat(t, openGaulesWithChat(t, m)), "i g")
				} else {
					m = play(t, m, tc.keys)
				}
				if got := tc.text(m); got != "g" || m.prefix != "" {
					t.Fatalf("g não entrou no campo: texto %q, prefixo %q", got, m.prefix)
				}
				return m
			})
		})
	}
}
