package app

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/player"
)

// MuteMainForPreview must remember the original mute only once. A second call
// while the preview is already open (channel switch) must not overwrite the
// saved value; RestoreMainMute then clears the flag so a later preview can save
// again.
func TestMuteMainForPreviewSavesOriginalOnlyOnce(t *testing.T) {
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	b := newPlaybackBridge(mgr)

	if msg := b.MuteMainForPreview(); msg != nil {
		_ = msg()
	}
	if !b.hasMainMuteBefore {
		t.Fatal("first MuteMainForPreview did not save mute")
	}
	if b.mainMuteBeforePreview {
		t.Fatal("no session starts muted; saved value should be false")
	}

	if msg := b.MuteMainForPreview(); msg != nil {
		_ = msg()
	}
	if !b.hasMainMuteBefore || b.mainMuteBeforePreview {
		t.Fatalf("second MuteMainForPreview changed saved state: has=%v val=%v", b.hasMainMuteBefore, b.mainMuteBeforePreview)
	}

	if msg := b.RestoreMainMute(); msg != nil {
		_ = msg()
	}
	if b.hasMainMuteBefore {
		t.Fatal("RestoreMainMute left hasMainMuteBefore set")
	}

	if msg := b.MuteMainForPreview(); msg != nil {
		_ = msg()
	}
	if !b.hasMainMuteBefore {
		t.Fatal("MuteMainForPreview after restore did not save again")
	}
}

func TestRestoreMainMuteWithoutPriorSaveIsNoop(t *testing.T) {
	mgr := player.New(player.Options{})
	t.Cleanup(mgr.Close)
	b := newPlaybackBridge(mgr)
	if msg := b.RestoreMainMute(); msg != nil {
		if got := msg(); got != nil {
			t.Fatalf("unexpected msg %T %#v", got, got)
		}
	}
}
