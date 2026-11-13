package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Os nomes de canal que a auditoria usou para disparar o rollback: cada um
// contém o nome de uma exceção Python em minúsculas.
var suspiciousTargets = []struct {
	name   string
	target Target
}{
	{"twitch keyerror", testTarget("keyerror")},
	{"twitch xtypeerrorx", testTarget("xtypeerrorx")},
	{"twitch attributeerror", testTarget("attributeerror")},
	{"twitch link colado", Target{URL: "https://www.twitch.tv/keyerror?sr=a", Label: "keyerror"}},
	{"kick keyerror", Target{URL: "https://kick.com/keyerror", Label: "kick:keyerror"}},
}

func TestClassifyLineIgnoresThirdPartyText(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Failure
	}{
		{"eco da URL em info", "[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/keyerror", FailureNone},
		{"eco da URL Kick em info", "[cli][info] Found matching plugin kick for URL https://kick.com/keyerror", FailureNone},
		{"debug com nome de exceção", "[plugins.twitch][debug] KeyError: 'x' while reading the channel", FailureNone},
		{"info que cita offline", "[plugins.kick][info] channel is offline? keyerror", FailureNone},
		{"login em linha de erro", "error: something failed for https://www.twitch.tv/xtypeerrorx", FailureNone},
		{"nome em minúsculas com dois-pontos", "error: keyerror: not a python exception", FailureNone},
		{"nome sozinho sem traceback", "KeyError", FailureNone},
		{"exceção no formato do Python", "KeyError: 'data'", FailureStreamlinkBroken},
		{"exceção qualificada", "streamlink.exceptions.PluginError: Unable to parse manifest", FailureStreamlinkBroken},
		{"exceção derivada", "streamlink.exceptions.FatalPluginError: gone", FailureStreamlinkBroken},
		{"exceção em linha de erro", "[plugins.twitch][error] TypeError: 'NoneType' object is not subscriptable", FailureStreamlinkBroken},
		{"warning conta", "[stream.hls][warning] Reloading failed: Unable to open URL: https://x/p.m3u8 (404 Client Error: Not Found for url: https://x/p.m3u8)", FailureNetwork},
		{"offline com eco da exceção no nome", "error: No playable streams found on this URL: https://www.twitch.tv/keyerror", FailureOffline},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyLine(c.line); got != c.want {
				t.Fatalf("ClassifyLine(%q) = %q, want %q", c.line, got, c.want)
			}
		})
	}
}

func TestClassifyTargetLineMasksTarget(t *testing.T) {
	kick := Target{URL: "https://kick.com/KeyError:x", Label: "kick:KeyError:x"}
	cases := []struct {
		name   string
		target Target
		line   string
		want   Failure
	}{
		{"URL alvo com exceção no formato do Python", Target{URL: "https://example.com/KeyError:x.m3u8"},
			"error: something failed for https://example.com/KeyError:x.m3u8", FailureNone},
		{"URL alvo com outra caixa", Target{URL: "https://example.com/KeyError:x.m3u8"},
			"error: something failed for HTTPS://EXAMPLE.COM/KeyError:x.m3u8", FailureNone},
		{"identidade num caminho de API", kick,
			"error: Error while querying Kick API for /api/v2/channels/KeyError:x", FailureNone},
		{"identidade num repr do Python", Target{URL: "https://www.twitch.tv/x", Label: "TypeError:"},
			"error: plugin failed with 'TypeError:'", FailureNone},
		// A identidade solta no texto não é trocada: um login "streams" não pode
		// apagar a frase de offline.
		{"identidade que é palavra da frase", testTarget("streams"),
			"error: No playable streams found on this URL: https://www.twitch.tv/streams", FailureOffline},
		{"identidade que é palavra da frase de rede", testTarget("open"),
			"error: Unable to open URL: https://gql.twitch.tv/gql (Max retries exceeded)", FailureNetwork},
		{"exceção real ao lado da URL alvo", testTarget("keyerror"),
			"KeyError: 'https://www.twitch.tv/keyerror'", FailureStreamlinkBroken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyTargetLine(c.line, c.target); got != c.want {
				t.Fatalf("ClassifyTargetLine(%q, %+v) = %q (mascarada %q), want %q",
					c.line, c.target, got, maskTarget(c.line, c.target), c.want)
			}
		})
	}
}

// O eco da URL que o streamlink imprime antes da linha de offline não pode
// culpar a versão, para nenhum dos nomes da auditoria.
func TestEchoedTargetOfflineIsOffline(t *testing.T) {
	for _, c := range suspiciousTargets {
		t.Run(c.name, func(t *testing.T) {
			mgr := newTestManager(t, "echo-offline", Options{SocketWait: 30 * time.Second})
			gen, err := mgr.Start(c.target)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			ev := waitError(t, mgr, gen, 5*time.Second)
			if ev.Failure != FailureOffline {
				t.Fatalf("Failure = %q (line %q), want offline", ev.Failure, ev.LastLine)
			}
		})
	}
}

// Uma sessão que só imprime o eco e sai com 0 termina como encerrada.
func TestEchoedTargetWithExitZeroEnds(t *testing.T) {
	for _, c := range suspiciousTargets {
		t.Run(c.name, func(t *testing.T) {
			mgr := newTestManager(t, "echo-exit0", Options{SocketWait: 30 * time.Second})
			gen, err := mgr.Start(c.target)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			ev := waitTerminal(t, mgr, gen, 5*time.Second)
			if ev.State != StateEnded || ev.Failure != FailureNone {
				t.Fatalf("state = %s, failure = %q (line %q); want ended without failure", ev.State, ev.Failure, ev.LastLine)
			}
		})
	}
}

func TestLaterFatalLineReplacesWeakerClass(t *testing.T) {
	mgr := newTestManager(t, "weak-then-offline", Options{SocketWait: 30 * time.Second})
	gen, err := mgr.Start(testTarget("keyerror"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := waitError(t, mgr, gen, 5*time.Second)
	if ev.Failure != FailureOffline {
		t.Fatalf("Failure = %q (line %q), want offline", ev.Failure, ev.LastLine)
	}
	if !strings.Contains(ev.LastLine, "No playable streams found") {
		t.Fatalf("LastLine = %q, want the offline line", ev.LastLine)
	}
}

// Controle positivo: um traceback de verdade ainda culpa a versão, e a classe
// vem da linha da exceção (a sessão sai com 0, sem o status de erro como pista).
func TestRealTracebackBlamesTheBuild(t *testing.T) {
	cases := []struct {
		mode  string
		want  Failure
		state State
	}{
		{"traceback-exit0", FailureStreamlinkBroken, StateError},
		{"traceback-bare-exit0", FailureStreamlinkBroken, StateError},
		{"bare-name-exit0", FailureNone, StateEnded},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			mgr := newTestManager(t, c.mode, Options{SocketWait: 30 * time.Second})
			gen, err := mgr.Start(testTarget("keyerror"))
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			ev := waitTerminal(t, mgr, gen, 5*time.Second)
			if ev.State != c.state || ev.Failure != c.want {
				t.Fatalf("state = %s, failure = %q (line %q); want %s, %q", ev.State, ev.Failure, ev.LastLine, c.state, c.want)
			}
			if c.want == FailureStreamlinkBroken && !strings.Contains(ev.LastLine, "KeyError") {
				t.Fatalf("LastLine = %q, want the exception line", ev.LastLine)
			}
		})
	}
}

// Um erro síncrono de validação repete a URL alvo; o texto não classifica.
func TestStartValidationErrorDoesNotBlameTheBuild(t *testing.T) {
	for _, raw := range []string{"https://www.twitch.tv/%zzkeyerror", "https://www.twitch.tv/%zzKeyError:x"} {
		t.Run(raw, func(t *testing.T) {
			mgr := newTestManager(t, "play", Options{})
			_, err := mgr.Start(Target{URL: raw})
			if err == nil {
				t.Fatal("Start must reject the URL")
			}
			if got := FailureOf(err); got != FailureUnknown {
				t.Fatalf("FailureOf(%v) = %q, want unknown", err, got)
			}
		})
	}
}

func TestFailureOfClassifiesOnlyTypes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Failure
	}{
		{"texto com nome de exceção", errors.New("player: invalid stream URL: KeyError: x"), FailureUnknown},
		{"texto com offline", errors.New("player: No playable streams found"), FailureUnknown},
		{"texto com exec format error", errors.New("player: exec format error"), FailureUnknown},
		{"exec com formato inválido", &ExecError{Err: &os.PathError{Op: "fork/exec", Path: "/x/AppRun", Err: syscall.ENOEXEC}}, FailureStreamlinkBroken},
		{"exec embrulhado", fmt.Errorf("start: %w", &ExecError{Err: &os.PathError{Op: "fork/exec", Path: "/x/AppRun", Err: syscall.ENOEXEC}}), FailureStreamlinkBroken},
		{"exec sem permissão", &ExecError{Err: &os.PathError{Op: "fork/exec", Path: "/x/AppRun", Err: syscall.EACCES}}, FailureUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FailureOf(c.err); got != c.want {
				t.Fatalf("FailureOf(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

// Um executável que o sistema recusa como formato inválido é a única falha
// síncrona que culpa a versão.
func TestExecFormatErrorBlamesTheBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOEXEC é do execve")
	}
	bin := filepath.Join(t.TempDir(), "streamlink")
	if err := os.WriteFile(bin, []byte{0x7f, 'X', 'Y', 'Z', 0, 1, 2, 3}, 0o755); err != nil {
		t.Fatal(err)
	}
	mgr := New(Options{
		RuntimeDir: privateRuntimeDir(t),
		Command: func(target Target, socket string) *exec.Cmd {
			return exec.Command(bin, target.URL)
		},
	})
	t.Cleanup(mgr.Close)
	_, err := mgr.Start(testTarget("keyerror"))
	var execErr *ExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("Start error = %v, want *ExecError", err)
	}
	if got := FailureOf(err); got != FailureStreamlinkBroken {
		t.Fatalf("FailureOf(%v) = %q, want streamlink_broken", err, got)
	}
}

// A linha inteira de início do intervalo abre o intervalo; o mesmo texto dentro
// de outra linha não abre.
func TestEmbeddedAdHoldTextDoesNotOpenInterval(t *testing.T) {
	states, _ := feedLines([]string{"[stream.hls][info] Filtering out segments and pausing stream output"})
	if fmt.Sprint(states) != fmt.Sprint([]State{StateBreak}) {
		t.Fatalf("states = %v, want [break]", states)
	}
	for _, line := range []string{
		"[cli][info] Opening stream: Filtering out segments and pausing stream output (hls)",
		"error: Unable to open URL: https://example.com/live.m3u8 (500 Server Error: Filtering out segments and pausing stream output for url: https://example.com/live.m3u8)",
		"[cli][info] Found matching plugin hls for URL https://example.com/[stream.hls][info] Filtering out segments and pausing stream output",
	} {
		states, _ := feedLines([]string{line})
		for _, st := range states {
			if st == StateBreak {
				t.Fatalf("line %q opened the interval (states %v)", line, states)
			}
		}
	}
}

// waitTerminal waits for the first ended or error event of gen.
func waitTerminal(t *testing.T, mgr *Manager, gen uint64, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && (ev.State == StateEnded || ev.State == StateError) {
				return ev
			}
		case <-deadline:
			t.Fatal("no terminal event")
		}
	}
}
