//go:build !windows

package app

import (
	"os/exec"
	"syscall"
)

// detachWindow lets the window outlive the terminal that launched it.
func detachWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
