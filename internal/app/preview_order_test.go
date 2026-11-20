package app

import (
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Rapid switch: StartPreview for A is in flight; B fails before StartInOrder
// (empty channel). Without AdvanceOrder on that failure, A would still attach
// and leave an orphan preview. With it, A is superseded and spawns nothing.
func TestStartPreviewFailureSupersedesInFlightStart(t *testing.T) {
	var spawns struct {
		mu     sync.Mutex
		logins []string
	}
	entered := make(chan struct{})
	release := make(chan struct{})

	preview := player.New(player.Options{
		RuntimeDir: t.TempDir(),
		StopWait:   100 * time.Millisecond,
		Command: func(target player.Target, _ string) *exec.Cmd {
			spawns.mu.Lock()
			spawns.logins = append(spawns.logins, target.Identity())
			spawns.mu.Unlock()
			return exec.Command(os.Args[0], "-test.run=^$")
		},
	})
	t.Cleanup(preview.Close)
	preview.SetAfterPrevStop(func() {
		close(entered)
		<-release
	})

	// A custom Command keeps the main manager off streamlink, so StartPreview
	// never resolves or downloads it from the real data dir (macOS/Windows CI).
	main := player.New(player.Options{
		RuntimeDir: t.TempDir(),
		Command: func(player.Target, string) *exec.Cmd {
			return exec.Command(os.Args[0], "-test.run=^$")
		},
	})
	t.Cleanup(main.Close)
	b := newPlaybackBridge(main)
	b.setPreview(preview)

	errCh := make(chan ui.PreviewStatusMsg, 1)
	go func() {
		msg := b.StartPreview(ui.PlayTarget{
			Channel: catalog.Channel{Platform: platform.Twitch, Login: "channel-a"},
		}, 1)()
		errCh <- msg.(ui.PreviewStatusMsg)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("StartPreview A did not enter StartInOrder")
	}

	fail := b.StartPreview(ui.PlayTarget{
		Channel: catalog.Channel{Platform: platform.Twitch},
	}, 2)().(ui.PreviewStatusMsg)
	if fail.State != ui.PreviewStateFailed || fail.Selection != 2 {
		t.Fatalf("B failure = %+v, want Failed selection 2", fail)
	}
	close(release)

	select {
	case a := <-errCh:
		if a.State == ui.PreviewStateStarting {
			t.Fatalf("A started after B failed: %+v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartPreview A did not return")
	}
	spawns.mu.Lock()
	got := slices.Clone(spawns.logins)
	spawns.mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("spawned %v after B failed before StartInOrder, want none", got)
	}
}
