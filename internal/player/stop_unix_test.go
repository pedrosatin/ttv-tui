//go:build unix

package player

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A session that ended on its own keeps its pgid until the next channel switch
// or Ctrl+Q. By then the number may lead another program's group: Stop must not
// signal it.
func TestStopOfEndedSessionSignalsNoGroup(t *testing.T) {
	mgr := newTestManager(t, "exit-after-play", Options{StopWait: 200 * time.Millisecond})
	gen, err := mgr.Start(testTarget("ended"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateEnded, 5*time.Second)

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if sess == nil {
		t.Fatal("ended session no longer held by the manager")
	}
	// Ended can come from the IPC closing, before the leader is reaped.
	<-sess.waitDone

	// Stand-in for a recycled pgid: another program's group under the same id.
	stranger := startIdleGroup(t, "")
	sess.group = recordedGroup(stranger.Process.Pid)

	mgr.Stop()
	if !processAlive(stranger.Process.Pid) {
		t.Fatalf("Stop of an ended session signalled group %d", stranger.Process.Pid)
	}
}

// A member that SIGKILL cannot remove (stuck in D state) keeps the group alive.
// Stop must report that as an error, not as a clean stop. A zombie whose parent,
// this test, does not reap it stands in for that member.
func TestGroupThatOutlivesSIGKILLIsAnError(t *testing.T) {
	mgr := newTestManager(t, "play", Options{StopWait: 100 * time.Millisecond})
	gen, err := mgr.Start(testTarget("stuck"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	pgid := mgr.session.Pid()
	mgr.mu.Unlock()

	zombie := exec.Command(os.Args[0], "-test.run=^$")
	zombie.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
	if err := zombie.Start(); err != nil {
		t.Fatalf("start group member: %v", err)
	}
	t.Cleanup(func() { _ = zombie.Wait() })

	mgr.Stop()
	var last Event
	for drained := false; !drained; {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == gen {
				last = ev
			}
		default:
			drained = true
		}
	}
	if last.State != StateError {
		t.Fatalf("last event state=%s reason=%s, want %s", last.State, last.Reason, StateError)
	}
	notes := mgr.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0], "SIGKILL") {
		t.Fatalf("notes=%q want one about SIGKILL", notes)
	}
}
