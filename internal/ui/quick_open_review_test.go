package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func TestQuickOpenOmitePrimitivasEAcoesSoDaJanela(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+k")
	for _, it := range m.quickOpenItems() {
		if it.Kind != quickAction {
			continue
		}
		switch it.Action {
		case actUp, actDown, actPageUp, actPageDown, actHome, actEnd, actSelect, actBack:
			t.Fatalf("primitiva %s apareceu no terminal", it.Action)
		}
		for _, a := range m.keys.actions {
			if a.ID == it.Action && a.windowOnly() {
				t.Fatalf("ação só da janela %s apareceu sem host", it.Action)
			}
		}
		if it.Action == actSwitchPlatform {
			t.Fatal("switch_platform com uma plataforma não deveria aparecer")
		}
	}
}

func TestQuickOpenIncluiAcoesDaJanelaComHost(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m.hosted.host = &recordingHost{}
	m = play(t, m, "ctrl+k")
	found := false
	for _, it := range m.quickOpenItems() {
		if it.Kind == quickAction && it.Action == actChatOverlay {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("com host, chat overlay deveria aparecer no seletor")
	}
	for _, it := range m.quickOpenItems() {
		if it.Kind == quickAction {
			switch it.Action {
			case actUp, actDown, actPageUp, actPageDown, actHome, actEnd, actSelect, actBack:
				t.Fatalf("primitiva %s apareceu na janela", it.Action)
			}
		}
	}
}

func TestQuickOpenHiddenListNaoInterceptaComSeletorAberto(t *testing.T) {
	for _, tc := range []struct {
		name string
		list bool
		chat bool
	}{
		{name: string(LayoutFocus), list: false, chat: false},
		{name: string(LayoutChat), list: false, chat: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24)
			m.layout.showList, m.layout.showChat = tc.list, tc.chat
			if tc.chat {
				m.chat.panel.channel = "gaules"
			}
			m.normalizeFocusAfterLayout()
			m = play(t, m, "ctrl+k")
			if !m.list.quickOpen.active {
				t.Fatal("Ctrl+K não abriu o seletor com lista oculta")
			}
			for _, k := range []tea.KeyMsg{
				{Type: tea.KeyDown},
				{Type: tea.KeyUp},
				{Type: tea.KeyEnter},
				{Type: tea.KeyEsc},
			} {
				if m.hiddenListKey(k) {
					t.Fatalf("hiddenListKey interceptou %s com seletor aberto em %s", k, tc.name)
				}
			}
			m = play(t, m, "esc")
			if m.list.quickOpen.active {
				t.Fatal("Esc não fechou o seletor com lista oculta")
			}
		})
	}
}

func TestQuickOpenRevelaListaAoBuscarEAoAbrirCategoria(t *testing.T) {
	t.Run("busca do chat", func(t *testing.T) {
		m, w := newScenario(t, 120, 40)
		m = play(t, m, "enter enter")
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		m = focusChat(t, m)
		m.layout.showList = false
		m.normalizeFocusAfterLayout()
		if m.Focus() != FocusChat {
			t.Fatalf("foco = %v, quer chat", m.Focus())
		}
		m = play(t, m, "ctrl+k z e l d a enter")
		if m.list.quickOpen.active {
			t.Fatal("seletor ficou aberto")
		}
		if !m.layout.showList {
			t.Fatal("busca pelo seletor não revelou a lista")
		}
		if m.Focus() != FocusList {
			t.Fatalf("foco = %v, quer lista", m.Focus())
		}
		if len(w.searched) == 0 || w.searched[len(w.searched)-1] != "zelda" {
			t.Fatalf("buscas = %q, quer zelda", w.searched)
		}
	})
	t.Run("categoria com lista oculta", func(t *testing.T) {
		m, _ := newScenario(t, 120, 40)
		cat := m.list.categories[0]
		m.layout.showList = false
		m.layout.showChat = true
		m.setFocus(FocusChat)
		m.normalizeFocusAfterLayout()
		m = play(t, m, "ctrl+k")
		// Digita o começo do nome da categoria e confirma o primeiro resultado de categoria.
		for _, r := range strings.ToLower(cat.Name) {
			if r == ' ' {
				continue
			}
			m = play(t, m, string(r))
			if len(m.quickOpenItems()) > 0 {
				break
			}
		}
		idx := -1
		for i, it := range m.quickOpenItems() {
			if it.Kind == quickCategory && it.Category != nil && it.Category.ID == cat.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("categoria %q ausente: %+v", cat.Name, m.quickOpenItems())
		}
		for step := 0; m.list.quickOpen.cursor != idx && step < len(m.quickOpenItems()); step++ {
			m = play(t, m, "down")
		}
		m = play(t, m, "enter")
		if !m.layout.showList {
			t.Fatal("abrir categoria pelo seletor não revelou a lista")
		}
		if m.Focus() != FocusList {
			t.Fatalf("foco = %v, quer lista", m.Focus())
		}
		if m.Mode() != ModeChannels {
			t.Fatalf("modo = %s, quer canais", m.Mode())
		}
	})
}

func TestQuickOpenFechaPreviaAntesDeAbrirCanal(t *testing.T) {
	fav := scenarioChannels()[0]
	fav.Platform = platform.Twitch
	m, w := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{fav}))
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	if !m.previewOpen() {
		t.Fatal("prévia não abriu")
	}
	beforeStop := w.playback.previewStopped
	m = play(t, m, "ctrl+k g a u enter")
	if m.previewOpen() {
		t.Fatal("prévia continuou aberta depois do seletor")
	}
	if w.playback.previewStopped <= beforeStop {
		t.Fatalf("prévia não foi fechada: stopped=%d", w.playback.previewStopped)
	}
	if len(w.playback.started) == 0 || w.playback.started[len(w.playback.started)-1] != "gaules" {
		t.Fatalf("canal não abriu: %v", w.playback.started)
	}
}

func TestQuickOpenCaixaNoTopoAlturaFixa(t *testing.T) {
	fav := scenarioChannels()[0]
	fav.Platform = platform.Twitch
	m, _ := newScenario(t, 80, 24, WithFavorites([]catalog.Channel{fav}))
	m = play(t, m, "ctrl+k")
	empty := m.renderQuickOpenLines(40)
	if len(empty) < 3 {
		t.Fatalf("caixa vazia curta demais: %d linhas", len(empty))
	}
	m = play(t, m, "g a u")
	typed := m.renderQuickOpenLines(40)
	if len(typed) != len(empty) {
		t.Fatalf("altura mudou ao digitar: vazia=%d digitada=%d", len(empty), len(typed))
	}
	screen := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(screen[0], m.t("quick_open.title")) {
		t.Fatalf("seletor não está no topo:\n%s", strings.Join(screen[:5], "\n"))
	}
}

func TestQuickOpenRespeitaTerminaisBaixos(t *testing.T) {
	for _, height := range []int{3, 5} {
		t.Run(fmt.Sprintf("%d", height), func(t *testing.T) {
			m, _ := newScenario(t, 80, height)
			m = play(t, m, "ctrl+k g a u")
			lines := m.renderQuickOpenLines(80)
			available := height - 1 - m.footerRows()
			if available > 0 && len(lines) > available {
				t.Fatalf("altura %d: %d linhas > available %d", height, len(lines), available)
			}
			rows := strings.Split(ansi.Strip(m.View()), "\n")
			if len(rows) != height {
				t.Fatalf("tela com %d linhas em terminal %d", len(rows), height)
			}
			for i, row := range rows {
				if got := ansi.StringWidth(row); got > 80 {
					t.Fatalf("linha %d com %d colunas: %q", i+1, got, row)
				}
			}
		})
	}
}

func TestQuickOpenStatusNaoRepetePrompt(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+k g a u")
	bar := ansi.Strip(m.StatusBar())
	if want := m.t("quick_open.status_open"); !strings.Contains(bar, want) {
		t.Fatalf("barra = %q, quer %q", bar, want)
	}
	if strings.Contains(bar, "Seletor: gau") || strings.Contains(bar, "Quick open: gau") {
		t.Fatalf("barra repetiu o prompt com o texto: %q", bar)
	}
}

func TestQuickOpenCasaLoginEPendentes(t *testing.T) {
	ch := catalog.Channel{
		ID: "id-login", Login: "zelda_speed", DisplayName: "Speedrunner",
		Platform: platform.Twitch,
	}
	m, _ := newScenario(t, 80, 24)
	m.list.favorites = nil
	m.list.recents = nil
	m.list.pendingRecents = []catalog.Channel{ch}
	m = play(t, m, "ctrl+k z e l d a")
	items := m.quickOpenItems()
	found := false
	for _, it := range items {
		if it.Channel != nil && it.Channel.Login == "zelda_speed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("login zelda_speed / pending recente ausente: %+v", items)
	}
}

func TestQuickOpenTruncaRotuloAntesDoSufixo(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+k")
	it := quickOpenItem{
		Kind:     quickAction,
		Label:    strings.Repeat("ação bem longa para forçar o corte do rótulo ", 4),
		KeyLabel: "Ctrl+X",
		Action:   actStopVideo,
	}
	line := m.formatQuickOpenRow(it, "  ", 40)
	if !strings.HasSuffix(strings.TrimRight(line, " "), "Ctrl+X") {
		t.Fatalf("sufixo da tecla foi cortado: %q", line)
	}
	ch := quickOpenItem{
		Kind:     quickFavorite,
		Label:    strings.Repeat("nome-muito-longo-", 6),
		Platform: platform.Twitch,
	}
	line = m.formatQuickOpenRow(ch, "  ", 40)
	if !strings.Contains(line, "[Twitch]") {
		t.Fatalf("sufixo da plataforma foi cortado: %q", line)
	}
}

func TestQuickOpenCategoriaSemBrowserAjustaModo(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m.browser = nil
	cat := catalog.Category{ID: "id-cat", Name: "Just Chatting"}
	next, _ := m.openQuickCategory(cat, platform.Twitch)
	m = next.(Model)
	if m.Mode() != ModeChannels {
		t.Fatalf("modo = %s, quer canais", m.Mode())
	}
	if m.list.loading {
		t.Fatal("sem browser, loading deveria ser false")
	}
	if m.list.selectedCategory == nil || m.list.selectedCategory.ID != cat.ID {
		t.Fatalf("categoria = %+v", m.list.selectedCategory)
	}
}

// Um aviso aberto sobre o seletor fica no fim da área, como os outros
// overlays; só o próprio seletor vai para o topo.
func TestOverlaySobreOSeletorFicaNoFim(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+k")
	m.keymapNotice = "aviso-sobre-o-seletor"
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	row := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "aviso-sobre-o-seletor") })
	if row < 0 {
		t.Fatalf("aviso ausente:\n%s", strings.Join(lines, "\n"))
	}
	if row < len(lines)/2 {
		t.Fatalf("aviso na linha %d, quer na metade de baixo:\n%s", row, strings.Join(lines, "\n"))
	}
}

// Pelo seletor, uma ação da lista oculta é recusada como a tecla: o filtro não
// fica armado fora da tela.
func TestQuickOpenAcaoDaListaOcultaRecusada(t *testing.T) {
	m, _ := newScenario(t, 120, 40)
	m = play(t, m, "enter enter")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m.layout.showList, m.layout.showChat = false, false
	m.normalizeFocusAfterLayout()
	if m.listVisible() {
		t.Fatal("a lista continua visível no layout foco")
	}
	for _, id := range []actionID{actFilter, actSearch, actClearRecents, actSwitchPlatform} {
		next, _ := m.fireQuickAction(id)
		got := next.(Model)
		if got.list.filtering || got.list.search.active || got.list.confirmClear {
			t.Fatalf("%s executou com a lista oculta", id)
		}
		if got.ListPlatform() != m.ListPlatform() {
			t.Fatalf("%s trocou a plataforma com a lista oculta", id)
		}
	}
}

// No keymap classic, buscar pelo seletor também mostra a lista oculta.
func TestQuickOpenRevelaListaNoClassic(t *testing.T) {
	m, w := newScenario(t, 120, 40, WithKeymap(true, ""))
	m.layout.showList = false
	m.layout.showChat = true
	m.chat.panel.channel = "gaules"
	m.setFocus(FocusChat)
	m.normalizeFocusAfterLayout()
	m = play(t, m, "ctrl+k z e l d a enter")
	if !m.layout.showList {
		t.Fatal("busca pelo seletor não revelou a lista no classic")
	}
	if m.Focus() != FocusList {
		t.Fatalf("foco = %v, quer lista", m.Focus())
	}
	if len(w.searched) == 0 || w.searched[len(w.searched)-1] != "zelda" {
		t.Fatalf("buscas = %q, quer zelda", w.searched)
	}
}
