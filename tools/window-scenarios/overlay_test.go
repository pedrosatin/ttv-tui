//go:build cenarios && linux

package windowscenarios

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
	"strings"
	"testing"
	"time"
)

func TestJanelaAlturaEFundoDoChat(t *testing.T) {
	for _, backdrop := range []string{"panel", "text"} {
		t.Run(backdrop, func(t *testing.T) {
			t.Parallel()
			w := openWindow(t, setup{config: fmt.Sprintf("chat_overlay = true\nchat_overlay_rows = 60\nchat_overlay_backdrop = %q\n", backdrop), irc: fixture.IRCConfig{Messages: 3}})
			st := w.openFirstChannel()
			w.waitScreen(fixture.MessageText(3), 5*time.Second)
			rows := st.Window.Hole[3]
			expected := max(6, (rows*60+50)/100)
			if st.Window.OverlayRows != expected {
				t.Fatalf("altura %d, quero %d", st.Window.OverlayRows, expected)
			}
			// A célula à direita, no início do painel, não tem texto.
			x := (st.Window.Hole[0]-1+st.Window.Hole[2])*st.Window.CellW - 2
			y := (rows-expected)*st.Window.CellH + 2
			expect(t, func() error {
				r, g, b, err := w.fbpixel(x, y)
				if err != nil {
					return err
				}
				red := fixture.ClipLooksRight(r, g, b)
				if (backdrop == "text") != red {
					return fmt.Errorf("fundo %s: pixel %d,%d,%d", backdrop, r, g, b)
				}
				r, g, b, err = w.fbpixel(x, (rows-expected-1)*st.Window.CellH+2)
				if err != nil {
					return err
				}
				if !fixture.ClipLooksRight(r, g, b) {
					return fmt.Errorf("acima do painel %s: pixel %d,%d,%d", backdrop, r, g, b)
				}
				return nil
			})
			w.press("alt+Down")
			w.waitState("altura reduzida e aviso da tecla", 5*time.Second, func(s ui.StateReport) bool {
				return s.Config.ChatOverlayRows < 60 && strings.Contains(s.Notice, "Pg")
			})
			w.press("alt+PageUp")
			expect(t, func() error {
				cfg := w.savedConfig()
				for _, want := range []string{"chat_overlay_rows = ", "chat_overlay_height_keys_seen = true", "chat_overlay_opacity = 75"} {
					if !strings.Contains(cfg, want) {
						return fmt.Errorf("config sem %s", want)
					}
				}
				return nil
			})
		})
	}
}
