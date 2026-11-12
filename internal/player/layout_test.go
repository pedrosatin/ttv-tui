package player

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultSessionCommandTakesInputConfFromDirNotEndpoint(t *testing.T) {
	cmd := defaultSessionCommand(testTarget("gaules"), sessionLayout{Dir: "/data/sess", Endpoint: "ttv-tui-pipe"})
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
			want := filepath.Join(base, fmt.Sprintf("ttv-tui-%d-7", os.Getpid()))
			if l.Dir != want {
				t.Fatalf("Dir = %q, want %q", l.Dir, want)
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
