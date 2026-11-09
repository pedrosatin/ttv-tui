package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type recordingHost struct{ panels []HostPanel }

func (h *recordingHost) SetPanel(p HostPanel) { h.panels = append(h.panels, p) }

// hostedModel is a model hosted in a window of the given size, with a chat panel.
func hostedModel(t *testing.T, width, height int, withChat bool) Model {
	t.Helper()
	m := Model{hosted: hostedState{host: &recordingHost{}}, layout: layoutState{showList: true, showChat: true}}
	m.width, m.height = width, height
	if withChat {
		m.chat.panel.channel = "canal"
	}
	if withChat != m.hasChatPanel() {
		t.Fatalf("hasChatPanel = %v, quero %v", m.hasChatPanel(), withChat)
	}
	return m
}

func TestHostedWidths(t *testing.T) {
	for _, tc := range []struct {
		name               string
		width              int
		hideList, hideChat bool
		wantList, wantChat int
		wantVideo          int
	}{
		{name: "janela cheia", width: 212, wantList: 42, wantChat: 46, wantVideo: 122},
		{name: "limites máximos", width: 300, wantList: 44, wantChat: 50, wantVideo: 204},
		{name: "cabe em 80 colunas", width: 80, wantList: 22, wantChat: 24, wantVideo: 32},
		{name: "sem lista o vídeo cresce à esquerda", width: 212, hideList: true, wantChat: 46, wantVideo: 165},
		{name: "sem chat o vídeo cresce à direita", width: 212, hideChat: true, wantList: 42, wantVideo: 169},
		{name: "sem os dois o vídeo ocupa tudo", width: 212, hideList: true, hideChat: true, wantVideo: 212},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := hostedModel(t, tc.width, 49, true)
			m.layout.showList, m.layout.showChat = !tc.hideList, !tc.hideChat
			list, chat, video := m.hostedWidths()
			if list != tc.wantList || chat != tc.wantChat || video != tc.wantVideo {
				t.Fatalf("larguras = %d, %d, %d; quero %d, %d, %d", list, chat, video, tc.wantList, tc.wantChat, tc.wantVideo)
			}
			used := list + chat + video
			if list > 0 {
				used += hostedSepWidth
			}
			if chat > 0 {
				used += hostedSepWidth
			}
			if used != tc.width {
				t.Fatalf("as colunas somam %d em uma tela de %d", used, tc.width)
			}
		})
	}
}

func TestHostedPanelFollowsTheToggles(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 122, Visible: true}); got != want {
		t.Fatalf("três colunas: %+v, quero %+v", got, want)
	}
	m.toggleListPanel()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 1, Rows: 48, Cols: 165, Visible: true}); got != want {
		t.Fatalf("sem lista: %+v, quero %+v", got, want)
	}
	m.toggleChatPanel()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 1, Rows: 48, Cols: 212, Visible: true}); got != want {
		t.Fatalf("sem lista e sem chat: %+v, quero %+v", got, want)
	}
	m.toggleListPanel()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 169, Visible: true}); got != want {
		t.Fatalf("lista de volta, sem chat: %+v, quero %+v", got, want)
	}
}

func TestHostedPanelCarriesTheHelpOverTheVideo(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.helpVisible = true
	want := HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 122, Visible: true, OverlayCols: 122, OverlayOpacity: 70}
	if got := m.hostedPanel(); got != want {
		t.Fatalf("com a ajuda aberta: %+v, quero %+v", got, want)
	}
}

func TestHostedPanelGivesWayToTheFilterMenu(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.list.prefs.menuOpen = true
	if got := m.hostedPanel(); got != (HostPanel{}) {
		t.Fatalf("com o menu de filtros aberto: %+v", got)
	}
}

func TestHostedPublishesOnlyChanges(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	host := m.hosted.host.(*recordingHost)
	m = m.syncHostPanel()
	m = m.syncHostPanel()
	if len(host.panels) != 1 {
		t.Fatalf("publicou %d vezes sem mudança: %+v", len(host.panels), host.panels)
	}
	m.toggleChatPanel()
	m = m.syncHostPanel()
	if len(host.panels) != 2 || host.panels[1].Cols != 169 {
		t.Fatalf("depois de esconder o chat: %+v", host.panels)
	}
	_ = m
}

func TestHostedColumnsDoNotOverflow(t *testing.T) {
	for _, width := range []int{40, 79, 80, 120, 212} {
		m := hostedModel(t, width, 30, true)
		for i, line := range strings.Split(m.frame(), "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("largura %d, linha %d com %d células: %q", width, i, got, ansi.Strip(line))
			}
		}
	}
}

func TestHostedSkipsWindowNotice(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	if m.hostedIdleText() == "" {
		t.Fatal("sem reprodução a coluna do vídeo precisa dizer o que fazer")
	}
	m.player.state = PlayerStatePlaying
	if m.hostedIdleText() != "" {
		t.Fatal("tocando, a coluna do vídeo fica em branco para a janela desenhar")
	}
}

func TestHostedResizeMovesTheDividers(t *testing.T) {
	m := hostedModel(t, 212, 49, true)

	m.resizeHostedList(hostedResizeStep)
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 46, Rows: 48, Cols: 120, Visible: true}); got != want {
		t.Fatalf("lista mais larga: %+v, quero %+v", got, want)
	}
	m.resizeHostedChat(hostedResizeStep)
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 46, Rows: 48, Cols: 118, Visible: true}); got != want {
		t.Fatalf("chat mais largo: %+v, quero %+v", got, want)
	}
	m.resizeHostedChat(-2 * hostedResizeStep)
	if got := m.hostedPanel().Cols; got != 122 {
		t.Fatalf("chat mais estreito: vídeo com %d colunas, quero 122", got)
	}
}

func TestHostedResizeStopsAtTheLimits(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	for range 200 {
		m.resizeHostedList(-hostedResizeStep)
		m.resizeHostedChat(-hostedResizeStep)
	}
	if list, chat, _ := m.hostedWidths(); list != hostedMinList || chat != hostedMinChat {
		t.Fatalf("mínimos: lista %d, chat %d", list, chat)
	}
	for range 200 {
		m.resizeHostedList(hostedResizeStep)
	}
	if _, _, video := m.hostedWidths(); video != hostedMinVideo {
		t.Fatalf("a lista não pode esmagar o vídeo: sobraram %d colunas", video)
	}
	listAtMax, _, _ := m.hostedWidths()
	m.resizeHostedChat(hostedResizeStep)
	if list, chat, video := m.hostedWidths(); list != listAtMax || chat != hostedMinChat || video != hostedMinVideo {
		t.Fatalf("sem espaço o chat não cresce: %d, %d, %d", list, chat, video)
	}
}

func TestHostedSavedWidthsGiveWayInSmallWindows(t *testing.T) {
	m := hostedModel(t, 106, 49, true)
	m.hosted.listCols, m.hosted.chatCols = 90, 60
	list, chat, video := m.hostedWidths()
	if video != hostedMinVideo || list+chat+video+2*hostedSepWidth != 106 {
		t.Fatalf("janela pequena: %d, %d, %d", list, chat, video)
	}
	if list < hostedMinList || chat < hostedMinChat {
		t.Fatalf("abaixo do mínimo: %d, %d", list, chat)
	}
	m.width = 212
	if list, chat, _ := m.hostedWidths(); list != 90 || chat != 60 {
		t.Fatalf("janela de volta: quero os tamanhos pedidos, veio %d, %d", list, chat)
	}
}

func TestHostedResizeOfHiddenPanelWarns(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.showList, m.layout.showChat = false, false
	if cmd := m.resizeHostedList(hostedResizeStep); cmd != nil || !m.notices.statusWarns || m.hosted.listCols != 0 {
		t.Fatalf("lista oculta: notice %q, cols %d", m.notices.status, m.hosted.listCols)
	}
	m.notices.status, m.notices.statusWarns = "", false
	if cmd := m.resizeHostedChat(hostedResizeStep); cmd != nil || !m.notices.statusWarns || m.hosted.chatCols != 0 {
		t.Fatalf("chat oculto: notice %q, cols %d", m.notices.status, m.hosted.chatCols)
	}
}

func TestHostedSaveKeepsOnlyTheLastChange(t *testing.T) {
	var saved [][2]int
	m := hostedModel(t, 212, 49, true)
	m.hosted.persist = func(list, chat int, _, _ bool, _ int) error {
		saved = append(saved, [2]int{list, chat})
		return nil
	}
	m.resizeHostedList(hostedResizeStep)
	first := m.hosted.saveGen
	m.resizeHostedList(hostedResizeStep)
	if cmd := m.saveHostedColumns(first); cmd != nil {
		t.Fatal("uma mudança mais nova cancela o salvamento antigo")
	}
	cmd := m.saveHostedColumns(m.hosted.saveGen)
	if cmd == nil {
		t.Fatal("a última mudança precisa ser salva")
	}
	cmd()
	if len(saved) != 1 || saved[0] != [2]int{46, 0} {
		t.Fatalf("salvou %v", saved)
	}
}

func TestHostedResizeKeysOnlyInTheHostedWindow(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.keys = defaultKeyMap()
	right := tea.KeyMsg{Type: tea.KeyCtrlRight}
	if handled, _ := m.handleHostedResizeKey(right); !handled || m.hosted.listCols != 44 {
		t.Fatalf("Ctrl+→ na janela: handled %v, cols %d", handled, m.hosted.listCols)
	}
	alt := tea.KeyMsg{Type: tea.KeyLeft, Alt: true}
	if handled, _ := m.handleHostedResizeKey(alt); !handled || m.hosted.chatCols != 48 {
		t.Fatalf("Alt+← na janela: handled %v, cols %d", handled, m.hosted.chatCols)
	}
	m.hosted.host = nil
	if handled, _ := m.handleHostedResizeKey(right); handled {
		t.Fatal("num terminal comum as teclas seguem livres")
	}
}

func TestHostedListTakesFocusWhenItComesBack(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.focus = FocusList
	m.toggleListPanel()
	if m.layout.focus != FocusChat {
		t.Fatalf("lista oculta: foco %v, quero o chat", m.layout.focus)
	}
	m.toggleListPanel()
	if m.layout.focus != FocusList {
		t.Fatalf("lista de volta: foco %v, quero a lista", m.layout.focus)
	}
}
