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
	gen, err := mgr.Start("gaules")
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
	if _, err := mgr.Start("gaules"); err != nil {
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
	gen, err := mgr.Start("real")
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
