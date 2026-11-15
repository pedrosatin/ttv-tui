//go:build cenarios && linux

package windowscenarios

import (
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Ao soltar Down, a rajada de repetições atrasadas não atravessa as categorias.
func TestJanelaSetaSoltaSemRajada(t *testing.T) {
	w := openWindow(t, setup{})
	w.must("repeat-burst", "Down 70")
	st := w.waitState("a seta na lista", 5*time.Second, func(s ui.StateReport) bool { return s.HighlightIndex > 0 })
	if st.TUI.HighlightIndex > 2 {
		t.Fatalf("rajada levou o seletor à posição %d", st.TUI.HighlightIndex)
	}
	time.Sleep(200 * time.Millisecond)
	st, err := w.state()
	if err != nil {
		t.Fatal(err)
	}
	if st.TUI.HighlightIndex > 2 {
		t.Fatalf("seleção continuou até %d após soltar", st.TUI.HighlightIndex)
	}
}
