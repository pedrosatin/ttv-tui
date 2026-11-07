package player

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionRoutesFramesAndLinesApart(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)

	mgr := newTestManager(t, "sixel-play", Options{})
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("gaules")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFrames(t, rec, 3)
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	// The line sits between frames on stdout and still drives the interval.
	waitForState(t, mgr.Events(), gen, StateBreak, 5*time.Second)

	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	waitCommands(t, cmdLog, 5*time.Second,
		`["set_property","options/vo-sixel-width",240]`,
		`["set_property","options/vo-sixel-height",132]`,
		`["set_property","vid","no"]`,
		`["set_property","vid","auto"]`,
	)
	mgr.Stop()
	_ = out.Close()

	text, frames := splitEmissions(t, rec.bytes(), 1, 1)
	if len(text) != 0 {
		t.Fatalf("the player reached the terminal outside a frame: %q", text)
	}
	for _, f := range frames {
		if s := string(f); s != testFrameA && s != testFrameB {
			t.Fatalf("frame changed on its way out: %q", f)
		}
	}
}

func TestSessionAppliesAPanelResizeThatBeatItsIPC(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	out := newTestSixel(t, &terminalRecorder{})

	mgr := newTestManager(t, "sixel-play", Options{})
	mgr.SetVideoOutput(out)
	if _, err := mgr.StartEmbedded("gaules"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The helper takes 250ms to listen: the panel changes while it starts.
	panel := testPanel
	panel.Rows = 5
	out.SetPanel(panel)
	width, height := out.FrameSize()
	if width == sixelMaxWidth {
		t.Fatal("panel did not change the frame size")
	}
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","vid","auto"]`)
	raw, err := os.ReadFile(cmdLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"options/vo-sixel-height",96`) {
		t.Fatalf("size %dx%d not applied: %s", width, height, raw)
	}
}

// countingLog reads the helper's command log.
func countingLog(t *testing.T, path, command string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(raw), command)
}

func TestWindowSessionNeverSeesTheOutput(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)

	// The helper writes frames either way: they must stay on the session's stdout.
	mgr := newTestManager(t, "sixel-play", Options{})
	mgr.SetVideoOutput(out)
	gen, err := mgr.Start("gaules")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	if err := mgr.SetVideoTrack(false); err != nil {
		t.Fatalf("SetVideoTrack: %v", err)
	}
	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	// Commands go out in order, so this one arriving means the others had their turn.
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","pause",true]`)
	raw, err := os.ReadFile(cmdLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"vid"`) || strings.Contains(string(raw), "vo-sixel") {
		t.Fatalf("the window got panel commands:\n%s", raw)
	}
	if n := rec.frames(); n != 0 {
		t.Fatalf("%d frames from a window session", n)
	}
}

func TestStartEmbeddedWithoutAnOutputUsesTheWindow(t *testing.T) {
	mgr := newTestManager(t, "sixel-play", Options{})
	gen, err := mgr.StartEmbedded("gaules")
	if err != nil {
		t.Fatalf("StartEmbedded: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	video := mgr.session.cfg.Video
	mgr.mu.Unlock()
	if video != nil {
		t.Fatal("session has an output nobody gave it")
	}
}

func TestHiddenPanelTurnsVideoOffAndAudioGoesOn(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	rec := &terminalRecorder{}
	mgr := newTestManager(t, "sixel-play", Options{})
	out := NewSixelOutput(rec, SixelOptions{
		Cell:  func() (int, int) { return 10, 20 },
		Track: func(on bool) { _ = mgr.SetVideoTrack(on) },
	})
	t.Cleanup(func() { _ = out.Close() })
	mgr.SetVideoOutput(out)
	out.SetPanel(testPanel)
	gen, err := mgr.StartEmbedded("gaules")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitFrames(t, rec, 3)

	out.SetPanel(SixelPanel{})
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","vid","no"]`)
	// The helper ignores vid=no, like a frame still in the pipe: none may get out.
	drawn := rec.frames()
	time.Sleep(2 * sixelPlayerDelay)
	if n := rec.frames(); n != drawn {
		t.Fatalf("%d frames with the panel hidden", n-drawn)
	}
	if state, _ := sessionState(mgr); state != StatePlaying && state != StateBreak {
		t.Fatalf("session left playback: %s", state)
	}
	if got := mgr.Generation(); got != gen {
		t.Fatalf("generation = %d, want %d: the player restarted", got, gen)
	}

	out.SetPanel(testPanel)
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","vid","auto"]`)
	waitFrames(t, rec, drawn+1)
}

func TestResizeLeavesAHiddenTrackOff(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	out := newTestSixel(t, &terminalRecorder{})
	out.SetPanel(testPanel)
	mgr := newTestManager(t, "sixel-play", Options{})
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("gaules")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if err := mgr.SetVideoTrack(false); err != nil {
		t.Fatalf("SetVideoTrack: %v", err)
	}
	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","options/vo-sixel-height",132]`)
	if n := countingLog(t, cmdLog, `"vid","auto"`); n != 0 {
		t.Fatal("a resize brought video back to a hidden panel")
	}
	if err := mgr.SetVideoTrack(true); err != nil {
		t.Fatalf("SetVideoTrack: %v", err)
	}
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","vid","auto"]`)
}

func TestSessionStartedBehindAHiddenPanelKeepsVideoOff(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "sixel-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	out := newTestSixel(t, &terminalRecorder{})
	mgr := newTestManager(t, "sixel-play", Options{})
	mgr.SetVideoOutput(out)
	if _, err := mgr.StartEmbedded("gaules"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCommands(t, cmdLog, 5*time.Second, `["set_property","vid","no"]`)
}

// Against the real mpv: frames cross the real session and pipe, and the resize
// the session sends really changes their size.
func TestRealMPVDrawsThroughTheOutput(t *testing.T) {
	if os.Getenv(realMPVEnv) != "1" {
		t.Skipf("set %s=1 to run against a real mpv", realMPVEnv)
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Fatalf("%s is set but mpv is not on PATH: %v", realMPVEnv, err)
	}
	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)
	mgr := New(Options{
		RuntimeDir: t.TempDir(),
		StopWait:   500 * time.Millisecond,
		Command: func(_, endpoint string) *exec.Cmd {
			args := append(sixelMPVArgs(endpoint, "", 480, 270), "--audio=no", "av://lavfi:testsrc=size=640x360:rate=30")
			return exec.Command("mpv", args...)
		},
	})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("real")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 15*time.Second)
	waitFrames(t, rec, 5)

	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, frames := splitEmissions(t, bytes.Clone(rec.bytes()), 1, 1)
		last := frames[len(frames)-1]
		width, height, ok := sixelRaster(last)
		if ok && width <= 240 && height <= 132 {
			t.Logf("%d frames, last %dx%d", len(frames), width, height)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("frames still %dx%d after the resize", width, height)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mgr.Stop()
	_ = out.Close()
	text, _ := splitEmissions(t, rec.bytes(), 1, 1)
	if len(text) != 0 {
		t.Fatalf("mpv reached the terminal outside a frame: %q", text)
	}
}

// Against the real mpv: vid=no stops the frames with the process and the audio
// alive, a resize meanwhile stays off, and vid=auto brings frames of the new size.
func TestRealMPVTrackOffKeepsAudio(t *testing.T) {
	if os.Getenv(realMPVEnv) != "1" {
		t.Skipf("set %s=1 to run against a real mpv", realMPVEnv)
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Fatalf("%s is set but mpv is not on PATH: %v", realMPVEnv, err)
	}
	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)
	mgr := New(Options{
		RuntimeDir: t.TempDir(),
		StopWait:   500 * time.Millisecond,
		Command: func(_, endpoint string) *exec.Cmd {
			args := append(sixelMPVArgs(endpoint, "", 480, 270), "--ao=null",
				"--audio-file=av://lavfi:sine=frequency=440", "av://lavfi:testsrc=size=640x360:rate=30")
			return exec.Command("mpv", args...)
		},
	})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("real")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 15*time.Second)
	waitFrames(t, rec, 5)

	if err := mgr.SetVideoTrack(false); err != nil {
		t.Fatalf("SetVideoTrack: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	drawn := rec.frames()
	time.Sleep(time.Second)
	if n := rec.frames(); n != drawn {
		t.Fatalf("%d frames with the track off", n-drawn)
	}
	if state, _ := sessionState(mgr); state != StatePlaying {
		t.Fatalf("mpv left playback with the track off: %s", state)
	}

	if err := mgr.SetVideoTrack(true); err != nil {
		t.Fatalf("SetVideoTrack: %v", err)
	}
	waitFrames(t, rec, drawn+3)
	_, frames := splitEmissions(t, bytes.Clone(rec.bytes()), 1, 1)
	width, height, ok := sixelRaster(frames[len(frames)-1])
	if !ok || width > 240 || height > 132 {
		t.Fatalf("frames back at %dx%d", width, height)
	}
	t.Logf("%d frames before the track went off, back at %dx%d", drawn, width, height)
	mgr.Stop()
	_ = out.Close()
}

// Against the real mpv: a paused player answers a rebuild with one frame of the new
// size. It is the only picture a panel that shrank during a pause will get, and it
// has to cross the output.
func TestRealMPVPausedRebuildDrawsOnce(t *testing.T) {
	if os.Getenv(realMPVEnv) != "1" {
		t.Skipf("set %s=1 to run against a real mpv", realMPVEnv)
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Fatalf("%s is set but mpv is not on PATH: %v", realMPVEnv, err)
	}
	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(testPanel)
	mgr := New(Options{
		RuntimeDir: t.TempDir(),
		StopWait:   500 * time.Millisecond,
		Command: func(_, endpoint string) *exec.Cmd {
			args := append(sixelMPVArgs(endpoint, "", 480, 270), "--audio=no", "av://lavfi:testsrc=size=640x360:rate=30")
			return exec.Command("mpv", args...)
		},
	})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("real")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePlaying, 15*time.Second)
	waitFrames(t, rec, 5)

	if err := mgr.TogglePause(); err != nil {
		t.Fatalf("TogglePause: %v", err)
	}
	waitForState(t, mgr.Events(), gen, StatePaused, 5*time.Second)
	paused := testPanel
	paused.Playing, paused.Still = false, true
	out.SetPanel(paused)
	time.Sleep(300 * time.Millisecond)
	drawn := rec.frames()

	start := time.Now()
	if err := mgr.ResizeVideo(240, 132); err != nil {
		t.Fatalf("ResizeVideo: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, frames := splitEmissions(t, bytes.Clone(rec.bytes()), 1, 1)
		if len(frames) > drawn {
			width, height, ok := sixelRaster(frames[len(frames)-1])
			if ok && width <= 240 && height <= 132 {
				t.Logf("frame of %dx%d %s after the resize", width, height, time.Since(start).Round(time.Millisecond))
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no frame of the new size while paused: %d frames, %d before", len(frames), drawn)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mgr.Stop()
	_ = out.Close()
}
