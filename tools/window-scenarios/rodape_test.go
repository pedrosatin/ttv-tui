//go:build cenarios && linux

package windowscenarios

import (
	"fmt"
	"strings"
	"testing"
)

func TestJanelaTeclasSemCanalAvisam(t *testing.T) {
	w := openWindow(t, setup{})
	for _, key := range []string{"i", "Tab", "ctrl+t", "o"} {
		if key == "o" {
			w.press("ctrl+w", "o")
		} else {
			w.press(key)
		}
		expect(t, func() error {
			if !strings.Contains(statusBar(w.screen()), "Abra um canal") {
				return fmt.Errorf("%s sem aviso na barra: %q", key, statusBar(w.screen()))
			}
			return nil
		})
	}
	for _, width := range []int{80, 120} {
		w.resizeCols(width)
		expect(t, func() error {
			rows := w.screen()
			footer := rows[len(rows)-2]
			for _, text := range []string{"Enter abrir", "f ★", "/ filtrar", "Ctrl+F buscar", "? ajuda"} {
				if !strings.Contains(footer, text) {
					return fmt.Errorf("rodapé em %d perdeu %q: %q", width, text, footer)
				}
			}
			return nil
		})
	}

}
