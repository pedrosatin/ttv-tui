package app

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/player"
)

// Stop must apply a pending RestoreMainMute before tearing the session down.
// tea.Batch(closePreview, Stop) can run Stop first; without this, SetMute after
// Stop has no session and the preview mute leaks into keptMuted.
func TestBridgeStopAppliesPendingMuteRestore(t *testing.T) {
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	mgr.EnableTestMute(false)
	b := newPlaybackBridge(mgr)

	if msg := b.MuteMainForPreview(); msg != nil {
		_ = msg()
	}
	if !mgr.Muted() || !b.hasMainMuteBefore {
		t.Fatalf("muted=%v hasSave=%v", mgr.Muted(), b.hasMainMuteBefore)
	}

	// Do not call RestoreMainMute; Stop must restore.
	if msg := b.Stop(); msg != nil {
		_ = msg()
	}
	if b.hasMainMuteBefore {
		t.Fatal("Stop left hasMainMuteBefore set")
	}
	if mgr.Muted() {
		t.Fatal("Stop did not restore unmute on the manager")
	}
}

// StartTarget's leading restore shares applyPendingMainMuteRestore with Stop.
func TestApplyPendingMainMuteRestoreUnmutes(t *testing.T) {
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	mgr.EnableTestMute(true)
	b := newPlaybackBridge(mgr)

	b.mainMuteMu.Lock()
	b.mainMuteBeforePreview = false
	b.hasMainMuteBefore = true
	b.mainMuteMu.Unlock()

	b.applyPendingMainMuteRestore(mgr)
	if b.hasMainMuteBefore {
		t.Fatal("applyPendingMainMuteRestore left the flag set")
	}
	if mgr.Muted() {
		t.Fatal("applyPendingMainMuteRestore did not unmute")
	}
}
