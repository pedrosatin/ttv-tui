package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// processAlive treats zombies as dead, like the Linux helper: they no longer run
// or play audio.
func processAlive(pid int) bool {
	start, err := osProcStartTime(pid)
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	return start != 0
}

func TestOSProcStartTimeOfRealProcesses(t *testing.T) {
	self, err := osProcStartTime(os.Getpid())
	if err != nil || self == 0 {
		t.Fatalf("osProcStartTime(self) = %d, %v", self, err)
	}
	if self != selfStart {
		t.Fatalf("osProcStartTime(self) = %d, selfStart = %d", self, selfStart)
	}
	if st, err := osProcStartTime(deadPid(t)); st != 0 || err != nil {
		t.Fatalf("osProcStartTime(dead) = %d, %v, want 0 with no error", st, err)
	}
}

func TestOSGroupHasArgReadsArgvOfRealGroup(t *testing.T) {
	socket := "/nonexistent/argv/i"
	pgid := startIdleGroup(t, socket).Process.Pid

	if found, err := osGroupHasEndpoint(pgid, socket); !found || err != nil {
		t.Fatalf("osGroupHasEndpoint(argv) = %v, %v, want true", found, err)
	}
	if found, err := osGroupHasEndpoint(pgid, "/nonexistent/other/i"); found || err != nil {
		t.Fatalf("osGroupHasEndpoint(other) = %v, %v, want false", found, err)
	}
}

func TestOSGroupHasArgIgnoresEnvironment(t *testing.T) {
	const needle = "/nonexistent/env-only/i"
	cmd := exec.Command(os.Args[0], "--", "idle", "no-socket")
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child", "LEAK="+needle)
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	group, err := startedGroup(cmd)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	t.Cleanup(func() {
		_ = group.kill()
		_ = cmd.Wait()
	})

	found, err := osGroupHasEndpoint(cmd.Process.Pid, needle)
	if err != nil {
		t.Fatalf("osGroupHasEndpoint: %v", err)
	}
	if found {
		t.Fatal("environment taken for argv")
	}
}

// traceKernel makes the registry judge with the real sysctls and logs every answer,
// so a failed run on macOS shows the evidence instead of an empty result: FindOrphans
// keeps a record it cannot judge without saying why. The trace prints on failure and
// under -v, never the arguments themselves.
func traceKernel(t *testing.T) {
	t.Helper()
	origStart, origArg := procStartTime, groupHasEndpoint
	t.Cleanup(func() { procStartTime, groupHasEndpoint = origStart, origArg })

	traced := traceProcs(t.Logf, darwinProcs)
	listGroup := traced.group
	traced.group = func(pgid int) ([]byte, error) {
		raw, err := listGroup(pgid)
		logKernelView(t, pgid, raw)
		return raw, err
	}
	procStartTime = func(pid int) (uint64, error) {
		start, err := traced.startTime(pid)
		t.Logf("procStartTime(%d) = %d, %v", pid, start, err)
		return start, err
	}
	groupHasEndpoint = func(pgid int, arg string) (bool, error) {
		found, err := traced.groupHasEndpoint(pgid, arg)
		t.Logf("groupHasEndpoint(%d, %q) = %v, %v", pgid, arg, found, err)
		return found, err
	}
}

// logKernelView logs what the kernel shows of pgid: the size of the whole table, the
// members kern.proc.pgrp listed with the verdict on each, and what ps sees, which
// goes through none of this code.
func logKernelView(t *testing.T, pgid int, raw []byte) {
	t.Helper()
	all, err := wholeTable()
	t.Logf("kern.proc.all: %d bytes, %d processes, %d in group %d, %s",
		len(all), len(all)/kinfoProcSize, countInGroup(all, pgid), pgid, errnoText(err))
	for _, row := range kinfoRows(raw) {
		verdict, _ := explainMember(row)
		t.Logf("  %s: %s", describeRow(row), verdict)
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,uid=,stat=,comm=").Output()
	if err != nil {
		t.Logf("ps: %v", err)
		return
	}
	for line := range strings.Lines(string(out)) {
		if fields := strings.Fields(line); len(fields) > 2 && fields[2] == strconv.Itoa(pgid) {
			t.Logf("  ps: %s", strings.Join(fields, " "))
		}
	}
}

// wholeTable retries ENOMEM: between asking the size and reading, the table can
// outgrow the five processes of slack the kernel adds (KERN_PROCSLOP).
func wholeTable() ([]byte, error) {
	raw, err := unix.SysctlRaw("kern.proc.all")
	for try := 0; errors.Is(err, syscall.ENOMEM) && try < 5; try++ {
		raw, err = unix.SysctlRaw("kern.proc.all")
	}
	return raw, err
}

func countInGroup(raw []byte, pgid int) int {
	count := 0
	for _, row := range kinfoRows(raw) {
		if int(row.Eproc.Pgid) == pgid {
			count++
		}
	}
	return count
}

// kinfoRows reads a kinfo_proc answer through the layout in x/sys, which also holds
// what the registry leaves out: owner, parent and name.
func kinfoRows(raw []byte) []unix.KinfoProc {
	rows := make([]unix.KinfoProc, len(raw)/kinfoProcSize)
	for i := range rows {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&rows[i])), kinfoProcSize), raw[i*kinfoProcSize:])
	}
	return rows
}

func describeRow(row unix.KinfoProc) string {
	comm, _, _ := bytes.Cut(row.Proc.P_comm[:], []byte{0})
	return fmt.Sprintf("pid=%d ppid=%d pgid=%d uid=%d ruid=%d stat=%d comm=%q",
		row.Proc.P_pid, row.Eproc.Ppid, row.Eproc.Pgid, row.Eproc.Ucred.Uid,
		row.Eproc.Pcred.P_ruid, row.Proc.P_stat, comm)
}

// explainMember reads one member's arguments the way groupHasEndpoint does and says what
// the kernel answered. It returns false for an answer xnu gives no reason for:
// sysctl_procargsx refuses a process it cannot find, or one with another effective
// uid unless the caller is root, and that uid is the e_ucred.cr_uid of the listing.
func explainMember(row unix.KinfoProc) (string, bool) {
	pid := int(row.Proc.P_pid)
	if int(row.Proc.P_stat) == kinfoStatZombie {
		return "zombie, skipped", true
	}
	raw, err := darwinProcs.args(pid)
	if err == nil {
		argv, parseErr := parseProcArgs2(raw)
		if parseErr == nil {
			return fmt.Sprintf("arguments read, %d bytes, %d in argv", len(raw), len(argv)), true
		}
		return fmt.Sprintf("arguments unparsable, %d bytes, %s: %v", len(raw), argsShape(raw), parseErr), false
	}
	refused := "arguments refused, " + errnoText(err)
	start := uint64(row.Proc.P_starttime.Sec)*1_000_000 + uint64(row.Proc.P_starttime.Usec)
	if now, nowErr := darwinProcs.startTime(pid); nowErr == nil && now != start {
		return refused + ", the process left", true
	}
	if row.Eproc.Ucred.Uid != uint32(os.Geteuid()) && errors.Is(err, syscall.EINVAL) {
		return fmt.Sprintf("%s, it belongs to uid %d", refused, row.Eproc.Ucred.Uid), true
	}
	return refused + ", no documented reason", false
}

// argsShape describes a KERN_PROCARGS2 answer without its text, which holds the
// environment.
func argsShape(raw []byte) string {
	if len(raw) < 4 {
		return "no argc"
	}
	return fmt.Sprintf("argc=%d, %d NUL bytes", int32(binary.LittleEndian.Uint32(raw)), bytes.Count(raw[4:], []byte{0}))
}

// The group listing must agree with the whole table: if kern.proc.pgrp misses a
// member, the registry cannot trust "no such group".
func TestKernelGroupListingMatchesWholeTable(t *testing.T) {
	pgid := startIdleGroup(t, "/nonexistent/listing/i").Process.Pid

	group, err := darwinProcs.group(pgid)
	t.Logf("kern.proc.pgrp[%d]: %d bytes, %s", pgid, len(group), errnoText(err))
	if err != nil {
		t.Fatalf("kern.proc.pgrp: %v", err)
	}
	all, err := wholeTable()
	t.Logf("kern.proc.all: %d bytes, %s", len(all), errnoText(err))
	if err != nil {
		t.Fatalf("kern.proc.all: %v", err)
	}

	listed, err := parseKinfoProcs(group)
	if err != nil {
		t.Fatalf("parse group: %v", err)
	}
	table, err := parseKinfoProcs(all)
	if err != nil {
		t.Fatalf("parse table: %v", err)
	}
	t.Logf("group %d: %d listed, %d processes in the table", pgid, len(listed), len(table))

	var listedPIDs, tablePIDs []int
	for _, p := range listed {
		listedPIDs = append(listedPIDs, p.pid)
	}
	for _, p := range table {
		if p.pgid == pgid {
			tablePIDs = append(tablePIDs, p.pid)
		}
	}
	slices.Sort(listedPIDs)
	slices.Sort(tablePIDs)
	if !slices.Equal(listedPIDs, []int{pgid}) || !slices.Equal(tablePIDs, listedPIDs) {
		t.Fatalf("group %d: kern.proc.pgrp lists %v, kern.proc.all holds %v", pgid, listedPIDs, tablePIDs)
	}
}

// launchd is alive and belongs to root: the kernel answers EINVAL, exactly as for a
// process that is gone, and the registry must call that "cannot tell".
func TestKernelRefusesArgumentsOfAnotherUser(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads the arguments of every process")
	}
	const launchd = 1
	start, err := darwinProcs.startTime(launchd)
	if err != nil || start == 0 {
		t.Fatalf("startTime(launchd) = %d, %v", start, err)
	}
	raw, err := darwinProcs.args(launchd)
	t.Logf("kern.procargs2[%d]: %d bytes, %s", launchd, len(raw), errnoText(err))
	if !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("kern.procargs2 of launchd: %v, want EINVAL", err)
	}
	found, err := osGroupHasEndpoint(launchd, "/nonexistent/launchd/i")
	t.Logf("osGroupHasEndpoint(%d) = %v, %v", launchd, found, err)
	if found || err == nil {
		t.Fatalf("osGroupHasEndpoint(launchd) = %v, %v, want false with an error", found, err)
	}
}

// The group this test runs in holds whatever started go test, which on a CI runner is
// not ours to choose. A member may make the registry answer "cannot tell", but only
// for a reason the kernel documents: anything else is a process this code cannot
// read, which would keep a record forever.
func TestKernelExplainsEveryMemberOfTheTestGroup(t *testing.T) {
	pgid := syscall.Getpgrp()
	raw, err := darwinProcs.group(pgid)
	t.Logf("kern.proc.pgrp[%d]: %d bytes, %s", pgid, len(raw), errnoText(err))
	if err != nil {
		t.Fatalf("kern.proc.pgrp: %v", err)
	}
	rows := kinfoRows(raw)
	t.Logf("group %d: %d members, self is %d, euid %d", pgid, len(rows), os.Getpid(), os.Geteuid())
	if !slices.ContainsFunc(rows, func(row unix.KinfoProc) bool { return int(row.Proc.P_pid) == os.Getpid() }) {
		t.Fatalf("group %d does not list this process", pgid)
	}
	for _, row := range rows {
		verdict, explained := explainMember(row)
		// A process in the middle of exec answers oddly for a moment.
		for try := 0; !explained && try < 10; try++ {
			time.Sleep(100 * time.Millisecond)
			verdict, explained = explainMember(row)
		}
		t.Logf("  %s: %s", describeRow(row), verdict)
		if !explained {
			t.Errorf("no documented reason for %s: %s", describeRow(row), verdict)
		}
	}
	found, err := osGroupHasEndpoint(pgid, "/nonexistent/test-group/i")
	t.Logf("osGroupHasEndpoint(%d) = %v, %v", pgid, found, err)
}
