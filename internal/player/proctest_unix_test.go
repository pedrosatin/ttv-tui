//go:build unix

package player

import (
	"syscall"
	"testing"
)

// gracefulTerminate is true where terminate gives members until the StopWait
// deadline before killing them.
const gracefulTerminate = true

// pidExists is kill(pid, 0): a zombie still answers, unlike processAlive.
func pidExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// groupExists is kill(-pgid, 0).
func groupExists(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

func groupOfPid(pid int) (int, error) {
	return syscall.Getpgid(pid)
}

// groupHoldsPid reports whether pid is in the process group of s.
func groupHoldsPid(t *testing.T, s *Session, pid int) bool {
	t.Helper()
	pgid, err := groupOfPid(pid)
	if err != nil {
		t.Fatalf("group of %d: %v", pid, err)
	}
	return pgid == s.Pid()
}

func killPid(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
