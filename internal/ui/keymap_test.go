package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
	assertClassicKeys(t, newClassicModel(nil).keys)
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

// Ctrl+Q e F1 desfazem o prefixo e fazem o seu trabalho: Ctrl+Q sai e F1 abre
// a ajuda. As outras teclas com Ctrl valem a letra (veja o teste seguinte).
func TestMapaLogicoTeclasGlobaisDesfazemOPrefixo(t *testing.T) {
	for _, tc := range []struct {
		key  string
		done func(Model) bool
	}{
		{"ctrl+q", func(m Model) bool { return m.Quitting() }},
		{"f1", func(m Model) bool { return m.HelpVisible() }},
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

// Como no vim, Ctrl+letra depois do prefixo vale a letra: Ctrl+W Ctrl+X troca
// os lados, como Ctrl+W x, em vez de encerrar o vídeo; Ctrl+W Ctrl+H esconde a
// lista; Ctrl+W Ctrl+R, sem sequência, é engolido e não volta às categorias.
func TestMapaLogicoCtrlLetraDepoisDoPrefixoValeALetra(t *testing.T) {
	m, w := hostedScenarioDefault(t, 160, 40)
	m = play(t, m, "enter enter")
	m = play(t, m, "ctrl+w ctrl+x")
	if !m.hosted.panelsSwapped || w.playback.stopped != 0 || m.prefix != "" {
		t.Fatalf("Ctrl+W Ctrl+X: lados trocados %v, vídeo parado %d vez(es), prefixo %q", m.hosted.panelsSwapped, w.playback.stopped, m.prefix)
	}
	m = play(t, m, "ctrl+w ctrl+x ctrl+w ctrl+r")
	if m.Mode() == ModeCategories || m.prefix != "" {
		t.Fatalf("Ctrl+W Ctrl+R saiu dos canais (modo %v) ou deixou o prefixo %q", m.Mode(), m.prefix)
	}
	m = play(t, m, "ctrl+w ctrl+h")
	if m.listVisible() {
		t.Fatal("Ctrl+W Ctrl+H não escondeu a lista")
	}
}

// Na escrita do chat e no filtro /, Ctrl+W apaga a palavra anterior, como na
// busca, e não começa o prefixo.
func TestMapaLogicoCtrlWApagaPalavraNaEscritaENoFiltro(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, focusChat(t, openGaulesWithChat(t, m)), "i fix space ab ctrl+w")
	if m.chat.composeDraft != "fix " || m.prefix != "" {
		t.Fatalf("escrita: rascunho %q, prefixo %q", m.chat.composeDraft, m.prefix)
	}
	m, _ = newScenario(t, 120, 40)
	m = play(t, m, "enter / fix space ab ctrl+w")
	if m.list.filter != "fix " || m.prefix != "" {
		t.Fatalf("filtro: texto %q, prefixo %q", m.list.filter, m.prefix)
	}
}

// Ctrl+W h e Ctrl+W l seguem o lado do painel, e o texto também: o popup e a
// ajuda falam do painel da esquerda e da direita, e a barra do modo foco, depois
// de Ctrl+W x, diz que Ctrl+W h traz o chat. O Ctrl+B sempre mostra a lista.
func TestMapaLogicoTextoDoPainelSegueOLado(t *testing.T) {
	m, _ := hostedScenarioDefault(t, 160, 40)
	m = play(t, m, "enter enter ctrl+w x ctrl+w")
	m = feed(t, m, prefixPopupMsg{m.prefixGeneration})
	popup := screen(m)
	for key, side := range map[string]string{"Ctrl+W h": "esquerda", "Ctrl+W l": "direita"} {
		line := textLineWith(popup, key+"  ")
		if !strings.Contains(line, side) || strings.Contains(line, "lista") {
			t.Errorf("popup: a linha de %s não fala do painel da %s: %q", key, side, line)
		}
	}
	m = play(t, m, "esc ? tab tab tab tab tab")
	help := strings.Join(m.renderHelpLines(200), "\n")
	for key, side := range map[string]string{"Ctrl+W h": "esquerda", "Ctrl+W l": "direita"} {
		if line := textLineWith(help, key+"  "); !strings.Contains(line, side) {
			t.Errorf("ajuda: a linha de %s não fala do painel da %s: %q", key, side, line)
		}
	}
	if line := textLineWith(help, "Ctrl+B  "); !strings.Contains(line, "a lista") || strings.Contains(line, "Também") {
		t.Errorf("ajuda: a linha do Ctrl+B: %q", line)
	}
	m = play(t, m, "esc ctrl+w w ctrl+w w ctrl+w w")
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("layout %v, quer foco", m.LayoutMode())
	}
	if bar := m.Footer(); !strings.Contains(bar, "Ctrl+W h chat") || !strings.Contains(bar, "Ctrl+W l lista") {
		t.Errorf("a barra do modo foco não acompanha os lados trocados: %q", bar)
	}
}

// textLineWith é a primeira linha de text que contém sub, sem cor.
func textLineWith(text, sub string) string {
	for _, line := range strings.Split(ansi.Strip(text), "\n") {
		if strings.Contains(line, sub) {
			return line
		}
	}
	return ""
}

// classicKeysAt6b74874 são as teclas do mapa antes do prefixo Ctrl+W, tiradas
// de git show 6b74874:internal/ui/actions.go. keymap = "classic" tem de
// restaurá-las exatamente, sem ação a mais nem a menos.
var classicKeysAt6b74874 = map[actionID][]string{
	actQuit:           {"ctrl+q"},
	actCtrlC:          {"ctrl+c"},
	actHelp:           {"?"},
	actHelpAnywhere:   {"f1"},
	actCloseHelp:      {"q"},
	actHelpNext:       {"tab", "right"},
	actHelpPrev:       {"shift+tab", "left"},
	actUp:             {"up", "k"},
	actDown:           {"down", "j"},
	actPageUp:         {"pgup"},
	actPageDown:       {"pgdown"},
	actHome:           {"home", "g"},
	actEnd:            {"end", "G"},
	actSelect:         {"enter"},
	actSwitchFocus:    {"tab"},
	actBack:           {"esc"},
	actCategories:     {"ctrl+r"},
	actSwitchPlatform: {"p"},
	actFavorite:       {"f"},
	actFilter:         {"/"},
	actSearch:         {"ctrl+f", "ctrl+s"},
	actChannelFilter:  {"l"},
	actCompose:        {"i"},
	actCopyMessage:    {"y"},
	actReconnectChat:  {"ctrl+t"},
	actPause:          {" ", "space"},
	actVolumeUp:       {"+", "="},
	actVolumeDown:     {"-", "_"},
	actMute:           {"m"},
	actStopVideo:      {"ctrl+x"},
	actRetry:          {"r"},
	actCycleLayout:    {"ctrl+w"},
	actToggleList:     {"ctrl+h"},
	actToggleChat:     {"ctrl+l"},
	actChatOverlay:    {"ctrl+o"},
	actSwapSides:      {"ctrl+p"},
	actChatTaller:     {"alt+up"},
	actChatShorter:    {"alt+down"},
	actChatMoreOpaque: {"alt+pgup"},
	actChatLessOpaque: {"alt+pgdown"},
	actListWider:      {"ctrl+right"},
	actListNarrower:   {"ctrl+left"},
	actChatWider:      {"alt+left"},
	actChatNarrower:   {"alt+right"},
}

func assertClassicKeys(t *testing.T, keys keyRegistry) {
	t.Helper()
	if len(keys.keys) != len(classicKeysAt6b74874) {
		t.Errorf("o classic tem %d ações, o mapa de 6b74874 tinha %d", len(keys.keys), len(classicKeysAt6b74874))
	}
	for id, want := range classicKeysAt6b74874 {
		if got := keys.keys[id]; !slices.Equal(got, want) {
			t.Errorf("classic mudou %s: %q, quer %q", id, got, want)
		}
	}
}

// Como no vim, g g vai ao topo da lista, já que o g sozinho virou prefixo. Uma
// tecla sem sequência depois do g é engolida em silêncio.
func TestCenarioMapaLogicoGGVaiAoTopo(t *testing.T) {
	eachSize(t, "mapa-logico-g-g-topo", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h)
		m = play(t, m, "j j")
		if items := m.VisibleItems(); m.list.indexOfHighlighted(items) != 2 {
			t.Fatalf("j j destacou %d", m.list.indexOfHighlighted(items))
		}
		m = play(t, m, "g x")
		if m.list.indexOfHighlighted(m.VisibleItems()) != 2 || m.prefix != "" || m.notices.status != "" {
			t.Fatalf("g x fez algo: destaque %d, prefixo %q, aviso %q", m.list.indexOfHighlighted(m.VisibleItems()), m.prefix, m.notices.status)
		}
		m = play(t, m, "g g")
		if i := m.list.indexOfHighlighted(m.VisibleItems()); i != 0 || m.prefix != "" {
			t.Fatalf("g g destacou %d, prefixo %q", i, m.prefix)
		}
		return m
	})
}

// No terminal, o popup só lista o que vale nele: sem janela, o chat sobreposto,
// a troca de lados, a altura e a largura do chat ficam de fora.
func TestMapaLogicoPopupSemJanelaSoMostraOQueVale(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, m, "ctrl+w")
	m = feed(t, m, prefixPopupMsg{m.prefixGeneration})
	popup := screen(m)
	for _, key := range []string{"h", "l", "w"} {
		if textLineWith(popup, "Ctrl+W "+key+"  ") == "" {
			t.Errorf("o popup sem janela não mostra Ctrl+W %s", key)
		}
	}
	for _, key := range []string{"o", "x", "+", "-", "<", ">"} {
		if line := textLineWith(popup, "Ctrl+W "+key+"  "); line != "" {
			t.Errorf("o popup sem janela mostra %q", line)
		}
	}
	m, _ = hostedScenarioDefault(t, 160, 40)
	m = play(t, m, "ctrl+w")
	m = feed(t, m, prefixPopupMsg{m.prefixGeneration})
	if textLineWith(screen(m), "Ctrl+W x  ") == "" {
		t.Error("na janela, o popup não mostra Ctrl+W x")
	}
}
