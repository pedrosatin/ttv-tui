package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/components"
	"github.com/pedrosatin/ttv-tui/internal/kick"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Saídas do streamlink 8.6.2 emuladas pela versão em teste. O eco vem de
// main.py (formato "[{name}][{levelname}] {message}" de logger.py).
const (
	// Canal offline: eco da URL, linha de offline, saída 1.
	streamlinkEchoOffline = `#!/bin/sh
url=""
for a in "$@"; do
  case "$a" in
    http://*|https://*) if [ -z "$url" ]; then url="$a"; fi ;;
  esac
done
plugin=twitch
case "$url" in *kick.com*) plugin=kick ;; esac
echo "[cli][info] Found matching plugin $plugin for URL $url" >&2
echo "error: No playable streams found on this URL: $url" >&2
exit 1
`
	// Sessão que só ecoa a URL e termina com 0.
	streamlinkEchoExit0 = `#!/bin/sh
for a in "$@"; do
  case "$a" in
    http://*|https://*) echo "[cli][info] Found matching plugin twitch for URL $a" >&2; break ;;
  esac
done
echo "[cli][info] Stream ended" >&2
exit 0
`
	// Defeito da própria versão: traceback do Python, saída 1.
	streamlinkTraceback = `#!/bin/sh
echo "Traceback (most recent call last):" >&2
echo '  File "streamlink/plugins/twitch.py", line 12, in _get_streams' >&2
echo "KeyError: 'x'" >&2
exit 1
`
)

// TestTrialSurvivesChannelNameEcho reproduz o achado da auditoria: um canal
// cujo nome contém o nome de uma exceção Python, offline, aberto com uma versão
// do streamlink ainda em teste. O eco da URL não pode culpar a versão.
func TestTrialSurvivesChannelNameEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o streamlink falso é um script sh")
	}
	cases := []struct {
		name       string
		script     string
		ch         catalog.Channel
		state      player.State
		failure    player.Failure
		rolledBack bool
	}{
		{"twitch somechannel", streamlinkEchoOffline,
			catalog.Channel{Platform: platform.Twitch, Login: "somechannel"},
			player.StateError, player.FailureOffline, false},
		{"twitch keyerror", streamlinkEchoOffline,
			catalog.Channel{Platform: platform.Twitch, Login: "keyerror"},
			player.StateError, player.FailureOffline, false},
		{"twitch xtypeerrorx", streamlinkEchoOffline,
			catalog.Channel{Platform: platform.Twitch, Login: "xtypeerrorx"},
			player.StateError, player.FailureOffline, false},
		// As linhas do catálogo e da busca da Kick trazem a URL kick.com/<slug>.
		{"kick keyerror", streamlinkEchoOffline,
			catalog.Channel{Platform: platform.Kick, Login: "keyerror", URL: kick.DefaultBaseURL + "/keyerror"},
			player.StateError, player.FailureOffline, false},
		{"twitch keyerror sai com 0", streamlinkEchoExit0,
			catalog.Channel{Platform: platform.Twitch, Login: "keyerror"},
			player.StateEnded, player.FailureNone, false},
		// Controle positivo: um traceback real ainda desfaz a atualização.
		{"traceback real", streamlinkTraceback,
			catalog.Channel{Platform: platform.Twitch, Login: "somechannel"},
			player.StateError, player.FailureStreamlinkBroken, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, dataDir := trialBridgeWithStreamlink(t, c.script)

			done := make(chan player.Event, 1)
			go func() {
				for ev := range b.mgr.Events() {
					b.observePlayer(ev) // como forwardPlayerEvents
					if ev.State == player.StateError || ev.State == player.StateEnded {
						done <- ev
						return
					}
				}
			}()
			if msg, ok := b.StartTarget(ui.PlayTarget{Channel: c.ch}, 1)().(ui.PlayerStatusMsg); !ok || msg.State != ui.PlayerStateStarting {
				t.Fatalf("StartTarget = %+v, want starting", msg)
			}
			var ev player.Event
			select {
			case ev = <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("no terminal event")
			}
			b.bg.Wait()

			if ev.State != c.state || ev.Failure != c.failure {
				t.Fatalf("state = %s, failure = %q (line %q); want %s, %q", ev.State, ev.Failure, ev.LastLine, c.state, c.failure)
			}
			assertTrial(t, b, dataDir, c.rolledBack)
		})
	}
}

// Um erro síncrono de validação da URL repete a URL alvo; ele não culpa a
// versão em teste.
func TestTrialSurvivesInvalidURLWithExceptionName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o streamlink falso é um script sh")
	}
	for _, raw := range []string{"https://example.com/%zzkeyerror", "https://example.com/%zzKeyError:x"} {
		t.Run(raw, func(t *testing.T) {
			b, dataDir := trialBridgeWithStreamlink(t, streamlinkEchoOffline)
			msg, ok := b.StartTarget(ui.PlayTarget{Channel: catalog.Channel{URL: raw}}, 1)().(ui.PlayerStatusMsg)
			if !ok || msg.State != ui.PlayerStateError {
				t.Fatalf("StartTarget = %+v, want error", msg)
			}
			if msg.Failure == ui.PlayerFailure(player.FailureStreamlinkBroken) {
				t.Fatalf("failure = %q for %q: a URL error blames the build", msg.Failure, msg.LastLine)
			}
			b.bg.Wait()
			assertTrial(t, b, dataDir, false)
		})
	}
}

// trialBridgeWithStreamlink is onTrialBridge with script as the streamlink of
// the version on trial and an mpv that does nothing on PATH.
func trialBridgeWithStreamlink(t *testing.T, script string) (*playbackBridge, string) {
	t.Helper()
	b, dataDir := onTrialBridge(t)
	stub := t.TempDir()
	if err := os.WriteFile(filepath.Join(stub, "mpv"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stub)
	b.setRegistry(provider.NewRegistry(twitch.NewProvider(nil), kick.NewProvider(), provider.NewURLProvider()))
	if err := os.WriteFile(b.mgr.StreamlinkPath(), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if !b.mgr.UsesStreamlink() {
		t.Fatal("bridge manager does not use the default streamlink command")
	}
	return b, dataDir
}

// assertTrial checks the state of version 8.7.0, on trial, against the
// expected rollback.
func assertTrial(t *testing.T, b *playbackBridge, dataDir string, rolledBack bool) {
	t.Helper()
	st := components.LoadUpdateState(dataDir)
	if rolledBack {
		if !st.IsBad("8.7.0") || len(b.Rollbacks()) != 1 || managedDirExists(dataDir, "8.7.0") {
			t.Fatalf("state %+v, rollbacks %v: want 8.7.0 rolled back", st, b.Rollbacks())
		}
		return
	}
	if st.Trial != "8.7.0" || len(st.Bad) != 0 {
		t.Fatalf("state %+v: want 8.7.0 still on trial and nothing bad", st)
	}
	if len(b.Rollbacks()) != 0 || !managedDirExists(dataDir, "8.7.0") {
		t.Fatalf("rollbacks %v, 8.7.0 present %v: want no rollback", b.Rollbacks(), managedDirExists(dataDir, "8.7.0"))
	}
}
