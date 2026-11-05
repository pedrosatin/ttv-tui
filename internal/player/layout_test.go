package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultSessionCommandTakesInputConfFromDirNotEndpoint(t *testing.T) {
	cmd := defaultSessionCommand("gaules", sessionLayout{Dir: "/data/sess", Endpoint: "ttv-tui-pipe"})
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
}

// The directory and the endpoint share no root: input.conf must still land in
// the directory, the command must get the endpoint as given, and cleanup must
// remove the directory and the endpoint without touching the endpoint's parent.
func TestSessionKeepsDirAndEndpointSeparate(t *testing.T) {
	skipWithoutSessionIPC(t)
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
		Login:      "split",
		Prepare: func(string, uint64) (sessionLayout, error) {
			return sessionLayout{Dir: dir, Endpoint: endpoint}, nil
		},
		Command: func(login string, l sessionLayout) *exec.Cmd {
			gotEndpoint = l.Endpoint
			return helperCommand("play")(login, l.Endpoint)
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
