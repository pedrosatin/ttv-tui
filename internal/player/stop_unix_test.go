//go:build unix

package player

import (
	"testing"
	"time"
)

// A session that ended on its own keeps its pgid until the next channel switch
// or Ctrl+Q. By then the number may lead another program's group: Stop must not
// signal it.
func TestStopOfEndedSessionSignalsNoGroup(t *testing.T) {
	mgr := newTestManager(t, "exit-after-play", Options{StopWait: 200 * time.Millisecond})
	gen, err := mgr.Start("ended")
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
