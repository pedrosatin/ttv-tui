package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyLine(t *testing.T) {
	cases := []struct {
		line string
		want Failure
	}{
		{"error: No playable streams found on this URL: https://www.twitch.tv/xqc", FailureOffline},
		{"error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by NameResolutionError(\"Failed to resolve 'gql.twitch.tv' ([Errno -2] Name or service not known)\")))", FailureNetwork},
		{"error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by ProxyError('Unable to connect to proxy', NewConnectionError(\"Failed to establish a new connection: [Errno 111] Connection refused\"))))", FailureNetwork},
		{"error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by ConnectTimeoutError(<HTTPSConnection(host='gql.twitch.tv', port=443) at 0x7f>, 'Connection to gql.twitch.tv timed out. (connect timeout=0.001)')))", FailureTimeout},
		{"error: Failed to start player: mpv (Player executable not found)", FailureMPVMissing},
		{"bash: streamlink: command not found", FailureStreamlinkMissing},
		{"[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/xqc", FailureNone},
		{"", FailureNone},
	}
	for _, c := range cases {
		if got := ClassifyLine(c.line); got != c.want {
			t.Errorf("ClassifyLine(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestFailureOfStartErrors(t *testing.T) {
	if got := FailureOf(&MissingToolError{Tool: "mpv"}); got != FailureMPVMissing {
		t.Fatalf("mpv missing = %q", got)
	}
	if got := FailureOf(&MissingToolError{Tool: "/usr/bin/streamlink"}); got != FailureStreamlinkMissing {
		t.Fatalf("streamlink missing = %q", got)
	}
	execErr := &exec.Error{Name: "streamlink", Err: exec.ErrNotFound}
	if got := FailureOf(errors.Join(errors.New("player: start"), execErr)); got != FailureStreamlinkMissing {
		t.Fatalf("exec not found = %q", got)
	}
	if got := FailureOf(errors.New("player: something odd")); got != FailureUnknown {
		t.Fatalf("unknown = %q", got)
	}
	if got := FailureOf(nil); got != FailureNone {
		t.Fatalf("nil = %q", got)
	}
}

func waitError(t *testing.T, mgr *Manager, gen uint64, timeout time.Duration) Event {
	t.Helper()
	return waitForState(t, mgr.Events(), gen, StateError, timeout)
}

func TestOfflineLineFailsFastAndEndsProcess(t *testing.T) {
	var pid atomic.Int64
	mgr := newTestManager(t, "offline-hang", Options{SocketWait: 30 * time.Second})
	start := time.Now()
	gen, err := mgr.Start("xqc")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.mu.Lock()
	if mgr.session != nil {
		pid.Store(int64(mgr.session.Pid()))
	}
	mgr.mu.Unlock()
	ev := waitError(t, mgr, gen, 3*time.Second)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("offline took %v; must be reported within the first seconds", elapsed)
	}
	if ev.Failure != FailureOffline {
		t.Fatalf("Failure = %q, want offline", ev.Failure)
	}
	if !strings.Contains(ev.LastLine, "No playable streams found") {
		t.Fatalf("LastLine = %q, want the original streamlink line", ev.LastLine)
	}
	// The lingering helper process group must be gone without a Stop.
	p := int(pid.Load())
	if p == 0 {
		t.Fatal("no session pid recorded")
	}
	deadline := time.Now().Add(3 * time.Second)
	for groupExists(p) {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still alive after offline failure", p)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOfflineWithExitZeroIsStillError(t *testing.T) {
	mgr := newTestManager(t, "offline-exit0", Options{SocketWait: 30 * time.Second})
	gen, err := mgr.Start("xqc")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := waitError(t, mgr, gen, 3*time.Second)
	if ev.Failure != FailureOffline {
		t.Fatalf("Failure = %q, want offline", ev.Failure)
	}
	if !strings.Contains(ev.LastLine, "No playable streams") {
		t.Fatalf("LastLine = %q: the error line must win over later output", ev.LastLine)
	}
}

func TestProcessFailuresAreClassified(t *testing.T) {
	cases := []struct {
		mode       string
		socketWait time.Duration
		want       Failure
	}{
		{"network-error", 30 * time.Second, FailureNetwork},
		{"timeout-error", 30 * time.Second, FailureTimeout},
		{"mpv-missing", 30 * time.Second, FailureMPVMissing},
		{"silent-hang", 300 * time.Millisecond, FailureTimeout},
		{"exit-error", 30 * time.Second, FailureUnknown},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			mgr := newTestManager(t, c.mode, Options{SocketWait: c.socketWait})
			gen, err := mgr.Start("chan")
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			ev := waitError(t, mgr, gen, 5*time.Second)
			if ev.Failure != c.want {
				t.Fatalf("Failure = %q, want %q (line %q)", ev.Failure, c.want, ev.LastLine)
			}
			if ev.LastLine == "" {
				t.Fatal("technical detail (LastLine) must be kept")
			}
		})
	}
}

// TestFastExitKeepsLastErrorLine reproduces the race between the process
// exiting and the reader goroutines consuming its final line: the helper prints
// a recognized error line and exits at once. Many sessions run side by side to
// add scheduling pressure; every one of them must still report the network
// failure instead of falling back to "unknown".
func TestFastExitKeepsLastErrorLine(t *testing.T) {
	const runs = 60
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan string, runs)
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				runtimeDir, err := os.MkdirTemp(t.TempDir(), "run-")
				if err != nil {
					errs <- fmt.Sprintf("run %d: runtime directory: %v", i, err)
					continue
				}
				mgr := New(Options{
					Command:    helperCommand("error-exit-now"),
					RuntimeDir: runtimeDir,
					SocketWait: 30 * time.Second,
					StopWait:   500 * time.Millisecond,
				})
				gen, err := mgr.Start("chan")
				if err != nil {
					errs <- fmt.Sprintf("run %d: Start: %v", i, err)
					mgr.Close()
					continue
				}
				ev, ok := nextError(mgr.Events(), gen, 10*time.Second)
				mgr.Close()
				switch {
				case !ok:
					errs <- fmt.Sprintf("run %d: no error event", i)
				case ev.Failure != FailureNetwork:
					errs <- fmt.Sprintf("run %d: Failure = %q, want network (line %q)", i, ev.Failure, ev.LastLine)
				case !strings.Contains(ev.LastLine, "Unable to open URL"):
					errs <- fmt.Sprintf("run %d: LastLine = %q, want the error line", i, ev.LastLine)
				}
			}
		}()
	}
	for i := 0; i < runs; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// nextError is waitError without *testing.T, for use from worker goroutines.
func nextError(ch <-chan Event, gen uint64, timeout time.Duration) (Event, bool) {
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return Event{}, false
			}
			if ev.Generation == gen && ev.State == StateError {
				return ev, true
			}
		case <-deadline:
			return Event{}, false
		}
	}
}

func TestMissingToolIsReportedWithoutSpawning(t *testing.T) {
	for _, tool := range []string{"streamlink", "mpv"} {
		t.Run(tool, func(t *testing.T) {
			var spawned atomic.Int32
			mgr := New(Options{
				RuntimeDir: privateRuntimeDir(t),
				Command: func(login, socket string) *exec.Cmd {
					spawned.Add(1)
					return exec.Command(os.Args[0])
				},
				RequiredTools: []string{"streamlink", "mpv"},
				LookPath: func(file string) (string, error) {
					if file == tool {
						return "", exec.ErrNotFound
					}
					return "/usr/bin/" + file, nil
				},
			})
			t.Cleanup(mgr.Close)
			gen, err := mgr.Start("gaules")
			if err == nil {
				t.Fatal("Start must fail when a required tool is missing")
			}
			want := FailureStreamlinkMissing
			if tool == "mpv" {
				want = FailureMPVMissing
			}
			if got := FailureOf(err); got != want {
				t.Fatalf("FailureOf(err) = %q, want %q", got, want)
			}
			ev := waitError(t, mgr, gen, time.Second)
			if ev.Failure != want {
				t.Fatalf("event Failure = %q, want %q", ev.Failure, want)
			}
			if spawned.Load() != 0 {
				t.Fatal("no process may be spawned when a tool is missing")
			}
		})
	}
}

func TestStreamlinkNotOnPathIsClassified(t *testing.T) {
	mgr := New(Options{
		RuntimeDir: privateRuntimeDir(t),
		Command: func(login, socket string) *exec.Cmd {
			return exec.Command("ttv-tui-test-streamlink-that-does-not-exist")
		},
	})
	t.Cleanup(mgr.Close)
	gen, err := mgr.Start("gaules")
	if err == nil {
		t.Fatal("Start must fail")
	}
	ev := waitError(t, mgr, gen, time.Second)
	if ev.Failure != FailureStreamlinkMissing {
		t.Fatalf("Failure = %q, want streamlink_missing (err %v)", ev.Failure, err)
	}
}
