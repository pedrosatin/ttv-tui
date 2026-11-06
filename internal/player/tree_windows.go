//go:build windows

package player

import (
	"errors"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree keeps handles, not just PIDs: a terminated parent's PID must not
// be reused while we identify its descendants. Snapshots are best effort, not
// the kernel containment a Job Object provides.
type processTree struct {
	mu      sync.Mutex
	members map[uint32]treeMember
}

type treeMember struct {
	handle  windows.Handle
	created uint64
}

func openTreeMember(pid uint32) (treeMember, error) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return treeMember{}, err
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		_ = windows.CloseHandle(h)
		return treeMember{}, err
	}
	return treeMember{h, uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)}, nil
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	// Duplicate the handle Start already owns. Reopening by PID may be denied
	// while a fast-exiting child is terminating, before its handle is signaled.
	var h windows.Handle
	var duplicateErr error
	err := cmd.Process.WithHandle(func(handle uintptr) {
		duplicateErr = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(handle),
			windows.CurrentProcess(), &h, 0, false, windows.DUPLICATE_SAME_ACCESS)
	})
	if err != nil {
		return nil, err
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	member := treeMember{h, uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)}
	return &processTree{members: map[uint32]treeMember{uint32(cmd.Process.Pid): member}}, nil
}

// discover must run with mu held. Creation times reject stale parent-PID links
// to a process from before this playback. Retained handles pin discovered PIDs.
func (t *processTree) discover() error {
	// Reject a PID reused between taking the snapshot and opening its handle.
	var observed windows.Filetime
	windows.GetSystemTimeAsFileTime(&observed)
	observedAt := uint64(observed.HighDateTime)<<32 | uint64(observed.LowDateTime)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var entries []windows.ProcessEntry32
	err = windows.Process32First(snapshot, &entry)
	for err == nil {
		entries = append(entries, entry)
		err = windows.Process32Next(snapshot, &entry)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	for added := true; added; {
		added = false
		for _, entry := range entries {
			parent, ok := t.members[entry.ParentProcessID]
			if !ok {
				continue
			}
			if _, known := t.members[entry.ProcessID]; known {
				continue
			}
			child, err := openTreeMember(entry.ProcessID)
			if err != nil {
				continue
			} // Exited or inaccessible: no safe handle to kill.
			if child.created < parent.created || child.created > observedAt {
				_ = windows.CloseHandle(child.handle)
				continue
			}
			t.members[entry.ProcessID] = child
			added = true
		}
	}
	return nil
}

func (t *processTree) kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.discover()
	for _, member := range t.members {
		if !processExited(member.handle) {
			err = errors.Join(err, windows.TerminateProcess(member.handle, jobKilledExitCode))
		}
	}
	return err
}

func (t *processTree) alive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.discover()
	for _, member := range t.members {
		if !processExited(member.handle) {
			return true
		}
	}
	return false
}

func (t *processTree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for pid, member := range t.members {
		_ = windows.CloseHandle(member.handle)
		delete(t.members, pid)
	}
}
