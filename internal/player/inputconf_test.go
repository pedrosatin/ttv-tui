package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInputConfContentIsExact(t *testing.T) {
	want := []struct{ key, command string }{
		{"SPACE", `no-osd cycle pause; show-text "${?pause==yes:‖}${?pause==no:▶}"`},
		{"+", `no-osd add volume 5; show-text "${volume}%"`},
		{"=", `no-osd add volume 5; show-text "${volume}%"`},
		{"-", `no-osd add volume -5; show-text "${volume}%"`},
		{"_", `no-osd add volume -5; show-text "${volume}%"`},
	}
	var b strings.Builder
	for _, line := range want {
		b.WriteString(line.key + " " + line.command + "\n")
	}
	if got := inputConfContent(); got != b.String() {
		t.Fatalf("input.conf:\n%s\nwant:\n%s", got, b.String())
	}
}

func TestInputConfBindsNothingElse(t *testing.T) {
	// Lowercase match: "quit" also covers quit-watch-later, "edition" cycle-edition.
	forbidden := []string{"audio-delay", "brightness", "contrast", "edition", "fullscreen", "quit"}
	keys := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(inputConfContent()), "\n") {
		for _, word := range forbidden {
			if strings.Contains(strings.ToLower(line), word) {
				t.Errorf("line %q contains %q", line, word)
			}
		}
		keys[strings.Fields(line)[0]] = true
	}
	for _, key := range []string{"q", "Q", "?", "e", "E", "f", "Ctrl+-", "Ctrl+=", "LEFT", "RIGHT", "UP", "DOWN", "MBTN_LEFT", "WHEEL_UP"} {
		if keys[key] {
			t.Errorf("key %q must stay unbound", key)
		}
	}
	if len(keys) != 5 {
		t.Fatalf("bound keys = %v, want exactly 5", keys)
	}
}

func TestInputConfVolumeOSDHasNoWords(t *testing.T) {
	for _, line := range strings.Split(strings.TrimSpace(inputConfContent()), "\n") {
		if !strings.Contains(line, "add volume") {
			continue
		}
		// Without no-osd, mpv adds its own English "Volume: 50%" over ours.
		if !strings.Contains(line, "no-osd add volume") {
			t.Errorf("line %q leaves mpv's volume OSD on", line)
		}
		if !strings.HasSuffix(line, `show-text "${volume}%"`) {
			t.Errorf("line %q does not end in the numeric OSD", line)
		}
	}
}

func TestInputConfPathIsInSessionDir(t *testing.T) {
	if got, want := inputConfPath("/run/user/1000/ttv-tui/1-x"), filepath.FromSlash("/run/user/1000/ttv-tui/1-x/mpv-input.conf"); got != want {
		t.Fatalf("inputConfPath = %q, want %q", got, want)
	}
}

// privateDir is a directory writeInputConf accepts. LOCALAPPDATA only matters on
// Windows, where being inside it is what makes a directory private.
func privateDir(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	t.Setenv("LOCALAPPDATA", parent)
	dir := filepath.Join(parent, "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	return dir
}

func TestWriteInputConfCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(privateDir(t), inputConfName)
	if err := writeInputConf(path); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// Windows has no such bits: see checkDirPrivacy.
	if perm := fi.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("perm = %o, want 600", perm)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != inputConfContent() {
		t.Fatalf("file content = %q", got)
	}
}

func TestWriteInputConfFailsOnMissingDir(t *testing.T) {
	if err := writeInputConf(filepath.Join(t.TempDir(), "gone", inputConfName)); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestSessionWritesInputConfBeforeCommand(t *testing.T) {
	type seen struct {
		dir     string
		content string
		dirPerm os.FileMode
		perm    os.FileMode
	}
	saw := make(chan seen, 1)
	// The directory is learned from where the writer is asked to put the file,
	// not from the endpoint the command receives.
	var confPath atomic.Value
	runtimeDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", runtimeDir)
	mgr := New(Options{
		RuntimeDir: runtimeDir,
		SocketWait: 5 * time.Second,
		StopWait:   500 * time.Millisecond,
		WriteInputConf: func(path string) error {
			confPath.Store(path)
			return writeInputConf(path)
		},
		Command: func(login, endpoint string) *exec.Cmd {
			path, _ := confPath.Load().(string)
			var s seen
			s.dir = filepath.Dir(path)
			if fi, err := os.Stat(s.dir); err == nil {
				s.dirPerm = fi.Mode().Perm()
			}
			if fi, err := os.Stat(path); err == nil {
				s.perm = fi.Mode().Perm()
			}
			if raw, err := os.ReadFile(path); err == nil {
				s.content = string(raw)
			}
			saw <- s
			return helperCommand("play")(login, endpoint)
		},
	})
	t.Cleanup(mgr.Close)

	gen, err := mgr.Start("confchan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if ev.InputConfFailed {
		t.Fatal("InputConfFailed set although the file was written")
	}
	s := <-saw
	if s.content != inputConfContent() {
		t.Fatalf("file when the command was built = %q", s.content)
	}
	if runtime.GOOS != "windows" && (s.perm != 0o600 || s.dirPerm != 0o700) {
		t.Fatalf("perm = %o, dir = %o; want 600 in 700", s.perm, s.dirPerm)
	}
	if err := mgr.InputConfErr(); err != nil {
		t.Fatalf("InputConfErr = %v", err)
	}

	mgr.Stop()
	if _, err := os.Stat(s.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session directory left behind: %v", err)
	}
}

func TestInputConfWriteFailureKeepsPlaying(t *testing.T) {
	writeErr := errors.New("disk full")
	var usedFallback atomic.Bool
	mgr := newTestManager(t, "play", Options{
		WriteInputConf: func(string) error { return writeErr },
		CommandNoInputConf: func(login, socketPath string) *exec.Cmd {
			usedFallback.Store(true)
			return helperCommand("play")(login, socketPath)
		},
	})

	gen, err := mgr.Start("nokeys")
	if err != nil {
		t.Fatalf("Start must survive a failed input.conf write: %v", err)
	}
	if !usedFallback.Load() {
		t.Fatal("session did not use CommandNoInputConf")
	}
	for _, want := range []State{StateStarting, StatePlaying} {
		if ev := waitForState(t, mgr.Events(), gen, want, 5*time.Second); !ev.InputConfFailed {
			t.Fatalf("%s event has no InputConfFailed", want)
		}
	}
	if !errors.Is(mgr.InputConfErr(), writeErr) {
		t.Fatalf("InputConfErr = %v", mgr.InputConfErr())
	}
}

func TestInputConfWriteFailureWithCustomCommandKeepsCommand(t *testing.T) {
	mgr := newTestManager(t, "play", Options{
		WriteInputConf: func(string) error { return errors.New("disk full") },
	})
	gen, err := mgr.Start("custom")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if ev := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second); !ev.InputConfFailed {
		t.Fatal("playing event has no InputConfFailed")
	}
}

// Unix rejects a shared fallback before writing input.conf. Windows keeps
// playing without input.conf when the temp directory is outside the profile.
func TestSharedFallbackDirectory(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	base := t.TempDir()
	defer setFallbackBaseDirForTest(base)()
	dir := filepath.Join(base, fmt.Sprintf("ttv-tui-%d-1", os.Getpid()))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	mgr := newTestManager(t, "play", Options{RuntimeDir: unusableRuntimeDir(t)})
	gen, err := mgr.Start("shared")
	if runtime.GOOS != "windows" {
		if err == nil {
			t.Fatal("Start accepted a shared fallback directory")
		}
		return
	}
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if ev := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second); !ev.InputConfFailed {
		t.Fatal("playing event has no InputConfFailed")
	}
	if mgr.InputConfErr() == nil {
		t.Fatal("InputConfErr = nil")
	}
	if _, err := os.Lstat(filepath.Join(dir, inputConfName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("input.conf written to a shared directory: %v", err)
	}
}

func TestDefaultFallbackDropsOnlyInputConf(t *testing.T) {
	mgr := New(Options{})
	t.Cleanup(mgr.Close)
	if mgr.opts.CommandNoInputConf == nil {
		t.Fatal("default Manager has no CommandNoInputConf")
	}
	joined := strings.Join(mgr.opts.CommandNoInputConf("gaules", "/run/user/1000/ttv-tui/1/i").Args, " ")
	if strings.Contains(joined, "--input-conf") {
		t.Fatalf("fallback still passes --input-conf: %q", joined)
	}
	// Going back to mpv's built-in bindings would bring the invisible actions back.
	if !strings.Contains(joined, "--no-input-default-bindings") {
		t.Fatalf("fallback lost --no-input-default-bindings: %q", joined)
	}
}

func TestVolumeChangedInMPVWindowReachesEvents(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_EXTERNAL_VOLUME", "35")
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start("extvolume")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for got := false; !got; {
		select {
		case ev := <-mgr.Events():
			got = ev.Generation == gen && ev.Volume == 35
		case <-deadline:
			t.Fatalf("no event with the window's volume; Volume()=%d", mgr.Volume())
		}
	}
	if got := mgr.Volume(); got != 35 {
		t.Fatalf("Volume() = %d, want 35", got)
	}

	// The next TUI step starts from the window's value, not from a stale one.
	if err := mgr.AdjustVolume(5); err != nil {
		t.Fatalf("AdjustVolume: %v", err)
	}
	waitVolume(t, mgr, gen, 40, 3*time.Second)
}
