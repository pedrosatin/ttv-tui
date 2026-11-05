//go:build windows

package player

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGroup is the set of processes one playback owns: the Job Object its
// spawned process runs in. Windows has no process group and no SIGHUP, and a
// child outlives its parent, so only the job reaches mpv through streamlink.
//
// The job has KILL_ON_JOB_CLOSE and this process holds its only handle, which is
// not inheritable. When this process dies, however abruptly, the kernel closes
// that handle and ends the whole tree. No code of ours has to run for that.
//
// # The race between Start and Assign
//
// The process runs outside the job from CreateProcess until
// AssignProcessToJobObject, and whatever it spawns in that window stays outside
// for good. Closing the window needs a process started suspended, which Go does
// not offer: CreationFlags accepts CREATE_SUSPENDED, but syscall.StartProcess
// closes the thread handle that ResumeThread would need, and it has no way to
// pass PROC_THREAD_ATTRIBUTE_JOB_LIST.
//
// So this code assumes streamlink takes longer to create mpv than this process
// takes to reach Assign. That is an assumption, not a guarantee: the window is a
// few syscalls, and streamlink has to start Python and open the stream before it
// launches the player. If this process were held off the CPU for that long, mpv
// would run outside the job, and neither Stop nor the kernel would end it.
//
// The tightest case is a streamlink.exe that is only a launcher, as pip generates:
// it creates python.exe within milliseconds, not seconds. A python.exe that beat
// the Assign would sit in the launcher's own job, which allows silent breakaway,
// so mpv would still end up outside every job.
type processGroup struct {
	pid int
	// job is nil for a group rebuilt from a record: a job dies with its owner, so
	// a later run has nothing to open.
	job *jobObject
}

// jobObject owns the handle of one session's job. Stop and the supervisor can
// reach it at the same time, and a closed handle value may be reused, so every
// use goes through mu.
type jobObject struct {
	mu sync.Mutex
	// handle is 0 once released.
	handle windows.Handle
}

// jobKilledExitCode is the exit status of a process ended through the job. It is
// the one os.Process.Kill leaves.
const jobKilledExitCode = 1

// jobBasicAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, which x/sys does
// not declare.
type jobBasicAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// assignToJob is a variable so a test can make the kernel refuse the process.
var assignToJob = windows.AssignProcessToJobObject

// newJobObject creates an empty job. It sets no BREAKAWAY limit on purpose: a job
// nested inside this one may allow breakaway, as pip's launcher does, and a child
// leaving that inner job moves up the chain only until a job that forbids it.
func newJobObject() (*jobObject, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("player: create job object: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("player: configure job object: %w", err)
	}
	return &jobObject{handle: h}, nil
}

func (j *jobObject) terminate() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(j.handle, jobKilledExitCode); err != nil {
		return fmt.Errorf("player: terminate job object: %w", err)
	}
	return nil
}

// active reports whether the job has members. A released job has none: closing it
// ended them. A failed query counts as active, because "cannot tell" is not "gone".
func (j *jobObject) active() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle == 0 {
		return false
	}
	var info jobBasicAccounting
	if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return true
	}
	return info.ActiveProcesses > 0
}

func (j *jobObject) release() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle != 0 {
		_ = windows.CloseHandle(j.handle)
		j.handle = 0
	}
}

// configureProcessGroup has nothing to set: the job is attached after Start.
func configureProcessGroup(*exec.Cmd) {}

// startedGroup puts cmd in a new job, after a successful Start. See processGroup
// for the race this leaves open.
//
// A process that already exited cannot be assigned, and that is no failure: it
// lived too briefly to create mpv, and the session must report its real outcome.
// A live process the kernel refuses is one nothing could stop later, so that is
// an error and the caller must end it.
func startedGroup(cmd *exec.Cmd) (processGroup, error) {
	job, err := newJobObject()
	if err != nil {
		return processGroup{}, err
	}
	var assignErr error
	err = cmd.Process.WithHandle(func(h uintptr) {
		process := windows.Handle(h)
		if assignErr = assignToJob(job.handle, process); assignErr != nil && processExited(process) {
			assignErr = nil
		}
	})
	if err == nil {
		err = assignErr
	}
	if err != nil {
		job.release()
		return processGroup{}, fmt.Errorf("player: assign process %d to job object: %w", cmd.Process.Pid, err)
	}
	return processGroup{pid: cmd.Process.Pid, job: job}, nil
}

func processExited(h windows.Handle) bool {
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == windows.WAIT_OBJECT_0
}

// recordedGroup rebuilds a group from the id kept in a registry Record. It has no
// job, and a PID alone proves nothing: Windows reuses them quickly.
func recordedGroup(id int) processGroup {
	return processGroup{pid: id}
}

// terminate ends every member at once. There is nothing gentler to send: Windows
// has no SIGTERM for a process without a console of its own. So StopWait never
// runs out here, and mpv and streamlink get no chance to clean up after themselves.
func (g processGroup) terminate() error {
	return g.kill()
}

// kill ends every member through TerminateJobObject, and refuses a group without
// a job: a recorded PID may belong to anyone by now.
func (g processGroup) kill() error {
	if g.job == nil {
		return fmt.Errorf("player: kill recorded process %d: %w", g.pid, errors.ErrUnsupported)
	}
	return g.job.terminate()
}

// alive reports whether any member still runs. A group without a job can only
// answer for its PID, and an error there counts as running.
func (g processGroup) alive() bool {
	if g.job != nil {
		return g.job.active()
	}
	running, err := pidRunning(g.pid)
	return running || err != nil
}

// release closes the job. Anything still in it dies, so the caller does this only
// after the group was stopped.
func (g processGroup) release() {
	if g.job != nil {
		g.job.release()
	}
}

// pidRunning asks whether some process runs under pid, whichever it is. An error
// means the PID exists or may exist and cannot be asked, as with another user's
// process.
func pidRunning(pid int) (bool, error) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("player: open process %d: %w", pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return false, fmt.Errorf("player: wait on process %d: %w", pid, err)
	}
	return event == uint32(windows.WAIT_TIMEOUT), nil
}
