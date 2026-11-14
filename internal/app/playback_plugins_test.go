package app

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/kick"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Só a reprodução da Kick leva o plugin do filtro de anúncio ao player; a
// Twitch e a URL genérica tocam com os plugins do streamlink.
func TestPlaybackBridgeHandsKickPluginOnlyToKick(t *testing.T) {
	tests := []struct {
		name       string
		ch         catalog.Channel
		wantPlugin bool
	}{
		{"Kick salvo", catalog.Channel{Platform: platform.Kick, Login: "xqc"}, true},
		{"Kick colado", catalog.Channel{URL: "https://kick.com/xqc"}, true},
		{"Twitch", catalog.Channel{Platform: platform.Twitch, Login: "gaules"}, false},
		{"URL genérica", catalog.Channel{URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started player.Target
			mgr := player.New(player.Options{
				RuntimeDir: t.TempDir(),
				StopWait:   100 * time.Millisecond,
				Command: func(target player.Target, _ string) *exec.Cmd {
					started = target
					return exec.Command(os.Args[0], "-test.run=^$")
				},
			})
			defer mgr.Close()

			bridge := newPlaybackBridge(mgr)
			bridge.setRegistry(provider.NewRegistry(twitch.NewProvider(nil), kick.NewProvider(), provider.NewURLProvider()))
			msg, ok := bridge.StartTarget(ui.PlayTarget{Channel: tt.ch}, 1)().(ui.PlayerStatusMsg)
			if !ok || msg.State == ui.PlayerStateError {
				t.Fatalf("StartTarget = %+v", msg)
			}
			if !tt.wantPlugin {
				if len(started.Plugins) != 0 {
					t.Fatalf("plugins = %d, want none", len(started.Plugins))
				}
				return
			}
			if len(started.Plugins) != 1 || started.Plugins[0].Name != "kick" || len(started.Plugins[0].Source) == 0 {
				t.Fatalf("plugins = %+v, want the kick plugin", started.Plugins)
			}
		})
	}
}
