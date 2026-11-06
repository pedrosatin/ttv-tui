package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// exitWait bounds how long a process ended by the kernel may take to be gone. It
// is a limit for a slow CI machine, not an expected delay.
const exitWait = 10 * time.Second

// openProcess holds a process by handle, so a test can wait for that one process
// and its PID cannot be reused meanwhile.
func openProcess(t *testing.T, pid int) windows.Handle {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("open process %d: %v", pid, err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return h
}

func waitExit(t *testing.T, h windows.Handle, what string) {
	t.Helper()
	event, err := windows.WaitForSingleObject(h, uint32(exitWait.Milliseconds()))
	if err != nil {
		t.Fatalf("wait for %s: %v", what, err)
	}
	if event != windows.WAIT_OBJECT_0 {
		t.Fatalf("%s still alive after %v", what, exitWait)
	}
}

// startIdle spawns a helper that stays until killed. The test owns its group.
func startIdle(t *testing.T) (*exec.Cmd, processGroup) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	reaped := make(chan struct{})
	group, err := startedGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("startedGroup: %v", err)
	}
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = group.kill()
		<-reaped
		group.release()
	})
	return cmd, group
}

// The failure the job exists for: streamlink gone, mpv still playing. Without the
// job nothing would see that descendant, let alone reach it.
func TestStreamlinkDeathLeavesDescendantForStop(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("TTV_PLAYER_HELPER_CHILD_PIDFILE", pidFile)

	mgr := newTestManager(t, "term-parent-stubborn-child", Options{})
	gen, err := mgr.Start("tree")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	childPid := waitPidFile(t, pidFile, 5*time.Second)
	t.Cleanup(func() { killPid(childPid) })
	child := openProcess(t, childPid)

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	if err := sess.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill parent: %v", err)
	}
	<-sess.waitDone

	if !processAlive(childPid) {
		t.Fatal("descendant died with its parent: this test proves nothing")
	}
	if !sess.group.alive() {
		t.Fatalf("group reports no members with descendant %d running", childPid)
	}
	mgr.Stop()
	waitExit(t, child, "descendant")
}

// The TUI killed outright runs no code at all: only the kernel closing its handle
// can end the tree. What it leaves on disk is then residue for the next run.
func TestAbruptOwnerDeathEndsTreeAndLeavesOnlyResidue(t *testing.T) {
	base := t.TempDir()
	regDir := filepath.Join(base, "players")
	runtimeDir := filepath.Join(base, "run")
	pidFile := filepath.Join(base, "child.pid")
	readyFile := filepath.Join(base, "owner.ready")

	owner := exec.Command(os.Args[0])
	owner.Env = append(os.Environ(),
		"TTV_PLAYER_HELPER=1",
		"TTV_PLAYER_HELPER_MODE=session-owner",
		"TTV_PLAYER_HELPER_RUNTIME="+runtimeDir,
		"TTV_PLAYER_HELPER_REGISTRY="+regDir,
		"TTV_PLAYER_HELPER_CHILD_PIDFILE="+pidFile,
		"TTV_PLAYER_HELPER_OWNER_READY="+readyFile,
	)
	if err := owner.Start(); err != nil {
		t.Fatalf("start owner: %v", err)
	}
	ownerHandle := openProcess(t, owner.Process.Pid)
	reaped := make(chan struct{})
	go func() {
		_ = owner.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = owner.Process.Kill()
		<-reaped
	})

	childPid := waitPidFile(t, pidFile, 10*time.Second)
	t.Cleanup(func() { killPid(childPid) })
	child := openProcess(t, childPid)

	logState := func(phase string, rec Record) {
		t.Helper()
		for _, pid := range []int{owner.Process.Pid, rec.PGID, childPid} {
			if pid > 0 {
				alive, err := pidRunning(pid)
				t.Logf("%s: pid=%d alive=%v err=%v", phase, pid, alive, err)
			}
		}
		if err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			t.Logf("%s: path=%s entry=%v err=%v", phase, path, entry, err)
			return err
		}); err != nil {
			t.Logf("%s: list files: %v", phase, err)
		}
	}
	var rec Record
	t.Cleanup(func() {
		if t.Failed() {
			logState("failure before process cleanup", rec)
		}
	})
	// child.pid proves only that the descendant started. Wait for Start to
	// return before asserting that its owner has committed the registry record.
	if readyPID := waitPidFile(t, readyFile, exitWait); readyPID != owner.Process.Pid {
		t.Fatalf("ready owner PID = %d, want %d", readyPID, owner.Process.Pid)
	}
	logState("owner ready, before reading record", rec)
	records, err := filepath.Glob(filepath.Join(regDir, recordPrefix+"*"+recordSuffix))
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %v, %v, want exactly one", records, err)
	}
	rec = readRecord(t, records[0])
	if rec.OwnerPID != owner.Process.Pid {
		t.Fatalf("record owner PID = %d, want %d", rec.OwnerPID, owner.Process.Pid)
	}
	parent := openProcess(t, rec.PGID)
	logState("before live-owner scan", rec)
	if orphans, err := FindOrphans(regDir); err != nil || len(orphans) != 0 {
		t.Fatalf("FindOrphans with the owner alive = %+v, %v", orphans, err)
	}
	if _, err := os.Stat(records[0]); err != nil {
		t.Fatalf("record of a live owner removed: %v", err)
	}
	logState("after live-owner scan, before killing owner", rec)

	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("kill owner: %v", err)
	}
	waitExit(t, ownerHandle, "owner")
	waitExit(t, parent, "streamlink's stand-in")
	waitExit(t, child, "mpv's stand-in")
	logState("all three handles signaled, before residue scan", rec)

	assertResidueKept(t, records[0], rec.Dir)
	orphans, err := FindOrphans(regDir)
	logState("after residue scan", rec)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none: the job ended them", orphans)
	}
	for _, path := range []string{records[0], rec.Dir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", path, err)
		}
	}
}

// Each channel switch makes a job, so one left open is a handle lost per session.
func TestStopClosesTheJob(t *testing.T) {
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start("released")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	job := mgr.session.group.job
	mgr.mu.Unlock()

	mgr.Stop()

	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle != 0 {
		t.Fatal("job handle still open after Stop")
	}
}

func TestReleaseEndsWhatIsLeftInTheJob(t *testing.T) {
	cmd, group := startIdle(t)
	h := openProcess(t, cmd.Process.Pid)

	group.release()

	waitExit(t, h, "job member")
	if group.alive() {
		t.Fatal("released group reports members")
	}
	if err := group.kill(); err != nil {
		t.Fatalf("kill after release: %v", err)
	}
}

// streamlink can fail before the Assign. That is its outcome to report, not ours.
func TestStartedGroupAcceptsProcessThatAlreadyExited(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=offline-exit0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()
	waitExit(t, openProcess(t, cmd.Process.Pid), "helper")

	group, err := startedGroup(cmd)
	if err != nil {
		t.Fatalf("startedGroup: %v", err)
	}
	defer group.release()
	if group.alive() {
		t.Fatal("group of an exited process reports members")
	}
}

// Errors other than ACCESS_DENIED still abort and reap the process.
func TestStartEndsProcessTheJobRefuses(t *testing.T) {
	orig := assignToJob
	t.Cleanup(func() { assignToJob = orig })
	assignToJob = func(windows.Handle, windows.Handle) error { return windows.ERROR_INVALID_HANDLE }

	runtimeDir, regDir := t.TempDir(), t.TempDir()
	var started *exec.Cmd
	originalStart := startCommand
	t.Cleanup(func() { startCommand = originalStart })
	startCommand = func(cmd *exec.Cmd) error {
		started = cmd
		return originalStart(cmd)
	}
	mgr := New(Options{
		Command: func(login, endpoint string) *exec.Cmd {
			return helperCommand("silent-hang")(login, endpoint)
		},
		RuntimeDir:  runtimeDir,
		RegistryDir: regDir,
	})
	t.Cleanup(mgr.Close)

	if _, err := mgr.Start("refused"); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("Start = %v, want ERROR_INVALID_HANDLE", err)
	}
	if started == nil || started.ProcessState == nil {
		t.Fatal("refused process was not ended and reaped")
	}
	for _, dir := range []string{runtimeDir, regDir} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("%s after a refused start = %v, %v, want empty", dir, entries, err)
		}
	}
}

func TestRecordedGroupKillsNothing(t *testing.T) {
	cmd, _ := startIdle(t)
	if err := recordedGroup(cmd.Process.Pid).kill(); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("kill = %v, want ErrUnsupported", err)
	}
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("a recorded PID was killed")
	}
}

func TestStartBreakawayRetry(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprint("denied=", denied), func(t *testing.T) {
			original := startCommand
			t.Cleanup(func() { startCommand = original })
			calls := 0
			var first *exec.Cmd
			startCommand = func(cmd *exec.Cmd) error {
				calls++
				if calls == 1 {
					first = cmd
				}
				hasFlag := cmd.SysProcAttr.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB != 0
				if calls == 1 && !hasFlag {
					t.Error("first start did not request breakaway")
				}
				if calls == 2 && hasFlag {
					t.Error("retry still requests breakaway")
				}
				if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
					t.Error("caller flag lost")
				}
				if denied && calls == 1 {
					// Consume the real Cmd.Start state without depending on the host's
					// job policy. A bare injected error would allow reusing this Cmd.
					cmd.Err = windows.ERROR_ACCESS_DENIED
					err := cmd.Start()
					cmd.Err = nil
					return err
				}
				if calls == 2 {
					if cmd == first {
						t.Fatal("retry reused the consumed command")
					}
					if cmd.SysProcAttr == first.SysProcAttr || first.SysProcAttr.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB == 0 {
						t.Fatal("retry changed the original process attributes")
					}
					attr := *first.SysProcAttr
					attr.CreationFlags &^= windows.CREATE_BREAKAWAY_FROM_JOB
					if cmd.Path != first.Path || !reflect.DeepEqual(cmd.Args, first.Args) ||
						!reflect.DeepEqual(cmd.Env, first.Env) || cmd.Dir != first.Dir ||
						cmd.Stdin != first.Stdin || cmd.Stdout != first.Stdout || cmd.Stderr != first.Stderr ||
						!reflect.DeepEqual(cmd.SysProcAttr, &attr) || cmd.WaitDelay != first.WaitDelay {
						t.Fatal("retry lost the command specification")
					}
				}
				// The actual runner may also forbid breakaway; retry remains exercised
				// deterministically here, independently from the host's job policy.
				cmd.SysProcAttr.CreationFlags &^= windows.CREATE_BREAKAWAY_FROM_JOB
				return cmd.Start()
			}
			mgr := newTestManager(t, "play", Options{})
			base := mgr.opts.Command
			dir := t.TempDir()
			mgr.opts.Command = func(login, endpoint string) *exec.Cmd {
				cmd := base(login, endpoint)
				cmd.Dir = dir
				cmd.Stdin = strings.NewReader("")
				cmd.WaitDelay = time.Second
				cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
				return cmd
			}
			gen, err := mgr.Start("breakaway")
			if err != nil {
				t.Fatal(err)
			}
			_ = waitForState(t, mgr.Events(), gen, StatePlaying, exitWait)
			if mgr.session.cmd.Process == nil || (denied && mgr.session.cmd == first) {
				t.Fatal("session did not retain the started command")
			}
			want := 1
			if denied {
				want = 2
			}
			if calls != want {
				t.Fatalf("start calls = %d, want %d", calls, want)
			}
			mgr.Stop()
		})
	}
}

func TestAssignAccessDeniedDegradesAndStopKillsTree(t *testing.T) {
	originalStart := startCommand
	t.Cleanup(func() { startCommand = originalStart })
	calls := 0
	startCommand = func(cmd *exec.Cmd) error {
		calls++
		if calls == 1 {
			return windows.ERROR_ACCESS_DENIED
		}
		if cmd.SysProcAttr.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB != 0 {
			t.Fatal("breakaway on retry")
		}
		return cmd.Start()
	}
	original := assignToJob
	t.Cleanup(func() { assignToJob = original })
	assignToJob = func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED }
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("TTV_PLAYER_HELPER_CHILD_PIDFILE", pidFile)
	mgr := newTestManager(t, "term-parent-stubborn-child", Options{})
	gen, err := mgr.Start("restricted")
	if err != nil {
		t.Fatal(err)
	}
	childPID := waitPidFile(t, pidFile, exitWait)
	t.Cleanup(func() { killPid(childPID) })
	child := openProcess(t, childPID)
	_ = waitForState(t, mgr.Events(), gen, StateStarting, exitWait)
	ev := waitForState(t, mgr.Events(), gen, StateStarting, exitWait)
	if !ev.ContainmentDegraded {
		t.Fatal("starting without containment warning")
	}
	sess := mgr.session
	parent := openProcess(t, sess.Pid())
	// Parent may exit before Stop. Its pinned handle must still identify children.
	if err := sess.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-sess.waitDone
	mgr.Stop()
	waitExit(t, parent, "fallback parent")
	waitExit(t, child, "fallback descendant")
	if len(sess.group.fallback.members) != 0 {
		t.Fatal("fallback handles leaked")
	}
}

func TestStartDoesNotRetryOtherErrors(t *testing.T) {
	original := startCommand
	t.Cleanup(func() { startCommand = original })
	calls := 0
	startCommand = func(*exec.Cmd) error { calls++; return windows.ERROR_FILE_NOT_FOUND }
	mgr := newTestManager(t, "play", Options{})
	if _, err := mgr.Start("missing"); !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("Start: %v", err)
	}
	if calls != 1 {
		t.Fatalf("start calls = %d", calls)
	}
}

// Force the refusal while the child races toward exit, not only for a long-lived
// process. This also exercises the fallback's duplicated handle during teardown.
func TestFastExitAccessDeniedKeepsLastErrorLine(t *testing.T) {
	original := assignToJob
	t.Cleanup(func() { assignToJob = original })
	assignToJob = func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED }
	TestFastExitKeepsLastErrorLine(t)
}

func TestAssignAccessDeniedPlaybackContinues(t *testing.T) {
	original := assignToJob
	t.Cleanup(func() { assignToJob = original })
	assignToJob = func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED }
	mgr := newTestManager(t, "play", Options{})
	gen, err := mgr.Start("restricted")
	if err != nil {
		t.Fatal(err)
	}
	ev := waitForState(t, mgr.Events(), gen, StatePlaying, exitWait)
	if !ev.ContainmentDegraded {
		t.Fatal("playing without containment warning")
	}
	mgr.Stop()
}
