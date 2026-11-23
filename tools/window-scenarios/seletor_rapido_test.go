//go:build cenarios && linux

package windowscenarios

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Ctrl+K abre o seletor rápido sobre a tela da janela e mostra o título e o
// campo; Esc fecha e devolve a lista de categorias.
func TestJanelaSeletorRapidoCtrlK(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{})
	w.waitScreen("Categoria 01", 5*time.Second)

	w.press("ctrl+k")
	w.waitScreen("Seletor rápido", 5*time.Second)
	expect(t, func() error {
		screen := strings.Join(w.screen(), "\n")
		if !strings.Contains(screen, "Seletor:") {
			return fmt.Errorf("campo do seletor ausente:\n%s", screen)
		}
		return nil
	})

	w.press("Escape")
	expect(t, func() error {
		screen := strings.Join(w.screen(), "\n")
		if strings.Contains(screen, "Seletor rápido") {
			return fmt.Errorf("seletor ainda na tela depois do Esc:\n%s", screen)
		}
		if rowWith(w.screen(), "Categoria 01") < 0 {
			return fmt.Errorf("categorias não voltaram:\n%s", screen)
		}
		return nil
	})
}

// Com o vídeo tocando, Ctrl+K abre o seletor e o painel de vídeo cede a tela.
func TestJanelaSeletorRapidoComVideoTocando(t *testing.T) {
	t.Parallel()
	w := openWindow(t, setup{})
	w.waitScreen("Categoria 01", 5*time.Second)
	w.openFirstChannel()

	w.press("ctrl+k")
	w.waitScreen("Seletor rápido", 5*time.Second)
	expect(t, func() error {
		screen := strings.Join(w.screen(), "\n")
		if !strings.Contains(screen, "Seletor:") {
			return fmt.Errorf("campo do seletor ausente com vídeo:\n%s", screen)
		}
		st, err := w.state()
		if err != nil {
			return err
		}
		if st.Window.HoleOn {
			return fmt.Errorf("buraco do vídeo ainda aberto com o seletor: %+v", st.Window)
		}
		return nil
	})

	w.press("Escape")
	expect(t, func() error {
		screen := strings.Join(w.screen(), "\n")
		if strings.Contains(screen, "Seletor rápido") {
			return fmt.Errorf("seletor ainda na tela depois do Esc com vídeo:\n%s", screen)
		}
		return nil
	})
}
