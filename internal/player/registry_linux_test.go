package player

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// processAlive treats zombies as dead: they no longer run or play audio.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	fields := procStatFields(pid)
	return len(fields) > 0 && fields[0] != "Z"
}

// semPidfd simula um kernel sem pidfd_open (anterior ao 5.3): Terminate cai na
// conferência pelo PGID.
func semPidfd(t *testing.T) {
	t.Helper()
	orig := pidfdOpen
	t.Cleanup(func() { pidfdOpen = orig })
	pidfdOpen = func(int, int) (int, error) { return -1, unix.ENOSYS }
}

func TestTerminateComESemPidfd(t *testing.T) {
	for _, tc := range []struct {
		nome    string
		preparo func(t *testing.T)
	}{
		{"com pidfd", func(*testing.T) {}},
		{"sem pidfd", semPidfd},
	} {
		t.Run(tc.nome+"/grupo trocado não leva SIGKILL", func(t *testing.T) {
			tc.preparo(t)
			assertTerminateSparesSwappedGroup(t)
		})
		t.Run(tc.nome+"/órfão teimoso leva SIGKILL", func(t *testing.T) {
			tc.preparo(t)
			s := startLiveSession(t)
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			cmd := helperCommand("term-parent-stubborn-child")(testTarget("teimoso"), s.socket)
			cmd.Env = append(cmd.Env, "TTV_PLAYER_HELPER_CHILD_PIDFILE="+pidFile)
			configureProcessGroup(cmd)
			_ = os.Remove(s.socket)
			if err := cmd.Start(); err != nil {
				t.Fatalf("start: %v", err)
			}
			go func() { _ = cmd.Wait() }()
			pgid := cmd.Process.Pid
			t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })
			child := waitPidFile(t, pidFile, 5*time.Second)
			orphan := Orphan{Record: Record{PGID: pgid, Socket: s.socket, Dir: s.sockDir}}
			if err := orphan.Terminate(300 * time.Millisecond); err != nil {
				t.Fatalf("Terminate: %v", err)
			}
			if processAlive(child) || recordedGroup(pgid).alive() {
				t.Fatalf("group %d (descendant %d) survived Terminate", pgid, child)
			}
		})
	}
}
