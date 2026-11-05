//go:build unix && !linux && !darwin

package player

import "syscall"

// processAlive cannot tell a zombie from a running process on this OS.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
