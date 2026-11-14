package player

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitMuted waits for the session's mute to reach want, from an event or the
// manager.
func waitMuted(t *testing.T, mgr *Manager, gen uint64, want bool, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if mgr.Muted() == want {
			return
		}
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen && ev.Muted == want {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for muted=%v", want)
		}
	}
}

// muteCommands lists the mute values sent to mpv, in order.
func muteCommands(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		switch {
		case strings.Contains(line, `["set_property","mute",true]`):
			out = append(out, "true")
		case strings.Contains(line, `["set_property","mute",false]`):
			out = append(out, "false")
		}
	}
	return out
}

// m mutes mpv over IPC and m again brings the sound back; the events carry it.
func TestToggleMuteReachesMpvAndEvents(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "mute-cmds.jsonl")
	t.Setenv("TTV_PLAYER_HELPER_CMDS", cmdLog)
	mgr := newTestManager(t, "play-control", Options{})
	gen, err := mgr.Start(testTarget("mutechan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if mgr.Muted() {
		t.Fatal("a session starts muted with nothing asking for it")
	}

	if err := mgr.ToggleMute(); err != nil {
		t.Fatalf("ToggleMute: %v", err)
	}
	waitMuted(t, mgr, gen, true, 3*time.Second)
	if err := mgr.ToggleMute(); err != nil {
		t.Fatalf("ToggleMute: %v", err)
	}
	waitMuted(t, mgr, gen, false, 3*time.Second)
	waitCommands(t, cmdLog, 3*time.Second, `["set_property","mute",true]`, `["set_property","mute",false]`)
	if got := mgr.Volume(); got != 100 {
		t.Errorf("mute changed the volume to %d", got)
	}
}

// The mute follows the viewer to the next channel; with the sound on, the new
// session leaves mpv's mute alone.
func TestMuteFollowsToTheNextChannel(t *testing.T) {
	cmdLog := filepath.Join(t.TempDir(), "mute-cmds.jsonl")
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

	gen := start("gaules")
	if err := mgr.ToggleMute(); err != nil {
		t.Fatal(err)
	}
	waitMuted(t, mgr, gen, true, 3*time.Second)

	gen = start("alanzoka")
	waitMuted(t, mgr, gen, true, 3*time.Second)
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	if got := muteCommands(t, cmdLog); len(got) != 2 || got[1] != "true" {
		t.Fatalf("mute commands = %v, want the toggle and the new session's baseline", got)
	}

	if err := mgr.ToggleMute(); err != nil {
		t.Fatal(err)
	}
	waitMuted(t, mgr, gen, false, 3*time.Second)
	start("gaules")
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	if mgr.Muted() {
		t.Fatal("unmuted before the switch, muted after it")
	}
	if got := muteCommands(t, cmdLog); len(got) != 3 {
		t.Fatalf("mute commands = %v: the unmuted session must not set mute", got)
	}
}

// Without a session there is nothing to mute.
func TestToggleMuteWithoutSession(t *testing.T) {
	mgr := New(Options{})
	if err := mgr.ToggleMute(); err == nil {
		t.Fatal("ToggleMute without a session succeeded")
	}
	if mgr.Muted() {
		t.Fatal("Muted without a session")
	}
}

// The mute OSD speaks no language, like the pause and volume ones.
func TestInputConfMuteOSDHasNoWords(t *testing.T) {
	for _, line := range strings.Split(strings.TrimSpace(inputConfContent()), "\n") {
		if !strings.Contains(line, "mute") {
			continue
		}
		if !strings.HasPrefix(line, "m no-osd cycle mute; ") || !strings.Contains(line, muteText) {
			t.Errorf("mute line %q", line)
		}
		for _, word := range []string{"Mute", "mudo", "muted", "yes\"", "no\""} {
			if strings.Contains(strings.ReplaceAll(line, "${?mute==", ""), word) {
				t.Errorf("mute line %q shows %q", line, word)
			}
		}
	}
}
