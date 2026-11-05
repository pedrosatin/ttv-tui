package player

import "syscall"

// processAlive cannot tell a zombie from a live process without /proc. A killed
// descendant is reparented to launchd, which reaps it right away.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
