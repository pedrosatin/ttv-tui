package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDefaultSessionCommandTakesInputConfFromDirNotEndpoint(t *testing.T) {
	cmd := testInvocation.command(testTarget("gaules"), sessionLayout{Dir: "/data/sess", Endpoint: "ttv-tui-pipe"})
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"--input-ipc-server=ttv-tui-pipe",
		quotePlayerArg("--input-conf=" + inputConfPath("/data/sess")),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestPipeNameHasNoSeparators(t *testing.T) {
	// Only the host's separator is split by filepath.Base; the Windows form of
	// this check runs on Windows.
	for _, dir := range []string{
		"/run/user/1000/ttv-tui/3-123456",
		"/tmp/a b/3-123456",
	} {
		got := pipeName(dir, 42)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("pipeName(%q) = %q has a path separator", dir, got)
		}
		if !strings.HasPrefix(got, "ttv-tui-42-") {
			t.Errorf("pipeName(%q) = %q, want the ttv-tui-<pid>- prefix", dir, got)
		}
	}
	if pipeName("/a/1-x", 7) == pipeName("/a/1-y", 7) {
		t.Error("two session directories share a pipe name")
	}
	if pipeName("/a/1-x", 7) == pipeName("/a/1-x", 8) {
		t.Error("two processes share a pipe name")
	}
	// The name reaches mpv through streamlink's shlex: it has to survive unquoted.
	for _, dir := range []string{"/run/ttv-tui/3-123456", "/tmp/ttv-tui-42-3"} {
		name := pipeName(dir, 42)
		if got := quotePlayerArg(name); got != name {
			t.Errorf("pipeName(%q) = %q needs quoting: %q", dir, name, got)
		}
	}
}

// unusableRuntimeDir cannot be created on any OS: a file sits where its parent
// should be.
func unusableRuntimeDir(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return filepath.Join(file, "run")
}

// useTempDir points os.TempDir at dir on every OS.
func useTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}

func TestFallbackGoesToOSTempDir(t *testing.T) {
	base := t.TempDir()
	useTempDir(t, base)
	for name, runtimeDir := range map[string]string{
		"unusable": unusableRuntimeDir(t),
		"empty":    "",
	} {
		t.Run(name, func(t *testing.T) {
			l, err := prepareSession(runtimeDir, 7)
			if err != nil {
				t.Fatalf("prepareSession: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })
			if filepath.Dir(l.Dir) != base || !strings.HasPrefix(filepath.Base(l.Dir), "ttv-tui-7-") {
				t.Fatalf("Dir = %q, want %s/ttv-tui-7-<aleatório>", l.Dir, base)
			}
			if fi, err := os.Stat(l.Dir); err != nil || !fi.IsDir() {
				t.Fatalf("fallback directory: %v, %v", fi, err)
			}
		})
	}
}

// The base is read at use, not frozen at startup.
func TestFallbackBaseFollowsTempDir(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	useTempDir(t, first)
	if got := getFallbackBaseDir(); got != first {
		t.Fatalf("base = %q, want %q", got, first)
	}
	useTempDir(t, second)
	if got := getFallbackBaseDir(); got != second {
		t.Fatalf("base = %q, want %q", got, second)
	}
	override := t.TempDir()
	restore := setFallbackBaseDirForTest(override)
	if got := getFallbackBaseDir(); got != override {
		t.Fatalf("base = %q, want the override %q", got, override)
	}
	restore()
	if got := getFallbackBaseDir(); got != second {
		t.Fatalf("base after restore = %q, want %q", got, second)
	}
}

// The directory and the endpoint share no root: input.conf must still land in
// the directory, the command must get the endpoint as given, and cleanup must
// remove the directory and the endpoint without touching the endpoint's parent.
func TestSessionKeepsDirAndEndpointSeparate(t *testing.T) {
	skipWithoutSocketFile(t)
	dir := filepath.Join(t.TempDir(), "sd")
	epRoot := filepath.Join(t.TempDir(), "ep")
	for _, d := range []string{dir, epRoot} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
	}
	endpoint := filepath.Join(epRoot, "sock")

	events := make(chan Event, 64)
	var confPath string
	var gotEndpoint string
	s, err := startSession(sessionConfig{
		Generation: 1,
		Target:     testTarget("split"),
		Prepare: func(string, uint64) (sessionLayout, error) {
			return sessionLayout{Dir: dir, Endpoint: endpoint}, nil
		},
		Command: func(target Target, l sessionLayout) *exec.Cmd {
			gotEndpoint = l.Endpoint
			return helperCommand("play")(target, l.Endpoint)
		},
		WriteInputConf: func(path string) error {
			confPath = path
			return writeInputConf(path)
		},
		SocketWait:   5 * time.Second,
		StopWait:     500 * time.Millisecond,
		BreakMaxHold: time.Minute,
		Emit: func(ev Event) {
			select {
			case events <- ev:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("startSession: %v", err)
	}
	t.Cleanup(s.Stop)

	_ = waitForState(t, events, 1, StatePlaying, 5*time.Second)

	if s.dir == filepath.Dir(s.endpoint) {
		t.Fatalf("test needs distinct roots, both are %q", s.dir)
	}
	if want := filepath.Join(dir, inputConfName); confPath != want {
		t.Fatalf("input.conf written at %q, want %q", confPath, want)
	}
	if _, err := os.Stat(filepath.Join(dir, inputConfName)); err != nil {
		t.Fatalf("input.conf missing from the session dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(epRoot, inputConfName)); !os.IsNotExist(err) {
		t.Fatalf("input.conf leaked next to the endpoint: %v", err)
	}
	if gotEndpoint != endpoint || s.IPCEndpoint() != endpoint {
		t.Fatalf("endpoint = %q / %q, want %q", gotEndpoint, s.IPCEndpoint(), endpoint)
	}

	s.Stop()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("session dir left behind: %v", err)
	}
	if _, err := os.Stat(endpoint); !os.IsNotExist(err) {
		t.Fatalf("endpoint left behind: %v", err)
	}
	if _, err := os.Stat(epRoot); err != nil {
		t.Fatalf("endpoint's parent must not be removed: %v", err)
	}
}

// O streamlink roda dentro da sessão, então o que ele recebe tem de ser absoluto:
// com um RuntimeDir ou $TMPDIR relativo, o MkdirTemp devolveria um diretório
// relativo e o input.conf, o plugin e o socket do mpv seriam lidos de dentro da
// própria sessão. Vale também para o fallback e para a sessão com plugin da Kick.
// shortLaunchDir é o diretório "de onde o app foi aberto" nos testes de base
// relativa. No Unix fica direto em /tmp: um TMPDIR longo faria a sessão sob ele
// passar do limite do socket, e fallbackBase cairia, corretamente, em /tmp, fora
// do diretório que o teste confere. O fallback por comprimento tem teste próprio.
func shortLaunchDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("/tmp", "ttvl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestSessionPathsAreAbsoluteWithRelativeBase(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, launch string) (runtimeDir string)
	}{
		{"runtime dir", func(t *testing.T, launch string) string {
			return "rt"
		}},
		{"fallback base", func(t *testing.T, launch string) string {
			if err := os.Mkdir(filepath.Join(launch, "fb"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(setFallbackBaseDirForTest("fb"))
			return ""
		}},
	}
	if runtime.GOOS != "windows" {
		cases = append(cases, struct {
			name  string
			setup func(t *testing.T, launch string) (runtimeDir string)
		}{"relative TMPDIR", func(t *testing.T, launch string) string {
			if err := os.Mkdir(filepath.Join(launch, "tmp"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", "tmp")
			return ""
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			launch, err := filepath.EvalSymlinks(shortLaunchDir(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(launch)
			runtimeDir := tc.setup(t, launch)

			l, err := prepareSession(runtimeDir, 1)
			if err != nil {
				t.Fatalf("prepareSession: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })
			pluginDir, err := writePlugins(l.Dir, []Plugin{{Name: "kick", Source: []byte("__plugin__ = None\n")}})
			if err != nil {
				t.Fatalf("writePlugins: %v", err)
			}
			l.PluginDir = pluginDir

			real, err := filepath.EvalSymlinks(l.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(real, launch+string(filepath.Separator)) {
				t.Fatalf("Dir %q is not under the base inside %q", real, launch)
			}
			paths := map[string]string{
				"Dir":        l.Dir,
				"input.conf": inputConfPath(l.Dir),
				"PluginDir":  l.PluginDir,
			}
			if runtime.GOOS != "windows" {
				paths["Endpoint"] = l.Endpoint
			}
			for what, path := range paths {
				if !filepath.IsAbs(path) {
					t.Errorf("%s = %q, want an absolute path", what, path)
				}
			}

			cmd := testInvocation.command(testTarget("gaules"), l)
			if cmd.Dir != l.Dir || !filepath.IsAbs(cmd.Dir) {
				t.Errorf("cmd.Dir = %q, want the absolute session directory %q", cmd.Dir, l.Dir)
			}
			if i := slices.Index(cmd.Args, "--plugin-dir"); i < 0 || cmd.Args[i+1] != l.PluginDir {
				t.Errorf("args %q: want --plugin-dir %q", cmd.Args, l.PluginDir)
			}
			joined := strings.Join(cmd.Args, " ")
			for _, want := range []string{"--input-ipc-server=" + l.Endpoint, "--input-conf=" + inputConfPath(l.Dir)} {
				if !strings.Contains(joined, want) {
					t.Errorf("args %q missing %q", joined, want)
				}
			}
		})
	}
}
