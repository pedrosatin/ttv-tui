package player

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCheckPluginName(t *testing.T) {
	for _, ok := range []string{"kick", "twitch", "a", "kick_v2", "k9"} {
		if err := checkPluginName(ok); err != nil {
			t.Errorf("checkPluginName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "Kick", "9kick", "_kick", "kick.py", "../kick", "a/b", `a\b`, "kick-x", "kíck", strings.Repeat("a", maxPluginNameLen+1)} {
		if err := checkPluginName(bad); err == nil {
			t.Errorf("checkPluginName(%q) = nil, want an error", bad)
		}
	}
}

func privateSessionDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sess")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWritePluginsWritesPrivateFiles(t *testing.T) {
	sess := privateSessionDir(t)
	src := []byte("__plugin__ = None\n")
	dir, err := writePlugins(sess, []Plugin{{Name: "kick", Source: src}, {Name: "other", Source: []byte("x = 1\n")}})
	if err != nil {
		t.Fatalf("writePlugins: %v", err)
	}
	if want := filepath.Join(sess, pluginDirName); dir != want {
		t.Fatalf("plugin dir = %q, want %q", dir, want)
	}
	got, err := os.ReadFile(filepath.Join(dir, "kick.py"))
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("kick.py = %q, %v; want %q", got, err, src)
	}
	if runtime.GOOS == "windows" {
		return
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("plugin dir mode = %v, %v; want 0700", fi.Mode().Perm(), err)
	}
	for _, name := range []string{"kick.py", "other.py"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v; want 0600", name, fi.Mode().Perm(), err)
		}
	}
}

func TestWritePluginsWithoutPluginsTouchesNothing(t *testing.T) {
	sess := privateSessionDir(t)
	dir, err := writePlugins(sess, nil)
	if err != nil || dir != "" {
		t.Fatalf("writePlugins(nil) = %q, %v; want \"\", nil", dir, err)
	}
	if _, err := os.Stat(filepath.Join(sess, pluginDirName)); !os.IsNotExist(err) {
		t.Fatalf("plugin dir created without plugins: %v", err)
	}
}

func TestWritePluginsRefusesAndLeavesNothing(t *testing.T) {
	src := []byte("x = 1\n")
	tests := []struct {
		name    string
		plugins []Plugin
		prepare func(t *testing.T, sess string)
	}{
		{name: "nome que é caminho", plugins: []Plugin{{Name: "../kick", Source: src}}},
		{name: "nome repetido", plugins: []Plugin{{Name: "kick", Source: src}, {Name: "kick", Source: src}}},
		{name: "fonte vazia", plugins: []Plugin{{Name: "kick"}}},
		{
			// Um diretório que já existe pode ter vindo de outra pessoa: não se
			// grava código Python dentro dele.
			name:    "diretório de plugins já existe",
			plugins: []Plugin{{Name: "kick", Source: src}},
			prepare: func(t *testing.T, sess string) {
				if err := os.Mkdir(filepath.Join(sess, pluginDirName), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := privateSessionDir(t)
			if tc.prepare != nil {
				tc.prepare(t, sess)
			}
			dir, err := writePlugins(sess, tc.plugins)
			if err == nil || dir != "" {
				t.Fatalf("writePlugins = %q, %v; want an error", dir, err)
			}
			if _, err := os.Stat(filepath.Join(sess, pluginDirName, "kick.py")); !os.IsNotExist(err) {
				t.Fatalf("kick.py written: %v", err)
			}
		})
	}
}

func TestWritePluginsRefusesSharedSessionDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a privacidade no Windows é a ACL, coberta pelos testes de input.conf")
	}
	sess := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(sess, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sess, 0o770); err != nil {
		t.Fatal(err)
	}
	if dir, err := writePlugins(sess, []Plugin{{Name: "kick", Source: []byte("x = 1\n")}}); err == nil {
		t.Fatalf("writePlugins in a group-writable dir = %q, nil; want an error", dir)
	}
	if _, err := os.Stat(filepath.Join(sess, pluginDirName)); !os.IsNotExist(err) {
		t.Fatalf("plugin dir created in a shared session dir: %v", err)
	}
}

func TestDefaultSessionCommandPluginDir(t *testing.T) {
	target := Target{URL: "https://kick.com/xqc", Label: "kick:xqc", ExtraArgs: []string{"--kick-low-latency"}}
	cmd := testInvocation.command(target, sessionLayout{Dir: "/data/sess", Endpoint: "/data/sess/i", PluginDir: "/data/sess/streamlink-plugins"})
	want := []string{"streamlink", "--no-config", "--loglevel", "info", "--plugin-dir", "/data/sess/streamlink-plugins", "--kick-low-latency", "https://kick.com/xqc"}
	if !slices.Equal(cmd.Args[:len(want)], want) {
		t.Fatalf("argv = %q, want it to start with %q", cmd.Args, want)
	}

	plain := testInvocation.command(testTarget("gaules"), sessionLayout{Dir: "/data/sess", Endpoint: "/data/sess/i"})
	if slices.Contains(plain.Args, "--plugin-dir") {
		t.Fatalf("a session without plugins got --plugin-dir: %q", plain.Args)
	}
}

// O Manager com o comando padrão grava o plugin no diretório da sessão e passa
// esse diretório ao streamlink; uma reprodução sem plugin não recebe nada.
func TestManagerHandsPluginDirToStreamlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o streamlink falso é um script sh")
	}
	work := t.TempDir()
	script := filepath.Join(work, "streamlink")
	// Copia o que recebeu antes de sair: o diretório da sessão some com ela.
	body := `#!/bin/sh
out="$0.out"
: > "$out"
dir=""
prev=""
for a in "$@"; do
  printf '%s\n' "$a" >> "$out"
  if [ "$prev" = "--plugin-dir" ]; then dir="$a"; fi
  prev="$a"
done
if [ -n "$dir" ]; then
  cp "$dir/kick.py" "$0.kick.py"
  ls -ld "$dir" | cut -c1-10 > "$0.dirmode"
  ls -l "$dir/kick.py" | cut -c1-10 > "$0.filemode"
fi
echo "[cli][error] fake streamlink" >&2
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(work, "run")
	mgr := New(Options{
		StreamlinkPath: script,
		RuntimeDir:     runtimeDir,
		RegistryDir:    filepath.Join(work, "registry"),
		LookPath:       func(string) (string, error) { return "/bin/true", nil },
		SocketWait:     2 * time.Second,
		StopWait:       500 * time.Millisecond,
	})
	t.Cleanup(mgr.Close)

	src := []byte("# ttv-tui test plugin\n")
	gen, err := mgr.Start(Target{URL: "https://kick.com/xqc", Label: "kick:xqc", Plugins: []Plugin{{Name: "kick", Source: src}}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateError, 10*time.Second)

	args, err := os.ReadFile(script + ".out")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(args)), "\n")
	i := slices.Index(argv, "--plugin-dir")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("streamlink argv lacks --plugin-dir: %q", argv)
	}
	if url := slices.Index(argv, "https://kick.com/xqc"); url < i {
		t.Fatalf("--plugin-dir after the URL: %q", argv)
	}
	if dir := argv[i+1]; !strings.HasPrefix(dir, runtimeDir+string(filepath.Separator)) || filepath.Base(dir) != pluginDirName {
		t.Fatalf("plugin dir %q is not inside the session under %q", dir, runtimeDir)
	}
	if got, err := os.ReadFile(script + ".kick.py"); err != nil || !bytes.Equal(got, src) {
		t.Fatalf("kick.py seen by streamlink = %q, %v; want %q", got, err, src)
	}
	for file, want := range map[string]string{".dirmode": "drwx------", ".filemode": "-rw-------"} {
		got, err := os.ReadFile(script + file)
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Fatalf("%s = %q, %v; want %q", file, got, err, want)
		}
	}

	_ = os.Remove(script + ".out")
	gen, err = mgr.Start(Target{URL: "https://www.twitch.tv/gaules", Label: "gaules"})
	if err != nil {
		t.Fatalf("Start without plugins: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateError, 10*time.Second)
	args, err = os.ReadFile(script + ".out")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--plugin-dir") {
		t.Fatalf("a target without plugins got --plugin-dir:\n%s", args)
	}

	// Um plugin que não pode ser gravado não impede a reprodução: o streamlink
	// toca com os plugins dele e o motivo vai para o log de saída.
	_ = os.Remove(script + ".out")
	gen, err = mgr.Start(Target{URL: "https://kick.com/xqc", Label: "kick:xqc", Plugins: []Plugin{{Name: "../kick", Source: src}}})
	if err != nil {
		t.Fatalf("Start with an unwritable plugin: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateError, 10*time.Second)
	args, err = os.ReadFile(script + ".out")
	if err != nil {
		t.Fatalf("streamlink did not run with an unwritable plugin: %v", err)
	}
	if strings.Contains(string(args), "--plugin-dir") {
		t.Fatalf("an unwritable plugin still got --plugin-dir:\n%s", args)
	}
	if !slices.ContainsFunc(mgr.Notes(), func(n string) bool { return strings.Contains(n, "streamlink plugins not written") }) {
		t.Fatalf("notes = %q, want the plugin write failure", mgr.Notes())
	}
}

func TestIsPluginFallbackLine(t *testing.T) {
	yes := []string{
		"[plugins.kick][warning] ttv-tui plugin fallback: ad filter off, using the built-in kick plugin (LookupError: KickHLSStreamWriter is missing)",
		"[plugins.kick][warning] ttv-tui plugin fallback: x",
	}
	no := []string{
		"",
		"ttv-tui plugin fallback: no prefix",
		"[plugins.kick][error] ttv-tui plugin fallback: wrong level",
		"[plugins.kick][info] ttv-tui plugin fallback: wrong level",
		"[cli][warning] ttv-tui plugin fallback: not a plugin",
		"[plugins.Kick][warning] ttv-tui plugin fallback: bad name",
		"[plugins.kick][warning] something else",
		"[cli][info] Found matching plugin kick for URL https://kick.com/[plugins.kick][warning] ttv-tui plugin fallback: x",
	}
	for _, l := range yes {
		if !isPluginFallbackLine(l) {
			t.Errorf("isPluginFallbackLine(%q) = false, want true", l)
		}
	}
	for _, l := range no {
		if isPluginFallbackLine(l) {
			t.Errorf("isPluginFallbackLine(%q) = true, want false", l)
		}
	}
}

// A linha de fallback do plugin vai para o log de saída e não culpa o
// streamlink: o vídeo segue pelo plugin embutido, e o nome de exceção Python que
// ela cita não pode marcar a versão gerenciada como quebrada.
func TestPluginFallbackLineIsNotedNotClassified(t *testing.T) {
	line := "[plugins.kick][warning] ttv-tui plugin fallback: ad filter off, using the built-in kick plugin (LookupError: KickHLSStreamWriter is missing)"
	s := &Session{
		cfg: sessionConfig{Generation: 1, Target: Target{URL: "https://kick.com/xqc"}, Emit: func(Event) {}},
	}
	var notes []string
	s.cfg.Note = func(n string) { notes = append(notes, n) }
	s.state = StatePlaying
	s.handleLine("Traceback (most recent call last):")
	s.handleLine(line)
	s.handleLine("[stream.hls][info] Filtering out segments and pausing stream output")
	if s.failClass == FailureStreamlinkBroken {
		t.Fatalf("fallback line classified as %q (line %q)", s.failClass, s.failLine)
	}
	if !slices.Contains(notes, "player: "+line) {
		t.Fatalf("notes = %q, want the fallback line", notes)
	}
	if s.state != StateBreak {
		t.Fatalf("state = %q, want the ad hold after the fallback line", s.state)
	}
}
