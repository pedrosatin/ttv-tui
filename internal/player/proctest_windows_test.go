package player

import (
	"errors"
	"fmt"
	"os"
)

// Windows has no process group: until the Job Object exists (v1.8 ticket 09) a
// group here is its spawned process, so each answer is about that one process and
// says nothing of its descendants.

func pidExists(pid int) bool {
	return processAlive(pid)
}

func groupExists(pgid int) bool {
	return processAlive(pgid)
}

func groupOfPid(pid int) (int, error) {
	return 0, fmt.Errorf("process group of %d: %w", pid, errors.ErrUnsupported)
}

func killPid(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
		_ = p.Release()
	}
}
