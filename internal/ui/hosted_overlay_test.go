package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHostedOverlayGivesTheVideoTheChatColumns(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.toggleChatOverlay()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 169, Visible: true, OverlayCols: 46}); got != want {
		t.Fatalf("sobreposto: %+v, quero %+v", got, want)
	}
	m.toggleChatOverlay()
	if got, want := m.hostedPanel(), (HostPanel{Row: 1, Col: 44, Rows: 48, Cols: 122, Visible: true}); got != want {
		t.Fatalf("de volta ao lado: %+v, quero %+v", got, want)
	}
}

func TestHostedOverlayChatLeavesVideoUncovered(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.chatOverlay = true
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
		m.chatOverlay = true
		for i, line := range strings.Split(m.frame(), "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("largura %d, linha %d com %d células", width, i, got)
			}
		}
	}
}

func TestHostedOverlayNeedsVisibleChat(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.showChat = false
	if cmd := m.toggleChatOverlay(); cmd != nil || m.chatOverlay || !m.statusNoticeWarns {
		t.Fatalf("chat oculto: overlay %v, notice %q", m.chatOverlay, m.statusNotice)
	}
}

func TestHostedOverlayWiderPanelGivesWayFirst(t *testing.T) {
	m := hostedModel(t, 150, 49, true)
	m.chatOverlay, m.hostedListCols, m.hostedChatCols = true, 90, 60
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
	if cmd := m.toggleChatOverlay(); cmd != nil || m.chatOverlay || m.statusNotice != m.t("chat.hidden_narrow_key") {
		t.Fatalf("janela estreita: overlay %v, notice %q", m.chatOverlay, m.statusNotice)
	}
}
