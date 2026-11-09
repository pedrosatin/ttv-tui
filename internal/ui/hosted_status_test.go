package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHostedStatusHidesRawPlayerLog(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.player.state = PlayerStatePlaying
	m.player.infoLine = "[plugins.twitch][info] Will skip ad segments"
	if bar := m.StatusBar(); strings.Contains(bar, "plugins.twitch") {
		t.Fatalf("log cru na barra: %q", bar)
	}
}

func TestHostedHelpDoesNotOfferTheTerminalVideo(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.layout.helpVisible = true
	help := strings.Join(m.renderHelpLines(400), "\n")
	if strings.Contains(help, m.t("help.video_enable")) {
		t.Fatal("a ajuda diz que o vídeo está desligado com o vídeo na janela")
	}
	layout := strings.Index(help, m.t("help.layout"))
	resize := strings.Index(help, m.t("help.hosted_resize"))
	if layout < 0 || resize != layout+len(m.t("help.layout"))+3 {
		t.Fatalf("os atalhos da janela devem vir logo depois do layout: %d, %d", layout, resize)
	}
}

func TestHostedNoticeExpiresOnlyIfStillShown(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.notices.status = "aviso"
	next, _ := m.update(hostedNoticeMsg{text: "outro"})
	if next.(Model).notices.status != "aviso" {
		t.Fatal("expirou um aviso que já era outro")
	}
	next, _ = m.update(hostedNoticeMsg{text: "aviso"})
	if next.(Model).notices.status != "" {
		t.Fatal("o aviso devia sair e devolver o estado da reprodução à barra")
	}
}

var _ tea.Msg = hostedNoticeMsg{}
