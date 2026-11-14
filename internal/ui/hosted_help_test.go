package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHostedHelpLiesInTheVideoColumn(t *testing.T) {
	for name, overlay := range map[string]bool{"chat ao lado": false, "chat sobreposto": true} {
		m := hostedModel(t, 212, 49, true)
		m.hosted.chatOverlay, m.layout.helpVisible = overlay, true
		frame := m.frame()
		if !strings.Contains(frame, m.t("help.title")) {
			t.Fatalf("%s: a ajuda não apareceu", name)
		}
		list, _, video := m.hostedWidths()
		for i, line := range strings.Split(frame, "\n") {
			if got := ansi.StringWidth(line); got > 212 {
				t.Fatalf("%s: linha %d com %d células", name, i, got)
			}
			if strings.Contains(line, m.t("help.title")) {
				plain := ansi.Strip(line)
				at := ansi.StringWidth(plain[:strings.Index(plain, "===")])
				if at <= list || at >= list+video {
					t.Fatalf("%s: título da ajuda na coluna %d", name, at)
				}
			}
		}
	}
}

// W13: sobre o vídeo, a ajuda tem fundo opaco atrás do texto, seja qual for a
// opacidade do chat, para o vídeo não atravessar as letras.
func TestHostedHelpIsOpaqueOverTheVideo(t *testing.T) {
	for _, opacity := range []int{10, 40, 70} {
		m := hostedModel(t, 212, 49, true)
		m.layout.helpVisible, m.hosted.chatOverlayOpacity = true, opacity
		if got := m.hostedPanel().OverlayOpacity; got != 100 {
			t.Fatalf("chat em %d%%: opacidade da ajuda %d, quer 100", opacity, got)
		}
		m.layout.helpVisible = false
		m.hosted.chatOverlay = true
		if got := m.hostedPanel().OverlayOpacity; got != opacity {
			t.Fatalf("a ajuda fechada mudou a opacidade do chat: %d, quer %d", got, opacity)
		}
	}
}

func TestHostedHelpWrapsToTheVideoColumn(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	_, _, video := m.hostedWidths()
	if got := m.helpTextWidth(); got != video-hostedHelpPadding {
		t.Fatalf("largura da ajuda %d, vídeo %d", got, video)
	}
}

func TestHostedHelpNeedsAUsableVideoColumn(t *testing.T) {
	m := hostedModel(t, 24, 30, false)
	m.layout.helpVisible = true
	for i, line := range strings.Split(m.frame(), "\n") {
		if got := ansi.StringWidth(line); got > 24 {
			t.Fatalf("linha %d com %d células", i, got)
		}
	}
	if !strings.Contains(m.frame(), m.t("help.title")) && m.helpTextWidth() == m.width {
		t.Fatal("sem coluna de vídeo a ajuda precisa aparecer na tela inteira")
	}
}
