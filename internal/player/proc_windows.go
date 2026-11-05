//go:build windows

package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// processGroup is the set of processes one playback owns.
//
// STUB: this compiles and stops the spawned process, nothing more. Windows has no
// process group, so a descendant (mpv) outlives streamlink here. The Job Object
// with KILL_ON_JOB_CLOSE (v1.8 ticket 09) belongs in this struct: create it in
// startedGroup, and make terminate, kill and alive act on it.
type processGroup struct {
	pid int
	// proc is nil for a group rebuilt from a record. It holds a live handle, so
	// killing through it cannot hit another process that reused the PID.
	proc *os.Process
}

// configureProcessGroup has nothing to set yet: the Job Object is attached after
// Start.
func configureProcessGroup(*exec.Cmd) {}

// startedGroup returns the group of cmd, after a successful Start.
func startedGroup(cmd *exec.Cmd) processGroup {
	return processGroup{pid: cmd.Process.Pid, proc: cmd.Process}
}

// recordedGroup rebuilds a group from the id kept in a registry Record. It has no
// handle: a recorded PID alone does not prove the process is still ours.
func recordedGroup(id int) processGroup {
	return processGroup{pid: id}
}

// terminate kills the process: Windows has no SIGTERM to send to a process
// without a console of its own.
func (g processGroup) terminate() error {
	return g.kill()
}

// kill ends the spawned process only, and refuses a group without a handle.
func (g processGroup) kill() error {
	if g.proc == nil {
		return fmt.Errorf("player: kill recorded process %d: %w", g.pid, errors.ErrUnsupported)
	}
	return g.proc.Kill()
}

// alive reports whether the spawned process still runs. Its descendants are not
// seen until the Job Object exists.
func (g processGroup) alive() bool {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(g.pid))
	if err != nil {
		// Access denied means the process exists and belongs to someone else.
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	event, err := syscall.WaitForSingleObject(h, 0)
	return err == nil && event == syscall.WAIT_TIMEOUT
}
