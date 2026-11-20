package player

import (
	"path/filepath"
	"testing"
	"time"
)

// SetMute without a session must still record keptMuted so a later Start opens
// with that mute. Mute → Stop → Restore is the preview close race: Stop's
// keepVolume inherits the preview mute, then Restore has no session; without
// updating keptMuted the next channel starts muted.
func TestSetMuteWithoutSessionUpdatesKeptMuted(t *testing.T) {
	t.Parallel()
	cmdLog := filepath.Join(t.TempDir(), "kept-mute-cmds.jsonl")
	mgr := newTestManager(t, "play-control", Options{}, "TTV_PLAYER_HELPER_CMDS="+cmdLog)

	if err := mgr.SetMute(true); err == nil {
		t.Fatal("SetMute without session must still return an error")
	}
	gen, err := mgr.Start(testTarget("kept-on"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitMuted(t, mgr, gen, true, 3*time.Second)

	mgr.Stop()
	if err := mgr.SetMute(false); err == nil {
		t.Fatal("SetMute without session must still return an error")
	}
	gen, err = mgr.Start(testTarget("kept-off"))
	if err != nil {
		t.Fatalf("Start after restore: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	if mgr.Muted() {
		t.Fatal("Restore via SetMute(false) without session left the next start muted")
	}
}

// Mute on a live session, Stop (keepVolume inherits muted), then SetMute(false)
// with no session must clear keptMuted before the next Start.
func TestRestoreMuteAfterStopClearsInheritance(t *testing.T) {
	t.Parallel()
	cmdLog := filepath.Join(t.TempDir(), "restore-after-stop.jsonl")
	mgr := newTestManager(t, "play-control", Options{}, "TTV_PLAYER_HELPER_CMDS="+cmdLog)

	gen, err := mgr.Start(testTarget("before-stop"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	if err := mgr.SetMute(true); err != nil {
		t.Fatalf("SetMute(true): %v", err)
	}
	waitMuted(t, mgr, gen, true, 3*time.Second)

	mgr.Stop()
	// Preview restore after Stop: no session left.
	if err := mgr.SetMute(false); err == nil {
		t.Fatal("expected error without session")
	}

	gen, err = mgr.Start(testTarget("after-restore"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	waitCommandsSettled(t, mgr, StatePlaying, 5*time.Second)
	if mgr.Muted() {
		t.Fatal("next session started muted after Mute→Stop→SetMute(false)")
	}
}
