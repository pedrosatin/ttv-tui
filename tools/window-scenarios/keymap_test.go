//go:build cenarios && linux

package windowscenarios

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/ui"
	"strings"
	"testing"
	"time"
)

func TestJanelaMapaLogico(t *testing.T) {
	w := openWindow(t, setup{})
	w.openFirstChannel()
	w.press("ctrl+w")
	w.waitScreen("Ctrl+W h", 5*time.Second)
	w.press("Escape")
	w.press("ctrl+w", "h")
	w.waitState("lista escondida pelo prefixo", 5*time.Second, func(s ui.StateReport) bool { return s.Layout == "chat" })
	w.press("ctrl+b")
	w.waitState("lista recuperada pelo alias", 5*time.Second, func(s ui.StateReport) bool { return s.Layout == "split" })
	w.press("ctrl+w", "x", "ctrl+w", "h")
	w.waitState("h acompanha o painel da esquerda", 5*time.Second, func(s ui.StateReport) bool { return s.Layout == "list" })
	w.press("ctrl+w", "h", "ctrl+w", "o", "ctrl+w", "-")
	w.waitState("altura pelo prefixo", 5*time.Second, func(s ui.StateReport) bool { return s.Config.ChatOverlayRows > 0 && s.Config.ChatOverlay })
	// O Ctrl+O continua apelido de Ctrl+W o no mapa padrão.
	w.press("ctrl+o")
	w.waitState("chat ao lado pelo Ctrl+O", 5*time.Second, func(s ui.StateReport) bool { return !s.Config.ChatOverlay })
}

func TestJanelaMapaClassicEMigracao(t *testing.T) {
	w := openWindow(t, setup{config: "language = \"pt-BR\"\n"})
	w.waitScreen("classic", 5*time.Second)
	expect(t, func() error {
		if !strings.Contains(w.savedConfig(), "keymap_notice_seen = true") {
			return fmt.Errorf("migração não registrada")
		}
		return nil
	})
	// A abertura seguinte usa o config salvo e não repete o aviso.
	saved := w.savedConfig()
	w.close()
	next := openWindow(t, setup{config: saved})
	if strings.Contains(strings.Join(next.screen(), "\n"), "classic") {
		t.Fatal("aviso repetido")
	}
	next.close()
	classic := openWindow(t, setup{config: "keymap = \"classic\"\n"})
	classic.openFirstChannel()
	classic.press("ctrl+h")
	classic.waitState("classic esconde lista", 5*time.Second, func(s ui.StateReport) bool { return s.Layout == "chat" })
	classic.press("ctrl+h", "ctrl+w")
	classic.waitState("classic cicla sem prefixo", 5*time.Second, func(s ui.StateReport) bool { return s.Layout == "chat" })
}
