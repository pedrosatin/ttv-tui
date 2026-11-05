package player

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

	owner := exec.Command(os.Args[0])
	owner.Env = append(os.Environ(),
		"TTV_PLAYER_HELPER=1",
		"TTV_PLAYER_HELPER_MODE=session-owner",
		"TTV_PLAYER_HELPER_RUNTIME="+runtimeDir,
		"TTV_PLAYER_HELPER_REGISTRY="+regDir,
		"TTV_PLAYER_HELPER_CHILD_PIDFILE="+pidFile,
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

	records, err := filepath.Glob(filepath.Join(regDir, recordPrefix+"*"+recordSuffix))
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %v, %v, want exactly one", records, err)
	}
	rec := readRecord(t, records[0])
	parent := openProcess(t, rec.PGID)
	if orphans, err := FindOrphans(regDir); err != nil || len(orphans) != 0 {
		t.Fatalf("FindOrphans with the owner alive = %+v, %v", orphans, err)
	}
	if _, err := os.Stat(records[0]); err != nil {
		t.Fatalf("record of a live owner removed: %v", err)
	}

	if err := owner.Process.Kill(); err != nil {
		t.Fatalf("kill owner: %v", err)
	}
	waitExit(t, ownerHandle, "owner")
	waitExit(t, parent, "streamlink's stand-in")
	waitExit(t, child, "mpv's stand-in")

	if _, err := os.Stat(filepath.Join(rec.Dir, inputConfName)); err != nil {
		t.Fatalf("abrupt death should leave the session's files behind: %v", err)
	}
	orphans, err := FindOrphans(regDir)
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

// A process outside any job could never be stopped with its tree, so it must not
// play at all.
func TestStartEndsProcessTheJobRefuses(t *testing.T) {
	orig := assignToJob
	t.Cleanup(func() { assignToJob = orig })
	assignToJob = func(windows.Handle, windows.Handle) error { return windows.ERROR_ACCESS_DENIED }

	runtimeDir, regDir := t.TempDir(), t.TempDir()
	var started *exec.Cmd
	mgr := New(Options{
		Command: func(login, endpoint string) *exec.Cmd {
			started = helperCommand("silent-hang")(login, endpoint)
			return started
		},
		RuntimeDir:  runtimeDir,
		RegistryDir: regDir,
	})
	t.Cleanup(mgr.Close)

	if _, err := mgr.Start("refused"); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("Start = %v, want ERROR_ACCESS_DENIED", err)
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
