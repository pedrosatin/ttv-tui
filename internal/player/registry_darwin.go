package player

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// darwinProcs asks the kernel directly. SysctlRaw and not SysctlKinfoProc: the
// latter turns the empty answer for a missing PID into EIO, which would make "gone"
// look like "cannot tell".
var darwinProcs = sysctlProcs{
	proc:  func(pid int) ([]byte, error) { return unix.SysctlRaw("kern.proc.pid", pid) },
	group: func(pgid int) ([]byte, error) { return unix.SysctlRaw("kern.proc.pgrp", pgid) },
	args:  func(pid int) ([]byte, error) { return unix.SysctlRaw("kern.procargs2", pid) },
}

// The parser reads kinfo_proc by offset so it can be tested off macOS. Each line
// indexes a one-element array with the distance from the layout in x/sys, so any
// drift is a compile error on every Darwin build.
var (
	_ = [1]struct{}{}[unsafe.Sizeof(unix.KinfoProc{})-kinfoProcSize]
	_ = [1]struct{}{}[unsafe.Offsetof(unix.KinfoProc{}.Proc)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_starttime)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_starttime.Sec)-kinfoStartSec]
	_ = [1]struct{}{}[unsafe.Offsetof(unix.KinfoProc{}.Proc)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_starttime)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_starttime.Usec)-kinfoStartUsec]
	_ = [1]struct{}{}[unsafe.Offsetof(unix.KinfoProc{}.Proc)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_stat)-kinfoStat]
	_ = [1]struct{}{}[unsafe.Offsetof(unix.KinfoProc{}.Proc)+unsafe.Offsetof(unix.KinfoProc{}.Proc.P_pid)-kinfoPID]
	_ = [1]struct{}{}[unsafe.Offsetof(unix.KinfoProc{}.Eproc)+unsafe.Offsetof(unix.KinfoProc{}.Eproc.Pgid)-kinfoPGID]
)

// osGroupHasEndpoint reports whether a live member of pgid is the player of
// endpoint.
func osGroupHasEndpoint(pgid int, endpoint string) (bool, error) {
	return darwinProcs.groupHasEndpoint(pgid, endpoint)
}

// osProcStartTime returns p_starttime in microseconds, or 0 when the process is
// gone.
func osProcStartTime(pid int) (uint64, error) {
	return darwinProcs.startTime(pid)
}
