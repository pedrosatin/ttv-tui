package player

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLogFollowerSeesWhatIsAppendedAndTheLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	read, err := openPlayerLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Close() }()
	write, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = write.Close() }()

	gone := make(chan struct{})
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(&logFollower{ctx: context.Background(), file: read, gone: gone})
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	next := func(want string) {
		t.Helper()
		select {
		case got, ok := <-lines:
			if !ok || got != want {
				t.Fatalf("line = %q (open: %v), want %q", got, ok, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for %q", want)
		}
	}

	if _, err := write.WriteString("first\nsec"); err != nil {
		t.Fatal(err)
	}
	next("first")
	// The rest of the line comes after the follower has found the end of the file.
	time.Sleep(3 * logPollInterval)
	if _, err := write.WriteString("ond\n"); err != nil {
		t.Fatal(err)
	}
	next("second")

	// Written just before the process goes, with no newline.
	if _, err := write.WriteString("last"); err != nil {
		t.Fatal(err)
	}
	close(gone)
	next("last")
	select {
	case line, ok := <-lines:
		if ok {
			t.Fatalf("line after the end: %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("follower did not stop with the process gone")
	}
}

func TestLogFollowerStopsWithTheSession(t *testing.T) {
	read, err := openPlayerLog(filepath.Join(t.TempDir(), "log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = read.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(&logFollower{ctx: ctx, file: read, gone: make(chan struct{})})
		for sc.Scan() {
		}
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("follower outlived its session")
	}
}

// A log left in the fallback directory by an earlier run says nothing about this one.
func TestOpenPlayerLogDropsAnOldLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("error: No playable streams found\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openPlayerLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("log kept %d bytes", info.Size())
	}
}

func TestDefaultSocketWaitCoversASlowNetwork(t *testing.T) {
	// streamlink took 15.6s to reach mpv behind a VPN at 380ms RTT.
	const measured = 15600 * time.Millisecond
	if got := New(Options{}).opts.SocketWait; got < measured*3/2 {
		t.Fatalf("SocketWait = %v, too close to the %v measured", got, measured)
	}
}

// fakeStreamlink puts a streamlink and an mpv on PATH, so that the default command
// is what runs: its real arguments reach something that treats them as streamlink
// does. script is what it does after the logging setup.
func fakeStreamlink(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake is a shell script, and Windows has no embedded video")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"streamlink": fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", self),
		"mpv":        "#!/bin/sh\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TTV_PLAYER_HELPER", "1")
	t.Setenv("TTV_PLAYER_HELPER_MODE", "fake-streamlink")
	t.Setenv("TTV_PLAYER_HELPER_LOGIN", "gaules")
	t.Setenv("TTV_FAKE_STREAMLINK", script)
}

func startFakeEmbedded(t *testing.T, rec *terminalRecorder) (*Manager, uint64) {
	t.Helper()
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)
	mgr := New(Options{RuntimeDir: t.TempDir(), StopWait: 500 * time.Millisecond, SocketWait: 5 * time.Second})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("gaules")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return mgr, gen
}

// streamlink writes the two lines of an ad break only to the file given in
// --logfile, the first one in two writes. Nothing else tells the session about it.
func TestEmbeddedSessionTakesTheAdBreakFromTheLog(t *testing.T) {
	fakeStreamlink(t, "play")
	rec := &terminalRecorder{}
	mgr, gen := startFakeEmbedded(t, rec)

	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitFrames(t, rec, 3)
	waitForState(t, mgr.Events(), gen, StateBreak, 5*time.Second)
	ev := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if !strings.Contains(ev.LastLine, "Resuming stream output") {
		t.Fatalf("LastLine = %q", ev.LastLine)
	}
	mgr.Stop()

	text, _ := splitEmissions(t, rec.bytes(), 0, 0)
	if len(text) != 0 {
		t.Fatalf("the log reached the terminal: %q", text)
	}
}

func TestEmbeddedSessionTakesAFatalLineFromTheLog(t *testing.T) {
	fakeStreamlink(t, "offline-hang")
	mgr, gen := startFakeEmbedded(t, &terminalRecorder{})

	start := time.Now()
	ev := waitError(t, mgr, gen, 4*time.Second)
	if ev.Failure != FailureOffline {
		t.Fatalf("Failure = %q, line %q", ev.Failure, ev.LastLine)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("failed after %v: the line did not end the wait", elapsed)
	}
}

// The line is in the file and the process is gone: only the last pass can find it.
func TestEmbeddedSessionReadsTheLogToTheEndAfterExit(t *testing.T) {
	fakeStreamlink(t, "error-exit")
	mgr, gen := startFakeEmbedded(t, &terminalRecorder{})

	ev := waitError(t, mgr, gen, 4*time.Second)
	if ev.Failure != FailureNetwork || !strings.Contains(ev.LastLine, "Unable to open URL") {
		t.Fatalf("Failure = %q, line %q", ev.Failure, ev.LastLine)
	}
}

// The session's directory goes with it, log included.
func TestEmbeddedSessionTakesItsLogAlong(t *testing.T) {
	fakeStreamlink(t, "play")
	mgr, gen := startFakeEmbedded(t, &terminalRecorder{})
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	path := playerLogPath(mgr.session.dir)
	mgr.mu.Unlock()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log while playing: %v", err)
	}
	mgr.Stop()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("log after the session: %v", err)
	}
}

// helperFakeStreamlink is streamlink as far as the log goes: it opens --logfile
// the way streamlink does and writes its log nowhere else. The player's stdout is
// its stdout, as under -v.
func helperFakeStreamlink(login string) {
	log := openLikeStreamlink(helperArg("--logfile"))
	logLine := func(line string) {
		if _, err := log.WriteString(line); err != nil {
			os.Exit(3)
		}
	}
	logLine("[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/" + login + "\n")
	switch os.Getenv("TTV_FAKE_STREAMLINK") {
	case "offline-hang":
		logLine("error: No playable streams found on this URL: https://www.twitch.tv/" + login + "\n")
		time.Sleep(60 * time.Second)
	case "error-exit":
		logLine("error: Unable to open URL: https://gql.twitch.tv/gql (Connection refused)")
		os.Exit(1)
	case "play":
		go func() {
			time.Sleep(400 * time.Millisecond)
			logLine("[stream.hls][info] Filtering out segments")
			time.Sleep(3 * logPollInterval)
			logLine(" and pausing stream output\n")
			time.Sleep(400 * time.Millisecond)
			logLine("[stream.hls][info] Resuming stream output\n")
		}()
		go func() {
			for {
				_, _ = os.Stdout.WriteString(mpvStream)
				time.Sleep(20 * time.Millisecond)
			}
		}()
		const ipcPrefix = "--input-ipc-server="
		args, err := posixShlexSplit(helperArg("--player-args"))
		if err != nil {
			os.Exit(2)
		}
		for _, arg := range args {
			if socket, ok := strings.CutPrefix(arg, ipcPrefix); ok {
				helperPlay(socket, login, false, helperIPCOptions{})
				return
			}
		}
		os.Exit(2)
	default:
		os.Exit(2)
	}
}

func helperArg(name string) string {
	for i, arg := range os.Args[:len(os.Args)-1] {
		if arg == name {
			return os.Args[i+1]
		}
	}
	fmt.Fprintf(os.Stderr, "missing %s\n", name)
	os.Exit(2)
	return ""
}

// openLikeStreamlink is Path(file).expanduser().resolve() and a logging.FileHandler
// in append mode, from streamlink_cli/main.py. On a pipe /dev/stderr resolves to
// /proc/PID/fd/pipe:[N], which does not exist: the error goes to stderr and the
// process ends, with nothing else started.
func openLikeStreamlink(path string) *os.File {
	resolved, err := filepath.EvalSymlinks(path)
	if _, missing := os.Lstat(path); errors.Is(missing, fs.ErrNotExist) {
		// resolve() is not strict, and the handler creates the file.
		resolved, err = path, nil
	}
	if err == nil {
		var f *os.File
		if f, err = os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			return f
		}
	}
	fmt.Fprintf(os.Stderr, "Logging setup error: %v\n", err)
	os.Exit(1)
	return nil
}
