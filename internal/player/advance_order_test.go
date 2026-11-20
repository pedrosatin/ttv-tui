package player

import (
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"
)

// AdvanceOrder while StartInOrder is past its order check must keep that Start
// from attaching: the selection that failed earlier owns the order now.
func TestAdvanceOrderSupersedesInFlightStart(t *testing.T) {
	var spawns struct {
		mu     sync.Mutex
		logins []string
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	mgr := New(Options{
		RuntimeDir: privateRuntimeDir(t),
		StopWait:   100 * time.Millisecond,
		Command: func(target Target, _ string) *exec.Cmd {
			spawns.mu.Lock()
			spawns.logins = append(spawns.logins, target.Identity())
			spawns.mu.Unlock()
			return exec.Command(os.Args[0], "-test.run=^$")
		},
	})
	t.Cleanup(mgr.Close)
	mgr.afterPrevStop = func() {
		close(entered)
		<-release
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := mgr.StartInOrder(testTarget("channel-a"), 1)
		errCh <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("StartInOrder did not reach afterPrevStop")
	}
	mgr.AdvanceOrder(2)
	close(release)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("in-flight StartInOrder succeeded after AdvanceOrder")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartInOrder did not return")
	}
	spawns.mu.Lock()
	got := slices.Clone(spawns.logins)
	spawns.mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("spawned %v after AdvanceOrder, want none", got)
	}
	if login := activeLogin(mgr); login != "" {
		t.Fatalf("session %q attached after AdvanceOrder", login)
	}
}
