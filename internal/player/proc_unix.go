//go:build unix

package player

import (
	"errors"
	"os/exec"
	"syscall"
)

// processGroup is the set of processes one playback owns, so that stopping it
// reaches mpv and not only streamlink. On Unix it is the POSIX process group led
// by the spawned process.
type processGroup struct {
	pgid int
}

// configureProcessGroup prepares cmd, before Start, to lead its own group.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// startedGroup returns the group of cmd, after a successful Start. The group
// already exists by then, so this cannot fail on Unix.
func startedGroup(cmd *exec.Cmd) (processGroup, error) {
	return processGroup{pgid: cmd.Process.Pid}, nil
}

// recordedGroup rebuilds a group from the id kept in a registry Record.
func recordedGroup(id int) processGroup {
	return processGroup{pgid: id}
}

// terminate asks every member to exit (SIGTERM).
func (g processGroup) terminate() error {
	return syscall.Kill(-g.pgid, syscall.SIGTERM)
}

// kill ends every member at once (SIGKILL).
func (g processGroup) kill() error {
	return syscall.Kill(-g.pgid, syscall.SIGKILL)
}

// alive reports whether any process is still a member of the group.
func (g processGroup) alive() bool {
	err := syscall.Kill(-g.pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// release has nothing to free: a process group holds no resource of ours.
func (g processGroup) release() {}

func startProcess(cmd *exec.Cmd) (*exec.Cmd, error) { return cmd, cmd.Start() }
func (g processGroup) containmentDegraded() bool    { return false }
