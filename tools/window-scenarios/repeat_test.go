//go:build cenarios && linux

package windowscenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Ao soltar Down, a rajada de repetições atrasadas não atravessa as categorias.
// Sem t.Parallel: a janela descarta repetições pela hora de chegada, e outra
// janela disputando a CPU espaçaria a rajada.
func TestJanelaSetaSoltaSemRajada(t *testing.T) {
	w := openWindow(t, setup{})
	if got := strings.TrimSpace(w.must("repeat-burst", "invalid 70")); got != "repeat-burst?" {
		t.Fatalf("argumento inválido respondeu %q", got)
	}
	if got := strings.TrimSpace(w.must("repeat-burst", "Down 70")); got != "ok" {
		t.Fatalf("rajada recusada: %q", got)
	}
	st := w.waitState("a seta na lista", 5*time.Second, func(s ui.StateReport) bool { return s.HighlightIndex > 0 })
	if st.TUI.HighlightIndex > 2 {
		t.Fatalf("rajada levou o seletor à posição %d", st.TUI.HighlightIndex)
	}
	// Ausência: nada anuncia que a seleção parou, então a conferência espera
	// uma janela de tempo real antes de ler o estado de novo.
	time.Sleep(200 * time.Millisecond)
	st, err := w.state()
	if err != nil {
		t.Fatal(err)
	}
	if st.TUI.HighlightIndex > 2 {
		t.Fatalf("seleção continuou até %d após soltar", st.TUI.HighlightIndex)
	}
}
