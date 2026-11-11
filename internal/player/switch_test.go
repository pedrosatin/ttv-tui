package player

import (
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"
)

// spawnLog wraps a CommandFunc. At each spawn it notes every group spawned
// earlier that is still alive: one channel at a time means none.
type spawnLog struct {
	inner CommandFunc

	mu      sync.Mutex
	logins  []string
	cmds    []*exec.Cmd
	overlap []string
}

func (s *spawnLog) command(login, endpoint string) *exec.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.cmds {
		if c.Process != nil && groupExists(c.Process.Pid) {
			s.overlap = append(s.overlap, login+" spawned while "+s.logins[i]+" was alive")
		}
	}
	cmd := s.inner(login, endpoint)
	s.logins = append(s.logins, login)
	s.cmds = append(s.cmds, cmd)
	return cmd
}

func (s *spawnLog) spawned() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logins)
}

func (s *spawnLog) overlaps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.overlap)
}

func newSpawnManager(t *testing.T, spawns *spawnLog, stopWait time.Duration) *Manager {
	t.Helper()
	mgr := New(Options{
		Command:    spawns.command,
		RuntimeDir: t.TempDir(),
		SocketWait: 5 * time.Second,
		StopWait:   stopWait,
	})
	t.Cleanup(mgr.Close)
	return mgr
}

func activeLogin(mgr *Manager) string {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if mgr.session == nil {
		return ""
	}
	return mgr.session.cfg.Login
}

// Ctrl+X while a switch is taking the old channel down: the switch must not
// bring the new channel up afterwards.
func TestStopDuringStartDropsNewSession(t *testing.T) {
	spawns := &spawnLog{inner: helperCommand("play")}
	mgr := newSpawnManager(t, spawns, 500*time.Millisecond)
	if _, err := mgr.Start("channel-a"); err != nil {
		t.Fatalf("Start A: %v", err)
	}

	mgr.afterPrevStop = mgr.Stop
	gen, err := mgr.Start("channel-b")
	if err == nil {
		t.Fatalf("Start B returned gen %d without error after the Stop", gen)
	}
	if login := activeLogin(mgr); login != "" {
		t.Fatalf("session %q attached after the Stop", login)
	}
	if got := spawns.spawned(); !slices.Equal(got, []string{"channel-a"}) {
		t.Fatalf("spawned %v, want only channel-a", got)
	}
}

// Ctrl+Q or SIGHUP during a switch: once Close returns, the process may exit,
// so no player of the new channel may exist or come up later.
func TestCloseDuringStartLeavesNoPlayer(t *testing.T) {
	spawns := &spawnLog{inner: helperCommand("play")}
	mgr := newSpawnManager(t, spawns, 500*time.Millisecond)
	if _, err := mgr.Start("channel-a"); err != nil {
		t.Fatalf("Start A: %v", err)
	}

	mgr.afterPrevStop = mgr.Close
	if gen, err := mgr.Start("channel-b"); err == nil {
		t.Fatalf("Start B returned gen %d without error after Close", gen)
	}
	if got := spawns.spawned(); !slices.Equal(got, []string{"channel-a"}) {
		t.Fatalf("spawned %v after Close, want only channel-a", got)
	}
}

// Start C arriving while Start B is still on its way up: B must not spawn next
// to C.
func TestStartsInARowNeverRunTwoGroups(t *testing.T) {
	spawns := &spawnLog{inner: helperCommand("play")}
	mgr := newSpawnManager(t, spawns, 500*time.Millisecond)
	if _, err := mgr.Start("channel-a"); err != nil {
		t.Fatalf("Start A: %v", err)
	}

	fired := false
	var errC error
	mgr.afterPrevStop = func() {
		if fired {
			return
		}
		fired = true
		_, errC = mgr.Start("channel-c")
	}
	_, errB := mgr.Start("channel-b")
	if errC != nil {
		t.Fatalf("Start C: %v", errC)
	}
	if errB == nil {
		t.Fatal("Start B attached although Start C came after it")
	}
	if o := spawns.overlaps(); len(o) > 0 {
		t.Fatalf("two players at once: %v", o)
	}
	if login := activeLogin(mgr); login != "channel-c" {
		t.Fatalf("active session %q, want channel-c", login)
	}
}

// Two Starts racing while the old channel ignores SIGTERM: the one that does
// not tear it down must wait for it to be gone before spawning.
func TestStartWaitsForTeardownByAnotherStart(t *testing.T) {
	spawns := &spawnLog{inner: helperCommand("ignore-term")}
	mgr := newSpawnManager(t, spawns, 300*time.Millisecond)
	genA, err := mgr.Start("channel-a")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	// Playing means the helper already ignores SIGTERM.
	_ = waitForState(t, mgr.Events(), genA, StatePlaying, 5*time.Second)

	done := make(chan struct{}, 2)
	for _, login := range []string{"channel-b", "channel-c"} {
		go func() {
			_, _ = mgr.Start(login)
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	if o := spawns.overlaps(); len(o) > 0 {
		t.Fatalf("two players at once: %v", o)
	}
	if activeLogin(mgr) == "" {
		t.Fatal("no session left after the two Starts")
	}
}

// A Stop for a generation a newer Start replaced leaves that Start's session up;
// a Stop for the current generation still stops it.
func TestStopGenerationIgnoresReplacedGeneration(t *testing.T) {
	spawns := &spawnLog{inner: helperCommand("play")}
	mgr := newSpawnManager(t, spawns, 500*time.Millisecond)
	genA, err := mgr.Start("channel-a")
	if err != nil {
		t.Fatalf("Start A: %v", err)
	}
	genB, err := mgr.Start("channel-b")
	if err != nil {
		t.Fatalf("Start B: %v", err)
	}

	if err := mgr.StopGeneration(genA); err != nil {
		t.Fatal(err)
	}
	if login := activeLogin(mgr); login != "channel-b" {
		t.Fatalf("after the Stop for A the session is %q, want channel-b", login)
	}
	if err := mgr.StopGeneration(genB); err != nil {
		t.Fatal(err)
	}
	if login := activeLogin(mgr); login != "" {
		t.Fatalf("session %q left after the Stop for B", login)
	}
}
