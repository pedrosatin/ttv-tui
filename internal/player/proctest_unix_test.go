//go:build unix

package player

import "syscall"

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

func killPid(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
