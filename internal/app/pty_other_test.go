//go:build unix && !linux && !darwin

package app

import (
	"os"
	"syscall"
	"testing"
)

// openPTY has no form on this OS yet: only Linux and macOS are supported, so the
// tests that need a terminal skip here.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	t.Skip("no PTY helper on this OS")
	return nil, nil
}

// alive cannot tell a zombie from a running process here.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
