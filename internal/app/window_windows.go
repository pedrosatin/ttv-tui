package app

import "os/exec"

// detachWindow has nothing to do: the window only exists on Linux.
func detachWindow(*exec.Cmd) {}
