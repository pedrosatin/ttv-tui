package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHostedOverlayGivesTheVideoTheChatColumns(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.toggleChatOverlay()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 47, Cols: 169, Visible: true, OverlayCols: 46, OverlayOpacity: 70, OverlayRows: 47}); got != want {
		t.Fatalf("sobreposto: %+v, quero %+v", got, want)
	}
	m.toggleChatOverlay()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 47, Cols: 122, Visible: true}); got != want {
		t.Fatalf("de volta ao lado: %+v, quero %+v", got, want)
	}
}

func TestHostedOverlayChatLeavesVideoUncovered(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.hosted.chatOverlay = true
	for range 200 {
		m.resizeHostedChat(hostedResizeStep)
	}
	_, chat, video := m.hostedWidths()
	if video != 169 || video-chat != hostedMinVideo {
		t.Fatalf("chat %d, vídeo %d: quero %d colunas descobertas", chat, video, hostedMinVideo)
	}
}

func TestHostedOverlayRowsFitTheWindow(t *testing.T) {
	for _, width := range []int{80, 120, 212} {
		m := hostedModel(t, width, 30, true)
		m.hosted.chatOverlay = true
		for i, line := range strings.Split(m.frame(), "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("largura %d, linha %d com %d células", width, i, got)
			}
		}
	}
}

func TestHostedOverlayNeedsVisibleChat(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.showChat = false
	if cmd := m.toggleChatOverlay(); cmd != nil || m.hosted.chatOverlay || !m.notices.statusWarns {
		t.Fatalf("chat oculto: overlay %v, notice %q", m.hosted.chatOverlay, m.notices.status)
	}
}

func TestHostedOverlayWiderPanelGivesWayFirst(t *testing.T) {
	m := hostedModel(t, 150, 49, true)
	m.hosted.chatOverlay, m.hosted.listCols, m.hosted.chatCols = true, 90, 60
	list, chat, video := m.hostedWidths()
	if video-chat != hostedMinVideo || list+hostedSepWidth+video != 150 {
		t.Fatalf("lista %d, chat %d, vídeo %d", list, chat, video)
	}
	if list > chat+1 || chat < 58 {
		t.Fatalf("a lista, mais larga, cede antes: lista %d, chat %d", list, chat)
	}
}

func TestHostedOverlayWarnsAboutNarrowWindow(t *testing.T) {
	m := hostedModel(t, 70, 49, true)
	if cmd := m.toggleChatOverlay(); cmd != nil || m.hosted.chatOverlay || m.notices.status != m.t("chat.hidden_narrow_key") {
		t.Fatalf("janela estreita: overlay %v, notice %q", m.hosted.chatOverlay, m.notices.status)
	}
}

func TestHostedOverlayFitsAt79Columns(t *testing.T) {
	m := hostedModel(t, 79, 24, true)
	m.toggleChatOverlay()
	if !m.hosted.chatOverlay || !m.chatColumnVisible() {
		t.Fatalf("overlay recusado em 79 colunas: %q", m.notices.status)
	}
	_, chat, video := m.hostedWidths()
	if chat < hostedMinChat || video-chat < hostedMinVideo {
		t.Fatalf("chat %d, vídeo %d", chat, video)
	}
	m.toggleChatOverlay()
	if m.hosted.chatOverlay {
		t.Fatal("overlay não desligou em 79 colunas")
	}
	m = hostedModel(t, 70, 24, true)
	m.layout.showList = false
	m.toggleChatOverlay()
	if !m.hosted.chatOverlay {
		t.Fatal("overlay recusado sem lista")
	}
}

func TestHostedOverlayReducedPageKeys(t *testing.T) {
	m := scrollFixture(t, "en", 40)
	m.hosted.host = hostedModel(t, 120, 24, true).hosted.host
	m.hosted.chatOverlay = true
	m.hosted.overlayPreferences.Rows = 40
	// 22 body rows * 40% = 9, minus the header (two rows at this width) and
	// the input.
	const budget = 6
	if got := m.chatPageSize(); got != budget {
		t.Errorf("page size %d, want %d", got, budget)
	}
	m = play(t, m, "pgup")
	if got := m.chat.panel.bottomIdx; got != 39-budget {
		t.Fatalf("PgUp bottom %d, want %d", got, 39-budget)
	}
	m = play(t, m, "pgup pgdown")
	if got := m.chat.panel.bottomIdx; got != 39-budget {
		t.Fatalf("PgDn bottom %d, want %d", got, 39-budget)
	}
	m = play(t, m, "pgdown")
	if !m.ChatFollowLive() {
		t.Fatal("PgDn did not return to live")
	}
	m = play(t, m, "home")
	if got := m.chat.panel.bottomIdx; got != budget-1 {
		t.Fatalf("Home bottom %d, want %d", got, budget-1)
	}
	m.hosted.overlayPreferences.Rows = 1
	m.height = 3
	if got := m.chatPageSize(); got != 1 {
		t.Fatalf("minimum page %d", got)
	}
}

// O usuário liga o chat sobreposto com a lista visível em uma janela de 79 colunas.
func TestCenarioOverlay79Colunas(t *testing.T) {
	m, _ := hostedScenarioClassic(t, 79, 24)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	if !m.hosted.chatOverlay {
		t.Fatal("overlay recusado em 79 colunas")
	}
	assertGolden(t, "chat-sobreposto-79x24", screen(m))
}

// O usuário lê a primeira página de mensagens com o painel sobreposto reduzido.
func TestCenarioOverlayHomeReduzido(t *testing.T) {
	m, _ := hostedScenarioClassic(t, 120, 40)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	m = withMessages(t, m, 40)
	m.hosted.overlayPreferences.Rows = 40
	m = play(t, m, "tab home")
	if m.chat.panel.bottomIdx != 11 {
		t.Fatalf("Home bottom %d", m.chat.panel.bottomIdx)
	}
	assertGolden(t, "chat-sobreposto-home-120x40", screen(m))
}

// O usuário abre um canal sem chat e vê o aviso junto da entrada do overlay.
func TestCenarioOverlaySemChat(t *testing.T) {
	m, _ := hostedScenarioClassic(t, 120, 40)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	m.chat.panel.unsupported = true
	m.hosted.overlayPreferences.Rows = 40
	lines := m.renderChatLines(15, 30)
	if !strings.Contains(lines[len(lines)-2], TruncateVisual("  "+m.t("chat.unsupported"), 30)) {
		t.Fatal("aviso distante da entrada")
	}
	assertGolden(t, "chat-sobreposto-sem-chat-120x40", screen(m))
}
