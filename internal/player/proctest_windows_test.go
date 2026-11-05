package player

import (
	"os"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// gracefulTerminate is false: TerminateJobObject ends every member at once, so
// Stop does not wait for the StopWait deadline.
const gracefulTerminate = false

func pidExists(pid int) bool {
	return processAlive(pid)
}

// A group here goes by the PID of its spawned process, the one a Record keeps, so
// these two can only answer for that process. Ask jobMembers about descendants.

func groupExists(pgid int) bool {
	return processAlive(pgid)
}

func groupOfPid(pid int) (int, error) {
	return pid, nil
}

// groupHoldsPid reports whether pid is in the Job Object of s.
func groupHoldsPid(t *testing.T, s *Session, pid int) bool {
	t.Helper()
	return slices.Contains(jobMembers(t, s.group), pid)
}

// jobMembers lists the PIDs of a group's job. JOBOBJECT_BASIC_PROCESS_ID_LIST ends
// in a variable array; a playback has far fewer than maxJobMembers processes.
func jobMembers(t *testing.T, g processGroup) []int {
	t.Helper()
	const maxJobMembers = 64
	if g.job == nil {
		t.Fatal("group has no job")
	}
	var list struct {
		Assigned uint32
		Listed   uint32
		IDs      [maxJobMembers]uintptr
	}
	g.job.mu.Lock()
	defer g.job.mu.Unlock()
	if err := windows.QueryInformationJobObject(g.job.handle, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		t.Fatalf("list job members: %v", err)
	}
	members := make([]int, list.Listed)
	for i := range members {
		members[i] = int(list.IDs[i])
	}
	return members
}

func killPid(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
		_ = p.Release()
	}
}
