package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This test file never invokes real streamlink or mpv. Instead, when
// TTV_PLAYER_HELPER is set, the test binary re-executes itself as a helper
// that mimics streamlink (stdout/stderr lines) and mpv (IPC JSON, on a Unix socket
// or a named pipe through listenEndpoint).
// Production Manager.Command is replaced with helperCommand in each test.

func TestMain(m *testing.M) {
	if os.Getenv("TTV_PLAYER_HELPER") == "1" {
		runPlayerHelper()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func testTarget(name string) Target {
	return Target{
		URL:   "https://www.twitch.tv/" + name,
		Label: name,
	}
}

func helperCommand(mode string) CommandFunc {
	return func(target Target, socketPath string) *exec.Cmd {
		login := target.Identity()
		cmd := exec.Command(os.Args[0], "--", login, ipcServerFlag+socketPath)
		cmd.Env = append(os.Environ(),
			"TTV_PLAYER_HELPER=1",
			"TTV_PLAYER_HELPER_MODE="+mode,
			"TTV_PLAYER_HELPER_SOCKET="+socketPath,
			"TTV_PLAYER_HELPER_LOGIN="+login,
			"TTV_PLAYER_HELPER_URL="+target.URL,
		)
		return cmd
	}
}

// skipWithoutSocketFile skips a test about the endpoint being a path: the socket
// file's length limit, its place in a directory. A named pipe has neither.
func skipWithoutSocketFile(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Windows endpoint is a pipe name, not a file")
	}
}

func newTestManager(t *testing.T, mode string, opts Options) *Manager {
	t.Helper()
	opts.Command = helperCommand(mode)
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = privateRuntimeDir(t)
	}
	if opts.SocketWait == 0 {
		opts.SocketWait = 5 * time.Second
	}
	if opts.StopWait == 0 {
		opts.StopWait = 500 * time.Millisecond
	}
	mgr := New(opts)
	t.Cleanup(func() {
		mgr.Close()
	})
	return mgr
}

// privateRuntimeDir is a 0700 runtime base. t.TempDir() returns a 0755
// directory, which the session refuses on Unix, sending every test to the
// same shared fallback.
func privateRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	return dir
}

func waitForState(t *testing.T, ch <-chan Event, gen uint64, want State, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("events channel closed")
			}
			if ev.Generation != gen {
				continue
			}
			if ev.State == want {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout waiting for state %s (gen %d)", want, gen)
			return Event{}
		}
	}
}

func TestPlayingOnlyAfterIPCEvent(t *testing.T) {
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start(testTarget("gaules"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	if ev.State != StateStarting {
		t.Fatalf("first useful state = %s, want iniciando", ev.State)
	}
	// Helper delays playback-restart; ensure we are not playing yet for a moment.
	select {
	case ev := <-mgr.Events():
		if ev.Generation == gen && ev.State == StatePlaying {
			t.Fatal("playing reported before helper sent playback-restart")
		}
	case <-time.After(100 * time.Millisecond):
	}
	play := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if play.Login != "gaules" {
		t.Fatalf("login = %q, want gaules", play.Login)
	}
}

func TestChannelSwitchABCLeavesOnlyC(t *testing.T) {
	mgr := newTestManager(t, "play-child", Options{StopWait: 300 * time.Millisecond})

	genA, err := mgr.Start(testTarget("channel-a"))
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genA, StateStarting, 3*time.Second)

	genB, err := mgr.Start(testTarget("channel-b"))
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genB, StateStarting, 3*time.Second)

	genC, err := mgr.Start(testTarget("channel-c"))
	if err != nil {
		t.Fatalf("Start C: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genC, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		t.Fatal("expected active session for C")
	}
	pid := sess.Pid()
	if pid <= 0 {
		t.Fatal("expected positive pid for C")
	}

	// Allow A/B teardown to finish.
	time.Sleep(400 * time.Millisecond)

	if !pidExists(pid) {
		t.Fatalf("channel C process %d not alive", pid)
	}

	// No other helper processes for A/B should remain under our runtime dir naming;
	// verify process group of C has living members, then Stop and ensure gone.
	pgid, err := groupOfPid(pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	mgr.Stop()
	time.Sleep(300 * time.Millisecond)

	if pidExists(pid) {
		t.Fatalf("pid %d still alive after Stop", pid)
	}
	if groupExists(pgid) {
		t.Fatalf("process group %d still reachable after Stop", pgid)
	}
	_ = genA
	_ = genB
}

func TestSIGTERMIgnoredEscalatesToSIGKILL(t *testing.T) {
	mgr := newTestManager(t, "ignore-term", Options{StopWait: 200 * time.Millisecond})
	gen, err := mgr.Start(testTarget("stubborn"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)

	mgr.mu.Lock()
	pid := mgr.session.Pid()
	mgr.mu.Unlock()

	done := make(chan struct{})
	go func() {
		mgr.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not return after SIGKILL escalation")
	}
	if pidExists(pid) {
		t.Fatalf("pid %d still alive after Kill escalation", pid)
	}
}

func TestSocketEOFBecomesEnded(t *testing.T) {
	mgr := newTestManager(t, "socket-eof", Options{})
	gen, err := mgr.Start(testTarget("eofchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	ev := waitForState(t, mgr.Events(), gen, StateEnded, 5*time.Second)
	// Process stays alive after closing the IPC connection → mpv_dead.
	if ev.Reason != EndReasonMPVDead && ev.Reason != EndReasonSocketEOF {
		t.Fatalf("reason = %q, want mpv_dead or socket_eof", ev.Reason)
	}
}

func TestProcessErrorBecomesErrorWithLastLine(t *testing.T) {
	mgr := newTestManager(t, "exit-error", Options{SocketWait: 2 * time.Second})
	gen, err := mgr.Start(testTarget("badchan"))
	// Start itself may succeed (process spawned); error arrives asynchronously.
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	ev := waitForState(t, mgr.Events(), gen, StateError, 5*time.Second)
	if !strings.Contains(ev.LastLine, "NO_PLAYABLE_STREAMS") {
		t.Fatalf("LastLine = %q, want it to contain NO_PLAYABLE_STREAMS", ev.LastLine)
	}
}

func TestStaleGenerationEventsAreDiscardable(t *testing.T) {
	mgr := newTestManager(t, "play", Options{})
	genA, err := mgr.Start(testTarget("aaa"))
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	genB, err := mgr.Start(testTarget("bbb"))
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}
	if genB <= genA {
		t.Fatalf("genB=%d genA=%d, want B > A", genB, genA)
	}

	current := genB
	seenB := false
	deadline := time.After(5 * time.Second)
	for !seenB {
		select {
		case ev := <-mgr.Events():
			if ev.Generation < current {
				// Consumer must discard — prove we can identify them.
				continue
			}
			if ev.Generation != current {
				t.Fatalf("unexpected future gen %d", ev.Generation)
			}
			if ev.State == StatePlaying {
				seenB = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for B playing")
		}
	}
}

func TestSocketRemovedAfterExit(t *testing.T) {
	dir := t.TempDir()
	mgr := newTestManager(t, "play", Options{RuntimeDir: dir})
	gen, err := mgr.Start(testTarget("sockchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	socket := ""
	sessionDir := ""
	if mgr.session != nil {
		socket = mgr.session.endpoint
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()
	if socket == "" {
		t.Fatal("expected socket path")
	}
	if !endpointPresent(socket) {
		t.Fatalf("socket should exist: %s", socket)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if !endpointGone(socket) {
		t.Fatalf("socket still present after Stop: %s", socket)
	}
	if sessionDir != "" {
		if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
			t.Fatalf("session dir still present after Stop: %v", err)
		}
	}
}

// TestPreRollHoldEntersAndLeavesBreak: streamlink holds output for a pre-roll
// (paired start/end lines) while mpv is starting; the session shows the interval
// and returns to playing on the end line.
func TestPreRollHoldEntersAndLeavesBreak(t *testing.T) {
	mgr := newTestManager(t, "ad-then-play", Options{})
	gen, err := mgr.Start(testTarget("adchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	br := waitForState(t, mgr.Events(), gen, StateBreak, 5*time.Second)
	if br.State != StateBreak {
		t.Fatalf("state = %s", br.State)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
}

func sessionState(mgr *Manager) (State, bool) {
	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		return "", false
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.state, sess.adHold
}

// expectNoBreak drains events for d and fails on any interval evidence.
func expectNoBreak(t *testing.T, ch <-chan Event, gen uint64, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("events channel closed")
			}
			if ev.Generation != gen {
				continue
			}
			if ev.State == StateBreak || ev.BreakElapsed > 0 {
				t.Fatalf("unexpected interval: state=%s elapsed=%s", ev.State, ev.BreakElapsed)
			}
		case <-deadline:
			return
		}
	}
}

// TestIPCSilenceNeverEntersBreak: after the initial events the fake mpv stays
// silent (stable idle-active/pause/volume). That is normal playback.
func TestIPCSilenceNeverEntersBreak(t *testing.T) {
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start(testTarget("silentchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	start := time.Now()
	expectNoBreak(t, mgr.Events(), gen, 5500*time.Millisecond)
	st, hold := sessionState(mgr)
	t.Logf("after %s of IPC silence: state=%s adHold=%v", time.Since(start).Round(100*time.Millisecond), st, hold)
	if st != StatePlaying || hold {
		t.Fatalf("state=%s adHold=%v, want tocando without hold", st, hold)
	}
}

// TestAdHoldStartAndEndRecoversWithoutInteraction: paired streamlink lines mid
// playback enter the interval, the counter grows, and the end line returns to
// playing with no user input.
func TestAdHoldStartAndEndRecoversWithoutInteraction(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_ADS", "hold-end")
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start(testTarget("adchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	_ = waitForState(t, mgr.Events(), gen, StateBreak, 5*time.Second)

	sawCounter := false
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-mgr.Events():
			if ev.Generation != gen {
				continue
			}
			switch ev.State {
			case StateBreak:
				if ev.BreakElapsed >= time.Second {
					sawCounter = true
				}
			case StatePlaying:
				if ev.BreakElapsed != 0 {
					t.Fatalf("playing event carries counter %s", ev.BreakElapsed)
				}
				done = true
			default:
				t.Fatalf("unexpected state %s during hold", ev.State)
			}
		case <-deadline:
			t.Fatal("interval did not end after the hold-end line")
		}
	}
	if !sawCounter {
		t.Fatal("counter never advanced during the hold")
	}
	expectNoBreak(t, mgr.Events(), gen, time.Second)
	if st, hold := sessionState(mgr); st != StatePlaying || hold {
		t.Fatalf("state=%s adHold=%v after hold end", st, hold)
	}
}

// TestAdInfoLinesAloneDoNotClaimBreak: lines that only announce ads, without
// streamlink's paired hold lines, do not create an interval.
func TestAdInfoLinesAloneDoNotClaimBreak(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_ADS", "info-only")
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start(testTarget("infochan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	expectNoBreak(t, mgr.Events(), gen, 2*time.Second)
	if st, hold := sessionState(mgr); st != StatePlaying || hold {
		t.Fatalf("state=%s adHold=%v, want tocando", st, hold)
	}
}

// TestAdHoldWithoutEndIsBounded: a hold-start line with no end line does not
// trap the app; past BreakMaxHold it stops claiming an interval on its own.
func TestAdHoldWithoutEndIsBounded(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_ADS", "hold-only")
	mgr := newTestManager(t, "play", Options{BreakMaxHold: 1200 * time.Millisecond})
	gen, err := mgr.Start(testTarget("holdchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	_ = waitForState(t, mgr.Events(), gen, StateBreak, 5*time.Second)
	back := waitForState(t, mgr.Events(), gen, StatePlaying, 4*time.Second)
	if back.BreakElapsed != 0 {
		t.Fatalf("counter after withdrawal = %s", back.BreakElapsed)
	}
	expectNoBreak(t, mgr.Events(), gen, 1500*time.Millisecond)
	if st, hold := sessionState(mgr); st != StatePlaying || hold {
		t.Fatalf("state=%s adHold=%v after ceiling", st, hold)
	}
}

// TestAdHoldOldGenerationIgnored: a hold from a previous channel cannot put the
// new generation in the interval state.
func TestAdHoldOldGenerationIgnored(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_ADS", "hold-only")
	mgrA := newTestManager(t, "play", Options{})
	genA, err := mgrA.Start(testTarget("chan-a"))
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgrA.Events(), genA, StateBreak, 5*time.Second)
	t.Setenv("TTV_PLAYER_HELPER_ADS", "")
	genB, err := mgrA.Start(testTarget("chan-b"))
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}
	_ = waitForState(t, mgrA.Events(), genB, StatePlaying, 5*time.Second)
	expectNoBreak(t, mgrA.Events(), genB, time.Second)
}

// TestPauseDuringHoldKeepsPausedThenResumesBreak covers the state machine
// without a process: pausing shows paused, unpausing while streamlink still
// holds shows the interval again, and the end line returns to playing.
func TestPauseDuringHoldKeepsPausedThenResumesBreak(t *testing.T) {
	var got []Event
	s := &Session{
		cfg:   sessionConfig{Generation: 7, Target: testTarget("x"), Emit: func(ev Event) { got = append(got, ev) }},
		state: StatePlaying,
	}
	s.startAdHold()
	s.applyPauseObservation(true)
	s.applyPauseObservation(false)
	s.endAdHold()
	var states []State
	for _, ev := range got {
		states = append(states, ev.State)
	}
	want := []State{StateBreak, StatePaused, StateBreak, StatePlaying}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Fatalf("states=%v want %v", states, want)
	}
	if s.adHold {
		t.Fatal("hold still active after end line")
	}
}

func TestClassifyAdLine(t *testing.T) {
	cases := []struct {
		line string
		want adSignal
	}{
		{"[stream.hls][info] Filtering out segments and pausing stream output", adSignalHoldStart},
		{"[stream.hls][info] Resuming stream output", adSignalHoldEnd},
		{"[plugins.twitch][info] Waiting for pre-roll ads to finish, be patient", adSignalNone},
		{"[plugins.twitch][info] Detected advertisement break of 15 seconds", adSignalNone},
		{"[info] Opening stream...", adSignalNone},
		{"[stream.hls][warning] Encountered a stream discontinuity. This is unsupported and will result in incoherent output data.", adSignalNone},
		{"[stream.hls][info] Stream output resumed", adSignalUnknown},
		{"[stream.hls][info] Filtering out segments, stream output paused", adSignalUnknown},
		{"[stream.hls][info] Filtering out segments and pausing stream output\r", adSignalHoldStart},
		// O mesmo texto dentro de outra linha é de terceiros: o nome de um
		// stream, a reason phrase de um servidor, outro logger ou outro nível.
		{"[cli][info] Opening stream: Filtering out segments and pausing stream output (hls)", adSignalUnknown},
		{"error: Unable to open URL: https://example.com/live.m3u8 (404 Client Error: Filtering out segments and pausing stream output for url: https://example.com/live.m3u8)", adSignalUnknown},
		{"[plugins.twitch][info] Filtering out segments and pausing stream output", adSignalUnknown},
		{"[stream.hls][warning] Filtering out segments and pausing stream output", adSignalUnknown},
		{"[stream.hls][info] Filtering out segments and pausing stream output now", adSignalUnknown},
		{"x[stream.hls][info] Filtering out segments and pausing stream output", adSignalUnknown},
		{"[cli][info] Opening stream: Resuming stream output (hls)", adSignalUnknown},
		{"", adSignalNone},
	}
	for _, tc := range cases {
		if got := classifyAdLine(tc.line); got != tc.want {
			t.Fatalf("classifyAdLine(%q)=%v want %v", tc.line, got, tc.want)
		}
	}
}

// runPlayerHelper is the fake streamlink+mpv process.
func runPlayerHelper() {
	mode := os.Getenv("TTV_PLAYER_HELPER_MODE")
	socket := os.Getenv("TTV_PLAYER_HELPER_SOCKET")
	login := os.Getenv("TTV_PLAYER_HELPER_LOGIN")

	switch mode {
	case "play":
		helperPlay(socket, login, false, helperIPCOptions{})
	case "play-child":
		helperPlay(socket, login, true, helperIPCOptions{})
	case "play-control":
		helperPlay(socket, login, false, helperIPCOptions{
			ReplyDelay:       helperDelay(),
			EmitPropOnSet:    true,
			LogCommandsPath:  os.Getenv("TTV_PLAYER_HELPER_CMDS"),
			DeferPropOnPause: os.Getenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP") == "1",
			InitialVolume:    helperInitialVolume(),
			InitialPause:     os.Getenv("TTV_PLAYER_HELPER_INIT_PAUSE") == "1",
			ExternalVolume:   helperEnvFloat("TTV_PLAYER_HELPER_EXTERNAL_VOLUME"),
		})
	case "ad-then-play":
		fmt.Println("[plugins.twitch][info] Waiting for pre-roll ads to finish, be patient")
		fmt.Println("[stream.hls][info] Filtering out segments and pausing stream output")
		go func() {
			time.Sleep(900 * time.Millisecond)
			fmt.Println("[stream.hls][info] Resuming stream output")
		}()
		helperPlay(socket, login, false, helperIPCOptions{})
	case "socket-no-listener":
		helperSocketNoListener(socket)
	case "idle-child":
		// Stays until signalled, with the default reaction to SIGTERM.
		time.Sleep(time.Hour)
	case "ignore-term":
		helperIgnoreTerm(socket)
	case "exit-error":
		fmt.Fprintln(os.Stderr, "error: NO_PLAYABLE_STREAMS for channel")
		time.Sleep(50 * time.Millisecond)
		os.Exit(1)
	case "long-line-offline":
		fmt.Fprintln(os.Stderr, strings.Repeat("x", 2*1024*1024))
		fmt.Fprintln(os.Stderr, "error: No playable streams found on this URL")
		time.Sleep(60 * time.Second)
	case "offline-hang":
		// streamlink's offline line, then a process that would linger: the
		// session must end it instead of waiting.
		fmt.Fprintln(os.Stderr, "[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/"+login)
		fmt.Fprintln(os.Stderr, "error: No playable streams found on this URL: https://www.twitch.tv/"+login)
		time.Sleep(60 * time.Second)
	case "offline-exit0":
		fmt.Fprintln(os.Stderr, "error: No playable streams found on this URL: https://www.twitch.tv/"+login)
		fmt.Fprintln(os.Stderr, "[cli][info] Closing currently open stream...")
	case "network-error":
		fmt.Fprintln(os.Stderr, "error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by NewConnectionError(\"Failed to establish a new connection: [Errno 111] Connection refused\")))")
		os.Exit(1)
	case "error-exit-now":
		// A failure line as the very last output, then an immediate exit: the
		// process is gone before the session has necessarily read the line.
		fmt.Println("[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/" + login)
		fmt.Fprintln(os.Stderr, "[cli][info] Available streams: audio_only, 160p, 360p, 480p, 720p60, 1080p60")
		fmt.Fprintln(os.Stderr, "error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by NewConnectionError(\"Failed to establish a new connection: [Errno 111] Connection refused\")))")
		os.Exit(1)
	case "plugin-error":
		fmt.Fprintln(os.Stderr, "[cli][info] Found matching plugin twitch for URL https://www.twitch.tv/"+login)
		fmt.Fprintln(os.Stderr, "error: Unable to validate response text: ValidationError(dict): Key 'data' not found in <dict>")
		os.Exit(1)
	case "kick-no-browser":
		// streamlink 8.6.2 when the Kick API answers 403 and no Chromium-based
		// browser is found to solve the challenge, then exit 1.
		fmt.Fprintln(os.Stderr, "[cli][info] Found matching plugin kick for URL https://kick.com/"+login)
		fmt.Fprintln(os.Stderr, "[plugins.kick][info] Solving JS challenge")
		fmt.Fprintln(os.Stderr, "[plugins.kick][error] Could not find Chromium-based web browser executable: Please set the path to a supported web browser using --webbrowser-executable")
		fmt.Fprintln(os.Stderr, "error: Error while querying Kick API: 403 Client Error: Forbidden for url: https://kick.com/api/v2/channels/"+login+"/livestream")
		os.Exit(1)
	case "python-traceback":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):")
		fmt.Fprintln(os.Stderr, "  File \"streamlink/plugins/twitch.py\", line 12, in <module>")
		fmt.Fprintln(os.Stderr, "ModuleNotFoundError: No module named 'streamlink.compat'")
		os.Exit(1)
	case "echo-offline":
		// streamlink 8.6.2 com um canal offline: o eco da URL em nível info
		// (main.py) e a linha de offline, saída 1.
		url := os.Getenv("TTV_PLAYER_HELPER_URL")
		fmt.Fprintln(os.Stderr, "[cli][info] Found matching plugin twitch for URL "+url)
		fmt.Fprintln(os.Stderr, "error: No playable streams found on this URL: "+url)
		os.Exit(1)
	case "echo-exit0":
		// Uma sessão normal que termina com 0 depois do eco da URL.
		url := os.Getenv("TTV_PLAYER_HELPER_URL")
		fmt.Fprintln(os.Stderr, "[cli][info] Found matching plugin twitch for URL "+url)
		fmt.Fprintln(os.Stderr, "[cli][info] Available streams: audio_only, 160p (worst), 1080p60 (best)")
		fmt.Fprintln(os.Stderr, "[cli][info] Opening stream: 1080p60 (hls)")
		fmt.Fprintln(os.Stderr, "[cli][info] Stream ended")
		fmt.Fprintln(os.Stderr, "[cli][info] Closing currently open stream...")
	case "weak-then-offline":
		// Uma linha de erro com classe fraca e depois a de offline: a causa
		// verificável posterior vale.
		url := os.Getenv("TTV_PLAYER_HELPER_URL")
		fmt.Fprintln(os.Stderr, "[plugins.twitch][error] PluginError: Unable to parse the access token for "+url)
		fmt.Fprintln(os.Stderr, "error: No playable streams found on this URL: "+url)
		os.Exit(1)
	case "traceback-exit0":
		// Um traceback real seguido de saída 0: a classe vem da linha da
		// exceção, não do status de saída.
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):")
		fmt.Fprintln(os.Stderr, "  File \"streamlink/plugins/twitch.py\", line 12, in _get_streams")
		fmt.Fprintln(os.Stderr, "KeyError: 'x'")
	case "traceback-bare-exit0":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):")
		fmt.Fprintln(os.Stderr, "  File \"streamlink/plugins/twitch.py\", line 12, in _get_streams")
		fmt.Fprintln(os.Stderr, "KeyError")
	case "bare-name-exit0":
		// O mesmo nome sozinho, sem traceback antes: não prova nada.
		fmt.Fprintln(os.Stderr, "KeyError")
	case "exit-silent":
		// Exits with an error status and says nothing that explains why.
		os.Exit(3)
	case "timeout-error":
		fmt.Fprintln(os.Stderr, "error: Unable to open URL: https://gql.twitch.tv/gql (HTTPSConnectionPool(host='gql.twitch.tv', port=443): Max retries exceeded with url: /gql (Caused by ConnectTimeoutError(<HTTPSConnection(host='gql.twitch.tv', port=443)>, 'Connection to gql.twitch.tv timed out. (connect timeout=20.0)')))")
		os.Exit(1)
	case "mpv-missing":
		fmt.Fprintln(os.Stderr, "[cli][info] Starting player: mpv")
		fmt.Fprintln(os.Stderr, "error: Failed to start player: mpv (Player executable not found)")
		time.Sleep(60 * time.Second)
	case "silent-hang":
		time.Sleep(60 * time.Second)
	case "socket-eof":
		helperSocketEOF(socket)
	case "exit-after-play":
		helperExitAfterPlay(socket)
	case "term-parent-stubborn-child":
		helperTermParentStubbornChild(socket, login)
	case "stubborn-child":
		helperStubbornChild()
	case "swap-on-term":
		helperSwapOnTerm()
	case "session-owner":
		helperSessionOwner()
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(2)
	}
}

func helperDelay() time.Duration {
	ms := os.Getenv("TTV_PLAYER_HELPER_REPLY_DELAY_MS")
	if ms == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(ms, "%d", &n); err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Millisecond
}

type helperIPCOptions struct {
	ReplyDelay       time.Duration
	EmitPropOnSet    bool
	LogCommandsPath  string
	DeferPropOnPause bool
	// InitialVolume is the first volume property-change (mpv softvol); 0 means 100.
	InitialVolume float64
	InitialPause  bool
	// ExternalVolume is a volume change nobody asked for over IPC, as a key
	// pressed in the mpv window: sent once, after the session's baseline.
	ExternalVolume float64
}

func helperInitialVolume() float64 {
	return helperEnvFloat("TTV_PLAYER_HELPER_INIT_VOLUME")
}

func helperEnvFloat(name string) float64 {
	s := strings.TrimSpace(os.Getenv(name))
	if s == "" {
		return 0
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return 0
	}
	return v
}

func helperPlay(socket, login string, withChild bool, opts helperIPCOptions) {
	fmt.Fprintf(os.Stderr, "[info] opening https://www.twitch.tv/%s\n", login)

	var child *os.Process
	if withChild {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER_MODE=idle-child")
		if err := cmd.Start(); err == nil {
			child = cmd.Process
			defer func() {
				_ = child.Kill()
				_, _ = child.Wait()
			}()
		}
	}

	// Delay before binding so tests can observe "iniciando" with no socket yet.
	time.Sleep(250 * time.Millisecond)

	go helperAdScript(os.Getenv("TTV_PLAYER_HELPER_ADS"))

	ln, err := listenEndpoint(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = ln.Close() }()
	_ = os.Chmod(socket, 0o600)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeMPV(conn, 30*time.Millisecond, opts)
		}
	}()

	// Stay alive until signalled.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	_ = ln.Close()
	_ = os.Remove(socket)
}

// helperAdScript prints streamlink-style ad lines on stdout after playback has
// started. The fake mpv stays silent meanwhile, as the real one does.
func helperAdScript(script string) {
	if script == "" {
		return
	}
	time.Sleep(600 * time.Millisecond)
	switch script {
	case "hold-end":
		fmt.Println("[plugins.twitch][info] Detected advertisement break of 2 seconds")
		fmt.Println("[stream.hls][info] Filtering out segments and pausing stream output")
		time.Sleep(2200 * time.Millisecond)
		fmt.Println("[stream.hls][info] Resuming stream output")
	case "hold-only":
		fmt.Println("[stream.hls][info] Filtering out segments and pausing stream output")
	case "info-only":
		fmt.Println("[plugins.twitch][info] Waiting for pre-roll ads to finish, be patient")
		fmt.Println("[plugins.twitch][info] Detected advertisement break of 30 seconds")
	}
}

func helperExitAfterPlay(socket string) {
	ln, err := listenEndpoint(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	conn, err := ln.Accept()
	if err != nil {
		fmt.Fprintf(os.Stderr, "accept: %v\n", err)
		os.Exit(1)
	}
	_, _ = conn.Write([]byte(`{"event":"playback-restart"}` + "\n"))
	_, _ = conn.Write([]byte(`{"event":"property-change","name":"idle-active","data":false}` + "\n"))
	time.Sleep(150 * time.Millisecond)
	_ = conn.Close()
	_ = ln.Close()
	_ = os.Remove(socket)
	os.Exit(0)
}

func serveFakeMPV(conn net.Conn, delay time.Duration, opts helperIPCOptions) {
	defer func() { _ = conn.Close() }()
	if delay > 0 {
		time.Sleep(delay)
	}
	initVol := opts.InitialVolume
	if initVol == 0 {
		initVol = 100
	}
	_, _ = conn.Write([]byte(`{"event":"playback-restart"}` + "\n"))
	_, _ = conn.Write([]byte(`{"event":"property-change","name":"idle-active","data":false}` + "\n"))
	pauseLine, _ := json.Marshal(map[string]any{
		"event": "property-change",
		"name":  "pause",
		"data":  opts.InitialPause,
	})
	_, _ = conn.Write(append(pauseLine, '\n'))
	volLine, _ := json.Marshal(map[string]any{
		"event": "property-change",
		"name":  "volume",
		"data":  initVol,
	})
	_, _ = conn.Write(append(volLine, '\n'))

	sc := json.NewDecoder(conn)
	for {
		var msg map[string]any
		if err := sc.Decode(&msg); err != nil {
			return
		}
		cmd, ok := msg["command"]
		if !ok {
			continue
		}
		if opts.LogCommandsPath != "" {
			line, _ := json.Marshal(msg)
			f, err := os.OpenFile(opts.LogCommandsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
			if err == nil {
				_, _ = f.Write(append(line, '\n'))
				_ = f.Close()
			}
		}

		reqID, _ := msg["request_id"].(float64)
		cmdList, _ := cmd.([]any)
		name := ""
		if len(cmdList) > 0 {
			name, _ = cmdList[0].(string)
		}

		if opts.ReplyDelay > 0 && (name == "set_property" || name == "cycle") {
			time.Sleep(opts.ReplyDelay)
		}

		resp := map[string]any{"error": "success"}
		if reqID != 0 {
			resp["request_id"] = int64(reqID)
		}
		raw, _ := json.Marshal(resp)
		if _, err := conn.Write(append(raw, '\n')); err != nil {
			return
		}

		if !opts.EmitPropOnSet || name != "set_property" || len(cmdList) < 3 {
			continue
		}
		prop, _ := cmdList[1].(string)
		if prop == "pause" && opts.ExternalVolume != 0 {
			// The baseline sets volume, then pause: this lands after both.
			rawVol, _ := json.Marshal(map[string]any{
				"event": "property-change",
				"name":  "volume",
				"data":  opts.ExternalVolume,
			})
			opts.ExternalVolume = 0
			if _, err := conn.Write(append(rawVol, '\n')); err != nil {
				return
			}
		}
		if prop == "pause" && opts.DeferPropOnPause {
			continue
		}
		propEv := map[string]any{
			"event": "property-change",
			"name":  prop,
			"data":  cmdList[2],
		}
		rawProp, _ := json.Marshal(propEv)
		if _, err := conn.Write(append(rawProp, '\n')); err != nil {
			return
		}
	}
}

func helperIgnoreTerm(socket string) {
	signal.Ignore(syscall.SIGTERM)
	ln, err := listenEndpoint(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeMPV(conn, 0, helperIPCOptions{})
		}
	}()
	// Block forever (until SIGKILL).
	select {}
}

func helperSocketEOF(socket string) {
	ln, err := listenEndpoint(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	conn, err := ln.Accept()
	if err != nil {
		fmt.Fprintf(os.Stderr, "accept: %v\n", err)
		os.Exit(1)
	}
	_, _ = conn.Write([]byte(`{"event":"playback-restart"}` + "\n"))
	// Close without reading client observe_property writes. That often yields
	// ECONNRESET (not a clean EOF) on the supervisor side; the product must
	// treat both as ended while this process stays alive until SIGTERM.
	_ = conn.Close()
	_ = ln.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	_ = os.Remove(socket)
}

func TestClampVolume(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},
		{100, 100},
		{-5, 0},
		{105, 100},
		{50, 50},
	}
	for _, tc := range cases {
		if got := clampVolume(tc.in); got != tc.want {
			t.Fatalf("clampVolume(%d)=%d want %d", tc.in, got, tc.want)
		}
	}
}

// TestPlaybackBaselineSyncsVolumeAndPause: mpv may open at softvol 102 and/or
// paused; after IPC connect the session forces volume 100 and pause=false so the
// status bar matches the player, then manual controls still work.
func TestPlaybackBaselineSyncsVolumeAndPause(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "baseline-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	t.Setenv("TTV_PLAYER_HELPER_INIT_VOLUME", "102")
	t.Setenv("TTV_PLAYER_HELPER_INIT_PAUSE", "1")
	t.Setenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP", "0")

	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("baseline"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	play := waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if play.State != StatePlaying {
		t.Fatalf("state=%s, want tocando after baseline unpause", play.State)
	}
	waitVolume(t, mgr, gen, 100, 3*time.Second)
	if mgr.Volume() != 100 {
		t.Fatalf("Volume=%d, want 100 (baseline must override softvol 102)", mgr.Volume())
	}

	// Both baselines must reach mpv, in any order. The session has to end up
	// playing with the pause lifted: the helper opened paused.
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","volume",100]`, `["set_property","pause",false]`)
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	raw, err := os.ReadFile(cmdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "show-text") {
		t.Fatalf("baseline displayed OSD: %s", raw)
	}
	drainEvents(mgr)

	// Manual controls still work after the baseline.
	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePaused, 5*time.Second)
	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause resume: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	if err := mgr.AdjustVolume(-5); err != nil {
		t.Fatalf("AdjustVolume: %v", err)
	}
	waitVolume(t, mgr, gen, 95, 3*time.Second)
	waitCommands(t, cmdLog, 5*time.Second,
		`{"_flags":["expand-properties"],"name":"show-text","text":"${?pause==yes:‖}${?pause==no:▶}"}`,
		`{"_flags":["expand-properties"],"name":"show-text","text":"${volume}%"}`)
}

func TestPauseCommandCorrelatesByRequestID(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	t.Setenv("TTV_PLAYER_HELPER_REPLY_DELAY_MS", "200")
	t.Setenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP", "1")

	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("pausechan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}

	// While the helper delays the reply, state must stay playing.
	select {
	case ev := <-mgr.Events():
		if ev.Generation == gen && ev.State == StatePaused {
			t.Fatal("state became paused before correlated reply")
		}
	case <-time.After(80 * time.Millisecond):
	}

	ev := waitForState(t, mgr.Events(), gen, StatePaused, 5*time.Second)
	if ev.State != StatePaused {
		t.Fatalf("state=%s", ev.State)
	}

	waitCommands(t, cmdLog, 5*time.Second, `["set_property","pause",true],"request_id":`)
}

func TestStaleRequestIDReplyIgnored(t *testing.T) {
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("stalechan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		t.Fatal("expected session")
	}

	// Late reply for a request_id that is no longer pending must not change state.
	sess.handleCommandReply(ipcMessage{
		Error:     "success",
		RequestID: 999999,
	})
	select {
	case ev := <-mgr.Events():
		if ev.Generation == gen && ev.State == StatePaused {
			t.Fatal("unknown request_id reply must not pause")
		}
	case <-time.After(100 * time.Millisecond):
	}

	// Superseded id: was pending, then removed; late success ignored.
	sess.cmdMu.Lock()
	sess.pending[42] = pendingCommand{kind: pendingPause, paused: true}
	delete(sess.pending, 42)
	sess.cmdMu.Unlock()
	sess.handleCommandReply(ipcMessage{Error: "success", RequestID: 42})
	select {
	case ev := <-mgr.Events():
		if ev.Generation == gen && ev.State == StatePaused {
			t.Fatal("superseded request_id reply must not pause")
		}
	case <-time.After(100 * time.Millisecond):
	}

	// Matching id still works.
	sess.cmdMu.Lock()
	sess.pending[7] = pendingCommand{kind: pendingPause, paused: true}
	sess.cmdMu.Unlock()
	sess.handleCommandReply(ipcMessage{Error: "success", RequestID: 7})
	_ = waitForState(t, mgr.Events(), gen, StatePaused, 2*time.Second)
}

func TestSupersededPauseReplyIgnored(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_REPLY_DELAY_MS", "250")
	t.Setenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP", "1")

	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("superchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	if err := mgr.TogglePause(); err != nil { // intent: pause
		t.Fatalf("TogglePause 1: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := mgr.TogglePause(); err != nil { // intent: unpause (flips desired)
		t.Fatalf("TogglePause 2: %v", err)
	}

	// Latest correlated intent is unpause → end state playing; a late pause reply is dropped.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation != gen {
				continue
			}
			if ev.State == StatePlaying {
				mgr.mu.Lock()
				sess := mgr.session
				mgr.mu.Unlock()
				if sess != nil {
					sess.mu.Lock()
					paused := sess.paused
					sess.mu.Unlock()
					if !paused {
						return
					}
				}
			}
		case <-deadline:
			mgr.mu.Lock()
			sess := mgr.session
			mgr.mu.Unlock()
			if sess != nil {
				sess.mu.Lock()
				paused := sess.paused
				st := sess.state
				sess.mu.Unlock()
				if st == StatePlaying && !paused {
					return
				}
				t.Fatalf("timeout; state=%s paused=%v", st, paused)
			}
			t.Fatal("timeout waiting for final playing state")
		}
	}
}

func TestPreviousGenerationEventDoesNotAffectCurrent(t *testing.T) {
	mgr := newTestManager(t, "play-control", Options{})
	genA, err := mgr.Start(testTarget("gen-a"))
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genA, StatePlaying, 5*time.Second)

	genB, err := mgr.Start(testTarget("gen-b"))
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genB, StatePlaying, 5*time.Second)

	// Synthesize a stale event as a consumer would see — Manager still tags generation.
	// Prove the active session volume/state are B's after an A-tagged pause event is ignored
	// by the consumer pattern (and A session is gone).
	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil || sess.cfg.Generation != genB {
		t.Fatalf("active session gen=%v want %d", sess, genB)
	}

	// Emit a fake stale event into the channel (as if a late A callback fired).
	mgr.emit(Event{Generation: genA, State: StatePaused, Login: "gen-a", Volume: 10})

	current := mgr.Generation()
	if current != genB {
		t.Fatalf("Generation=%d want %d", current, genB)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation < genB {
				continue // discard — same rule the UI uses
			}
			if ev.Generation == genB && ev.State == StatePaused && ev.Login == "gen-a" {
				t.Fatal("current generation carried stale A pause")
			}
			if ev.Generation == genB {
				return
			}
		case <-deadline:
			return // no harmful current-gen event
		}
	}
}

func TestVolumeClampOnAdjust(t *testing.T) {
	t.Setenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP", "0")
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("volchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	if err := mgr.SetVolume(100); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	waitVolume(t, mgr, gen, 100, 3*time.Second)

	if err := mgr.AdjustVolume(5); err != nil {
		t.Fatalf("AdjustVolume +5: %v", err)
	}
	waitVolume(t, mgr, gen, 100, 3*time.Second)

	if err := mgr.SetVolume(0); err != nil {
		t.Fatalf("SetVolume 0: %v", err)
	}
	waitVolume(t, mgr, gen, 0, 3*time.Second)

	if err := mgr.AdjustVolume(-5); err != nil {
		t.Fatalf("AdjustVolume -5: %v", err)
	}
	waitVolume(t, mgr, gen, 0, 3*time.Second)

	if err := mgr.AdjustVolume(5); err != nil {
		t.Fatalf("AdjustVolume +5 from 0: %v", err)
	}
	waitVolume(t, mgr, gen, 5, 3*time.Second)
}

// waitCommands reads the fake mpv's command log until every want is there, in
// any order. A want is a piece of a log line, so it should name the command and
// its arguments: a bare "pause" already matches observe_property.
func waitCommands(t *testing.T, path string, timeout time.Duration, want ...string) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read command log: %v", err)
		}
		missing := slices.DeleteFunc(slices.Clone(want), func(w string) bool {
			return strings.Contains(string(data), w)
		})
		if len(missing) == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("mpv never received %q, log so far:\n%s", missing, data)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// waitCommandsSettled waits until mpv has replied to every command sent and the
// session is in want. Replies follow the events the helper sent on connect, so
// none of those can still change the state afterwards.
func waitCommandsSettled(t *testing.T, mgr *Manager, want State, timeout time.Duration) {
	t.Helper()
	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		t.Fatal("expected session")
	}
	deadline := time.After(timeout)
	for {
		sess.cmdMu.Lock()
		pending := len(sess.pending)
		sess.cmdMu.Unlock()
		state, _ := sessionState(mgr)
		if pending == 0 && state == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("session did not settle: %d commands unanswered, state=%s, want %s", pending, state, want)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// drainEvents drops what is queued, so a later wait sees only what follows.
func drainEvents(mgr *Manager) {
	for {
		select {
		case <-mgr.Events():
		default:
			return
		}
	}
}

// lastVolumeSet is the last volume the session asked mpv for.
func lastVolumeSet(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.Contains(line, `"set_property","volume"`) {
			last = line
		}
	}
	return last
}

func TestRestartKeepsTheVolumeOfTheSameChannel(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "volume-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	mgr := newTestManager(t, "play-control", Options{})

	start := func(login string) uint64 {
		t.Helper()
		gen, err := mgr.Start(testTarget(login))
		if err != nil {
			t.Fatalf("Start(%s): %v", login, err)
		}
		_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
		return gen
	}
	settled := func(gen uint64, want int) {
		t.Helper()
		waitVolume(t, mgr, gen, want, 3*time.Second)
		waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
		command := fmt.Sprintf(`["set_property","volume",%d]`, want)
		if got := lastVolumeSet(t, cmdLog); !strings.Contains(got, command) {
			t.Fatalf("last volume sent = %q, want %s", got, command)
		}
		if got := mgr.Volume(); got != want {
			t.Fatalf("Volume = %d, want %d", got, want)
		}
	}

	gen := start("gaules")
	settled(gen, 100)
	if err := mgr.AdjustVolume(-30); err != nil {
		t.Fatal(err)
	}
	waitVolume(t, mgr, gen, 70, 3*time.Second)

	settled(start("gaules"), 70)

	// A retry can come after a stop, with no session left to read the volume from.
	mgr.Stop()
	settled(start("gaules"), 70)

	// Another channel is the only thing that goes back to the baseline.
	settled(start("alanzoka"), 100)
	settled(start("gaules"), 100)
}

func TestDefaultSocketWaitCoversASlowNetwork(t *testing.T) {
	// streamlink took 15.6s to reach mpv behind a VPN at 380ms RTT.
	const measured = 15600 * time.Millisecond
	if got := New(Options{}).opts.SocketWait; got < measured*3/2 {
		t.Fatalf("SocketWait = %v, too close to the %v measured", got, measured)
	}
}

func TestVolumeZeroSurvivesTheRestart(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "mute-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("gaules"))
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if err := mgr.SetVolume(0); err != nil {
		t.Fatal(err)
	}
	waitVolume(t, mgr, gen, 0, 3*time.Second)
	gen, err = mgr.Start(testTarget("gaules"))
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","volume",0]`)
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	if got := lastVolumeSet(t, cmdLog); !strings.Contains(got, `["set_property","volume",0]`) {
		t.Fatalf("muted channel came back at %q", got)
	}
}

func waitVolume(t *testing.T, mgr *Manager, gen uint64, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if mgr.Volume() == want {
			return
		}
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && ev.Volume == want {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for volume %d (got %d)", want, mgr.Volume())
		}
	}
}

func TestCtrlXStopRemovesSocketKeepsManagerUsable(t *testing.T) {
	dir := t.TempDir()
	mgr := newTestManager(t, "play-control", Options{RuntimeDir: dir})
	gen, err := mgr.Start(testTarget("stopchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	socket := ""
	pid := 0
	if mgr.session != nil {
		socket = mgr.session.endpoint
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	if socket == "" {
		t.Fatal("expected socket")
	}

	mgr.Stop()
	_ = waitForState(t, mgr.Events(), gen, StateEnded, 5*time.Second)

	time.Sleep(100 * time.Millisecond)
	if !endpointGone(socket) {
		t.Fatalf("socket still present: %s", socket)
	}
	if pid > 0 {
		if pidExists(pid) {
			t.Fatalf("pid %d still alive", pid)
		}
	}

	// Manager remains usable for a new Start.
	gen2, err := mgr.Start(testTarget("again"))
	if err != nil {
		t.Fatalf("Start again: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen2, StatePlaying, 5*time.Second)
}

func TestHelperExitBecomesEnded(t *testing.T) {
	mgr := newTestManager(t, "exit-after-play", Options{})
	gen, err := mgr.Start(testTarget("bye"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	ev := waitForState(t, mgr.Events(), gen, StateEnded, 5*time.Second)
	if ev.State != StateEnded {
		t.Fatalf("state=%s", ev.State)
	}
}

func TestPausePropertyChangeFromMPVWindow(t *testing.T) {
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("extpause"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		t.Fatal("expected session")
	}

	// Simulate mpv window pause via the same observation path (property-change).
	sess.handleIPCMessage(ipcMessage{
		Event: "property-change",
		Name:  "pause",
		Data:  json.RawMessage("true"),
	})
	ev := waitForState(t, mgr.Events(), gen, StatePaused, 2*time.Second)
	if ev.State != StatePaused {
		t.Fatalf("state=%s", ev.State)
	}

	sess.handleIPCMessage(ipcMessage{
		Event: "property-change",
		Name:  "pause",
		Data:  json.RawMessage("false"),
	})
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 2*time.Second)
}

func TestDefaultCommandShape(t *testing.T) {
	cmd := testCommand(testTarget("gaules"), "/run/user/1000/ttv-tui/1/i", "/run/user/1000/ttv-tui/1/mpv-input.conf")
	if filepath.Base(cmd.Path) != "streamlink" && !strings.HasSuffix(cmd.Path, "streamlink") {
		// exec.Command may leave Path empty until LookPath; check Args[0].
		if cmd.Args[0] != "streamlink" {
			t.Fatalf("Args[0]=%q", cmd.Args[0])
		}
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"https://www.twitch.tv/gaules",
		"best",
		"--player",
		"mpv",
		"--input-ipc-server=/run/user/1000/ttv-tui/1/i",
		"--force-window=immediate",
		"--hwdec=auto-safe",
		"--no-config",
		"--no-input-default-bindings",
		"--input-conf=/run/user/1000/ttv-tui/1/mpv-input.conf",
		"--no-osc",
		"--osd-level=1",
		"--osd-bar=no",
		"--volume-max=100",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command args %q missing %q", joined, want)
		}
	}
}

func TestRealStreamlinkSmoke(t *testing.T) {
	if os.Getenv("TTV_REAL_PLAYER") != "1" {
		t.Skip("set TTV_REAL_PLAYER=1 to run real streamlink+mpv smoke")
	}
	if _, err := exec.LookPath("streamlink"); err != nil {
		t.Skip("streamlink not installed")
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}

	mgr := New(Options{
		RuntimeDir: privateRuntimeDir(t),
		SocketWait: 45 * time.Second,
		StopWait:   3 * time.Second,
	})
	defer mgr.Close()

	gen, err := mgr.Start(testTarget("gaules"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 5*time.Second)
	ev := waitForState(t, mgr.Events(), gen, StatePlaying, 60*time.Second)
	t.Logf("playing: %+v", ev)

	mgr.mu.Lock()
	var pid int
	var socket string
	if mgr.session != nil {
		pid = mgr.session.Pid()
		socket = mgr.session.endpoint
	}
	mgr.mu.Unlock()
	if pid > 0 {
		if !pidExists(pid) {
			t.Fatalf("process %d not alive while playing", pid)
		}
	}

	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePaused, 10*time.Second)
	t.Logf("paused ok")

	if err := mgr.AdjustVolume(-5); err != nil {
		t.Fatalf("AdjustVolume: %v", err)
	}
	waitVolume(t, mgr, gen, clampVolume(ev.Volume-5), 10*time.Second)
	t.Logf("volume now %d", mgr.Volume())

	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause resume: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 10*time.Second)

	mgr.Stop()
	_ = waitForState(t, mgr.Events(), gen, StateEnded, 10*time.Second)
	time.Sleep(500 * time.Millisecond)
	if pid > 0 {
		if pidExists(pid) {
			t.Fatalf("pid %d still alive after Stop", pid)
		}
	}
	if socket != "" {
		if !endpointGone(socket) {
			t.Fatalf("socket still present: %s", socket)
		}
	}
}

func TestLongRuntimeDirFallsBackAndCleansUp(t *testing.T) {
	skipWithoutSocketFile(t)
	longDir := filepath.Join(t.TempDir(), strings.Repeat("long-component/", 16))
	if len(longDir) < 200 {
		t.Fatalf("expected longDir >= 200 bytes, got %d", len(longDir))
	}
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", longDir)
	useTempDir(t, base)
	assertLongRuntimeDirFallsBackTo(t, longDir, base)
}

// Nothing the environment names fits, $TMPDIR included: the session still starts.
func TestLongRuntimeAndTempDirsFallBackToShortBase(t *testing.T) {
	skipWithoutSocketFile(t)
	longDir := filepath.Join(t.TempDir(), strings.Repeat("long-component/", 16))
	t.Setenv("XDG_RUNTIME_DIR", longDir)
	useTempDir(t, longDir)
	assertLongRuntimeDirFallsBackTo(t, longDir, shortFallbackBaseDir)
}

// assertLongRuntimeDirFallsBackTo plays a session whose runtime directory cannot
// hold the socket, and checks that it lands under base and leaves nothing behind.
func assertLongRuntimeDirFallsBackTo(t *testing.T, longDir, base string) {
	t.Helper()
	mgr := newTestManager(t, "play", Options{RuntimeDir: longDir})
	gen, err := mgr.Start(testTarget("longchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	var socket, sessionDir string
	if mgr.session != nil {
		socket = mgr.session.endpoint
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()

	if socket == "" || sessionDir == "" {
		t.Fatal("expected active session with socket and dir")
	}

	if len(socket) > maxSocketPathLen {
		t.Fatalf("socket path length %d > %d: %s", len(socket), maxSocketPathLen, socket)
	}

	if filepath.Dir(sessionDir) != base {
		t.Fatalf("expected sessionDir to fall back under %s, got %s", base, sessionDir)
	}

	if !endpointPresent(socket) {
		t.Fatalf("socket should exist: %s", socket)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if !endpointGone(socket) {
		t.Fatalf("socket still exists after Stop: %s", socket)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir still exists after Stop: %s", sessionDir)
	}
}

func TestLongRuntimeDirDefaultEnvFallsBack(t *testing.T) {
	skipWithoutSocketFile(t)
	longDir := filepath.Join(t.TempDir(), strings.Repeat("xdg-long/", 25))
	if len(longDir) < 200 {
		t.Fatalf("expected longDir >= 200 bytes, got %d", len(longDir))
	}
	t.Setenv("XDG_RUNTIME_DIR", longDir)

	opts := Options{
		Command:    helperCommand("play"),
		SocketWait: 5 * time.Second,
		StopWait:   500 * time.Millisecond,
	}
	mgr := New(opts)
	defer mgr.Close()

	gen, err := mgr.Start(testTarget("xdgchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	var socket, sessionDir string
	if mgr.session != nil {
		socket = mgr.session.endpoint
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()

	if len(socket) > maxSocketPathLen {
		t.Fatalf("socket length %d > %d", len(socket), maxSocketPathLen)
	}
	if !endpointPresent(socket) {
		t.Fatalf("socket should exist: %s", socket)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if !endpointGone(socket) {
		t.Fatalf("socket still exists after Stop: %s", socket)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir still exists after Stop: %s", sessionDir)
	}
}

func TestFallbackNotWritableReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chmod cannot make a Windows directory read-only")
	}
	roBase, err := os.MkdirTemp("/tmp", "ro-fallback-*")
	if err != nil {
		t.Fatalf("create roBase: %v", err)
	}
	if err := os.Chmod(roBase, 0o500); err != nil {
		t.Fatalf("chmod roBase: %v", err)
	}
	restore := setFallbackBaseDirForTest(roBase)
	t.Cleanup(func() {
		_ = os.Chmod(roBase, 0o700)
		restore()
	})

	longDir := filepath.Join(t.TempDir(), strings.Repeat("long/", 30))
	mgr := newTestManager(t, "play", Options{RuntimeDir: longDir})

	gen, err := mgr.Start(testTarget("errchan"))
	if err == nil {
		t.Fatal("expected Start to fail when fallback is not writable")
	}

	expectedPath := filepath.Join(roBase, fmt.Sprintf("ttv-tui-%d-", gen))
	errMsg := err.Error()

	// Verify that error states the attempted path
	if !strings.Contains(errMsg, expectedPath) {
		t.Fatalf("error %q does not contain attempted path %q", errMsg, expectedPath)
	}

	// Verify that error states the reason (permission/read-only)
	if !strings.Contains(errMsg, "permission") && !strings.Contains(errMsg, "not writable") && !strings.Contains(errMsg, "read-only") {
		t.Fatalf("error %q does not contain reason (permission/not writable)", errMsg)
	}

	// Verify manager emitted StateError with the same reason
	ev := waitForState(t, mgr.Events(), gen, StateError, 3*time.Second)
	if !strings.Contains(ev.LastLine, expectedPath) {
		t.Fatalf("LastLine %q missing attempted path %q", ev.LastLine, expectedPath)
	}
}

// helperSocketNoListener leaves a socket file behind with nobody listening on
// it, and a descendant in the same process group, then waits to be signalled.
func helperSocketNoListener(socket string) {
	ln, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	_ = ln.Close() // the file stays, the listener is gone: connect gets ECONNREFUSED

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER_MODE=idle-child")
	if err := cmd.Start(); err == nil {
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}

func TestManagerStartRejectsEmptyURL(t *testing.T) {
	mgr := New(Options{})
	t.Cleanup(mgr.Close)
	_, err := mgr.Start(Target{})
	if err == nil {
		t.Fatal("Start with empty Target.URL must fail")
	}
	if !strings.Contains(err.Error(), "empty stream URL") {
		t.Fatalf("err = %v, want 'empty stream URL'", err)
	}
}

func TestManagerStartRejectsURLsStreamlinkCouldReadAsOptions(t *testing.T) {
	var started bool
	mgr := New(Options{Command: func(Target, string) *exec.Cmd {
		started = true
		return exec.Command("true")
	}})
	t.Cleanup(mgr.Close)
	for _, raw := range []string{
		"--player=evil",
		"-o/tmp/x",
		"twitch.tv/gaules",
		"file:///etc/passwd",
		"https://",
		"https://www.twitch.tv/gaules --player=evil",
		"https://www.twitch.tv/gaules\n--player=evil",
		"https://www.twitch.tv/gau\x00les",
	} {
		if _, err := mgr.Start(Target{URL: raw, Label: "x"}); err == nil {
			t.Errorf("Start(%q) succeeded, want an error", raw)
		}
	}
	if started {
		t.Fatal("a rejected URL reached the command")
	}
	for _, raw := range []string{
		"https://kick.com/xqc",
		"https://www.youtube.com/@canal/live",
		"HTTPS://www.twitch.tv/a?b=c",
		"http://host:8080/x",
	} {
		if err := checkStreamURL(raw); err != nil {
			t.Errorf("checkStreamURL(%q) = %v, want nil", raw, err)
		}
	}
}

func TestManagerPreservesVolumeByTargetIdentity(t *testing.T) {
	mgr := newTestManager(t, "play-control", Options{})
	target := Target{
		URL:   "https://www.twitch.tv/volchan",
		Label: "volchan",
	}
	gen, err := mgr.Start(target)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	// Adjust volume to 40.
	if err := mgr.AdjustVolume(-60); err != nil {
		t.Fatalf("AdjustVolume: %v", err)
	}
	waitVolume(t, mgr, gen, 40, 3*time.Second)
	mgr.Stop()

	// Starting same identity preserves volume.
	sameTarget := Target{
		URL:   "https://www.twitch.tv/volchan",
		Label: "volchan",
	}
	if vol := mgr.startVolume(sameTarget); vol != 40 {
		t.Fatalf("startVolume same identity = %d, want 40", vol)
	}

	// Starting different identity uses baseline volume.
	otherTarget := Target{
		URL:   "https://www.twitch.tv/other",
		Label: "other",
	}
	if vol := mgr.startVolume(otherTarget); vol != baselineVolume {
		t.Fatalf("startVolume other identity = %d, want %d", vol, baselineVolume)
	}
}

func TestRegistryRecordStoresTargetAndIdentity(t *testing.T) {
	rec := Record{
		OwnerPID: 1234,
		PGID:     5678,
		Socket:   "/tmp/sock",
		Dir:      "/tmp/dir",
		URL:      "https://www.twitch.tv/gaules",
		Label:    "gaules",
		Login:    "gaules",
	}
	if rec.Identity() != "gaules" {
		t.Fatalf("Identity = %q, want gaules", rec.Identity())
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"login":"gaules"`, `"url":"https://www.twitch.tv/gaules"`, `"label":"gaules"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("record JSON %s lacks %s", data, want)
		}
	}

	// A record written before url and label existed still names its channel.
	var oldRec Record
	if err := json.Unmarshal([]byte(`{"owner_pid":1,"pgid":5678,"socket":"/tmp/sock","dir":"/tmp/dir","login":"oldchan","generation":3}`), &oldRec); err != nil {
		t.Fatalf("Unmarshal old record: %v", err)
	}
	if oldRec.Identity() != "oldchan" {
		t.Fatalf("old Identity = %q, want oldchan", oldRec.Identity())
	}
}

func TestCustomStreamlinkPathAndEnv(t *testing.T) {
	mgr := New(Options{
		StreamlinkPath: "/custom/path/to/streamlink",
		StreamlinkEnv:  []string{"CUSTOM_VAR=1"},
	})
	defer mgr.Close()

	if got := mgr.StreamlinkPath(); got != "/custom/path/to/streamlink" {
		t.Fatalf("StreamlinkPath = %q, want /custom/path/to/streamlink", got)
	}

	mgr.SetStreamlink("/updated/streamlink", "APPIMAGE_EXTRACT_AND_RUN=1")
	if got := mgr.StreamlinkPath(); got != "/updated/streamlink" {
		t.Fatalf("updated StreamlinkPath = %q, want /updated/streamlink", got)
	}

	// checkTools should report missing custom streamlink when file does not exist
	_, err := mgr.checkTools(mgr.StreamlinkPath())
	if err == nil {
		t.Fatal("expected checkTools to fail for non-existent streamlink path")
	}
	if !strings.Contains(err.Error(), "/updated/streamlink") {
		t.Fatalf("expected error mentioning /updated/streamlink, got %v", err)
	}
}

func TestCheckTools_WindowShimWithoutRealMpv(t *testing.T) {
	shimDir := t.TempDir()
	streamlinkDir := t.TempDir()

	// Create fake streamlink in streamlinkDir
	streamlinkName := "streamlink"
	if runtime.GOOS == "windows" {
		streamlinkName += ".exe"
	}
	if err := os.WriteFile(filepath.Join(streamlinkDir, streamlinkName), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create window shim in shimDir (named mpv or mpv.exe/mpv.com)
	for _, name := range []string{"mpv", "mpv.exe", "mpv.com"} {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte("shim"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Case 1: Window available — shimDir is on PATH (first).
	// checkTools succeeds without any real mpv installed.
	t.Run("window shim on PATH", func(t *testing.T) {
		t.Setenv("PATH", shimDir+string(os.PathListSeparator)+streamlinkDir)
		mgr := New(Options{})
		defer mgr.Close()

		found, err := mgr.checkTools("")
		if err != nil {
			t.Fatalf("checkTools with window shim on PATH failed: %v", err)
		}
		if got := found["mpv"]; !filepath.IsAbs(got) || !strings.EqualFold(filepath.Dir(got), shimDir) {
			t.Fatalf("mpv found at %q, want the shim in %q", got, shimDir)
		}
	})

	// Case 2: Terminal mode without mpv — PATH only has streamlink, no mpv or shim.
	// checkTools must report missing mpv, and FailureOf must be FailureMPVMissing.
	t.Run("terminal mode without mpv", func(t *testing.T) {
		t.Setenv("PATH", streamlinkDir)
		mgr := New(Options{})
		defer mgr.Close()

		_, err := mgr.checkTools("")
		if err == nil {
			t.Fatal("expected checkTools to fail when mpv is not on PATH")
		}
		var missing *MissingToolError
		if !errors.As(err, &missing) {
			t.Fatalf("expected MissingToolError, got: %T (%v)", err, err)
		}
		if FailureOf(err) != FailureMPVMissing {
			t.Fatalf("FailureOf(%v) = %q, want %q", err, FailureOf(err), FailureMPVMissing)
		}
	})
}
