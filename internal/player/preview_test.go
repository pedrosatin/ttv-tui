package player

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPreviewCommandArgv(t *testing.T) {
	endpoint := "/run/user/1000/ttv-tui/preview/1/i"
	layout := sessionLayout{Dir: "/run/user/1000/ttv-tui/preview/1", Endpoint: endpoint}
	cmd := testInvocation.previewCommand(testTarget("gaules"), layout)

	wantArgs := []string{
		"streamlink",
		"--no-config",
		"--loglevel", "info",
		"https://www.twitch.tv/gaules",
		PreviewQuality,
		"--player", "mpv",
		"--player-args", buildPlayerArgs(previewMPVArgs(endpoint)),
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("preview argv:\ngot:  %v\nwant: %v", cmd.Args, wantArgs)
	}
	if slices.Contains(cmd.Args, "best") {
		t.Fatal("preview argv still asks for best quality")
	}
	playerArgs := ""
	for i, a := range cmd.Args {
		if a == "--player-args" && i+1 < len(cmd.Args) {
			playerArgs = cmd.Args[i+1]
		}
	}
	for _, want := range []string{
		"--mute=yes",
		"--autofit=" + PreviewAutofit,
		"--focus-on=never",
		"--input-ipc-server=" + endpoint,
	} {
		if !strings.Contains(playerArgs, want) {
			t.Fatalf("player-args %q missing %q", playerArgs, want)
		}
	}
	if strings.Contains(playerArgs, "--input-conf=") {
		t.Fatalf("preview must omit input.conf, got %q", playerArgs)
	}
}

func TestNewPreviewNestsRuntimeAndRegistry(t *testing.T) {
	runtime := t.TempDir()
	registry := t.TempDir()
	mgr := NewPreview(Options{RuntimeDir: runtime, RegistryDir: registry, Command: helperCommand("play")})
	t.Cleanup(mgr.Close)
	if got, want := mgr.opts.RuntimeDir, filepath.Join(runtime, "preview"); got != want {
		t.Fatalf("RuntimeDir = %q, want %q", got, want)
	}
	if got, want := mgr.opts.RegistryDir, filepath.Join(registry, "preview"); got != want {
		t.Fatalf("RegistryDir = %q, want %q", got, want)
	}
	if !mgr.opts.Preview {
		t.Fatal("Preview flag not set")
	}
}

func TestPreviewStopLeavesNoOrphan(t *testing.T) {
	t.Parallel()
	mgr := NewPreview(Options{
		RuntimeDir: t.TempDir(),
		Command:    helperCommand("play"),
		StopWait:   500 * time.Millisecond,
	})
	t.Cleanup(mgr.Close)

	gen, err := mgr.Start(testTarget("previewchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	pid := 0
	if mgr.session != nil {
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	if pid <= 0 {
		t.Fatal("no session pid")
	}
	mgr.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("preview process %d still alive after Stop", pid)
	}
}

func TestPreviewCloseLeavesNoOrphan(t *testing.T) {
	t.Parallel()
	mgr := NewPreview(Options{
		RuntimeDir: t.TempDir(),
		Command:    helperCommand("play"),
		StopWait:   500 * time.Millisecond,
	})
	gen, err := mgr.Start(testTarget("previewclose"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	pid := 0
	if mgr.session != nil {
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	mgr.Close()
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("preview process %d still alive after Close", pid)
	}
}
