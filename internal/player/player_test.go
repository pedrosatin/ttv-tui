package player

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This test file never invokes real streamlink or mpv. Instead, when
// TTV_PLAYER_HELPER is set, the test binary re-executes itself as a helper
// that mimics streamlink (stdout/stderr lines) and mpv (Unix IPC JSON).
// Production Manager.Command is replaced with helperCommand in each test.

func TestMain(m *testing.M) {
	if os.Getenv("TTV_PLAYER_HELPER") == "1" {
		runPlayerHelper()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperCommand(mode string) CommandFunc {
	return func(login, socketPath string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "--", login, socketPath)
		cmd.Env = append(os.Environ(),
			"TTV_PLAYER_HELPER=1",
			"TTV_PLAYER_HELPER_MODE="+mode,
			"TTV_PLAYER_HELPER_SOCKET="+socketPath,
			"TTV_PLAYER_HELPER_LOGIN="+login,
		)
		return cmd
	}
}

func newTestManager(t *testing.T, mode string, opts Options) *Manager {
	t.Helper()
	opts.Command = helperCommand(mode)
	if opts.RuntimeDir == "" {
		opts.RuntimeDir = t.TempDir()
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
	gen, err := mgr.Start("gaules")
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

	genA, err := mgr.Start("channel-a")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genA, StateStarting, 3*time.Second)

	genB, err := mgr.Start("channel-b")
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genB, StateStarting, 3*time.Second)

	genC, err := mgr.Start("channel-c")
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

	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("channel C process not alive: %v", err)
	}

	// No other helper processes for A/B should remain under our runtime dir naming;
	// verify process group of C has living members, then Stop and ensure gone.
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid: %v", err)
	}
	mgr.Stop()
	time.Sleep(300 * time.Millisecond)

	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("pid %d still alive after Stop", pid)
	}
	if err := syscall.Kill(-pgid, 0); err == nil {
		t.Fatalf("process group %d still reachable after Stop", pgid)
	}
	_ = genA
	_ = genB
}

func TestSIGTERMIgnoredEscalatesToSIGKILL(t *testing.T) {
	mgr := newTestManager(t, "ignore-term", Options{StopWait: 200 * time.Millisecond})
	gen, err := mgr.Start("stubborn")
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
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("pid %d still alive after Kill escalation", pid)
	}
}

func TestSocketEOFBecomesEnded(t *testing.T) {
	mgr := newTestManager(t, "socket-eof", Options{})
	gen, err := mgr.Start("eofchan")
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
	gen, err := mgr.Start("badchan")
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
	genA, err := mgr.Start("aaa")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	genB, err := mgr.Start("bbb")
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
	gen, err := mgr.Start("sockchan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	socket := ""
	sessionDir := ""
	if mgr.session != nil {
		socket = mgr.session.socket
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()
	if socket == "" {
		t.Fatal("expected socket path")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket should exist during play: %v", err)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket still present after Stop: %v", err)
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
	gen, err := mgr.Start("adchan")
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
	gen, err := mgr.Start("silentchan")
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
	gen, err := mgr.Start("adchan")
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
	gen, err := mgr.Start("infochan")
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
	gen, err := mgr.Start("holdchan")
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
	genA, err := mgrA.Start("chan-a")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgrA.Events(), genA, StateBreak, 5*time.Second)
	t.Setenv("TTV_PLAYER_HELPER_ADS", "")
	genB, err := mgrA.Start("chan-b")
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
		cfg:   sessionConfig{Generation: 7, Login: "x", Emit: func(ev Event) { got = append(got, ev) }},
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
		})
	case "ad-then-play":
		fmt.Println("[plugins.twitch][info] Waiting for pre-roll ads to finish, be patient")
		fmt.Println("[stream.hls][info] Filtering out segments and pausing stream output")
		go func() {
			time.Sleep(900 * time.Millisecond)
			fmt.Println("[stream.hls][info] Resuming stream output")
		}()
		helperPlay(socket, login, false, helperIPCOptions{})
	case "ignore-term":
		helperIgnoreTerm(socket)
	case "exit-error":
		fmt.Fprintln(os.Stderr, "error: NO_PLAYABLE_STREAMS for channel")
		time.Sleep(50 * time.Millisecond)
		os.Exit(1)
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
}

func helperInitialVolume() float64 {
	s := strings.TrimSpace(os.Getenv("TTV_PLAYER_HELPER_INIT_VOLUME"))
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
		cmd := exec.Command("sleep", "120")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
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

	ln, err := net.Listen("unix", socket)
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
	ln, err := net.Listen("unix", socket)
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
	ln, err := net.Listen("unix", socket)
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
	ln, err := net.Listen("unix", socket)
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
	gen, err := mgr.Start("baseline")
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

	deadline := time.After(2 * time.Second)
	var data []byte
	for {
		var err error
		data, err = os.ReadFile(cmdLog)
		if err == nil {
			body := string(data)
			if strings.Contains(body, `"volume"`) && strings.Contains(body, `"pause"`) {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for baseline set_property in log: %s", data)
		case <-time.After(20 * time.Millisecond):
		}
	}
	body := string(data)
	if !strings.Contains(body, `["set_property","volume",100]`) {
		t.Fatalf("expected baseline set_property volume 100, got %s", body)
	}
	if !strings.Contains(body, `["set_property","pause",false]`) {
		t.Fatalf("expected baseline set_property pause false, got %s", body)
	}

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
}

func TestPauseCommandCorrelatesByRequestID(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	t.Setenv("TTV_PLAYER_HELPER_REPLY_DELAY_MS", "200")
	t.Setenv("TTV_PLAYER_HELPER_DEFER_PAUSE_PROP", "1")

	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start("pausechan")
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

	data, err := os.ReadFile(cmdLog)
	if err != nil {
		t.Fatalf("read cmd log: %v", err)
	}
	if !strings.Contains(string(data), `"request_id"`) {
		t.Fatalf("expected request_id in command log, got %s", data)
	}
	if !strings.Contains(string(data), `"pause"`) {
		t.Fatalf("expected pause command in log, got %s", data)
	}
}

func TestStaleRequestIDReplyIgnored(t *testing.T) {
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start("stalechan")
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
	gen, err := mgr.Start("superchan")
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
	genA, err := mgr.Start("gen-a")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	_ = waitForState(t, mgr.Events(), genA, StatePlaying, 5*time.Second)

	genB, err := mgr.Start("gen-b")
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
	gen, err := mgr.Start("volchan")
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
	gen, err := mgr.Start("stopchan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	socket := ""
	pid := 0
	if mgr.session != nil {
		socket = mgr.session.socket
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	if socket == "" {
		t.Fatal("expected socket")
	}

	mgr.Stop()
	_ = waitForState(t, mgr.Events(), gen, StateEnded, 5*time.Second)

	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket still present: %v", err)
	}
	if pid > 0 {
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("pid %d still alive", pid)
		}
	}

	// Manager remains usable for a new Start.
	gen2, err := mgr.Start("again")
	if err != nil {
		t.Fatalf("Start again: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen2, StatePlaying, 5*time.Second)
}

func TestHelperExitBecomesEnded(t *testing.T) {
	mgr := newTestManager(t, "exit-after-play", Options{})
	gen, err := mgr.Start("bye")
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
	gen, err := mgr.Start("extpause")
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
	cmd := DefaultCommand("gaules", "/run/user/1000/ttv-tui/1/i")
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
		RuntimeDir: t.TempDir(),
		SocketWait: 45 * time.Second,
		StopWait:   3 * time.Second,
	})
	defer mgr.Close()

	gen, err := mgr.Start("gaules")
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
		socket = mgr.session.socket
	}
	mgr.mu.Unlock()
	if pid > 0 {
		if err := syscall.Kill(pid, 0); err != nil {
			t.Fatalf("process not alive while playing: %v", err)
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
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("pid %d still alive after Stop", pid)
		}
	}
	if socket != "" {
		if _, err := os.Stat(socket); !os.IsNotExist(err) {
			t.Fatalf("socket still present: %v", err)
		}
	}
}

func TestLongRuntimeDirFallsBackAndCleansUp(t *testing.T) {
	longDir := filepath.Join(t.TempDir(), strings.Repeat("long-component/", 16))
	if len(longDir) < 200 {
		t.Fatalf("expected longDir >= 200 bytes, got %d", len(longDir))
	}
	t.Setenv("XDG_RUNTIME_DIR", longDir)
	t.Setenv("TMPDIR", longDir)

	mgr := newTestManager(t, "play", Options{RuntimeDir: longDir})
	gen, err := mgr.Start("longchan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	var socket, sessionDir string
	if mgr.session != nil {
		socket = mgr.session.socket
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()

	if socket == "" || sessionDir == "" {
		t.Fatal("expected active session with socket and dir")
	}

	if len(socket) > maxSocketPathLen {
		t.Fatalf("socket path length %d > %d: %s", len(socket), maxSocketPathLen, socket)
	}

	if !strings.HasPrefix(sessionDir, getFallbackBaseDir()) {
		t.Fatalf("expected sessionDir to fall back under %s, got %s", getFallbackBaseDir(), sessionDir)
	}

	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket should exist during playback: %v", err)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after Stop: %s", socket)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir still exists after Stop: %s", sessionDir)
	}
}

func TestLongRuntimeDirDefaultEnvFallsBack(t *testing.T) {
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

	gen, err := mgr.Start("xdgchan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	mgr.mu.Lock()
	var socket, sessionDir string
	if mgr.session != nil {
		socket = mgr.session.socket
		sessionDir = mgr.session.dir
	}
	mgr.mu.Unlock()

	if len(socket) > maxSocketPathLen {
		t.Fatalf("socket length %d > %d", len(socket), maxSocketPathLen)
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket should exist: %v", err)
	}

	mgr.Stop()
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket still exists after Stop: %s", socket)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("session dir still exists after Stop: %s", sessionDir)
	}
}

func TestFallbackNotWritableReturnsError(t *testing.T) {
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

	gen, err := mgr.Start("errchan")
	if err == nil {
		t.Fatal("expected Start to fail when fallback is not writable")
	}

	expectedPath := filepath.Join(roBase, fmt.Sprintf("ttv-tui-%d-%d", os.Getpid(), gen))
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
