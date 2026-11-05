package player

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperTermParentStubbornChild plays the parent (streamlink) that exits on the
// first SIGTERM, after spawning a descendant (mpv) in the same process group that
// ignores SIGTERM. The descendant's argv carries the socket path, like mpv's
// --input-ipc-server does.
func helperTermParentStubbornChild(socket, login string) {
	// Linux would bind an empty name to an abstract address and carry on, with a
	// descendant that has no socket in its argv. Darwin refuses it.
	if socket == "" {
		fmt.Fprintln(os.Stderr, "term-parent-stubborn-child needs TTV_PLAYER_HELPER_SOCKET")
		os.Exit(2)
	}
	child := exec.Command(os.Args[0], "--", login, socket)
	child.Env = append(os.Environ(), "TTV_PLAYER_HELPER_MODE=stubborn-child")
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start child: %v\n", err)
		os.Exit(1)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	ln, err := listenEndpoint(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 512)
				for {
					if _, err := c.Read(buf); err != nil {
						_ = c.Close()
						return
					}
				}
			}(conn)
		}
	}()
	<-sig
	// Exit at once, leaving the stubborn child behind in our process group.
	os.Exit(0)
}

func helperStubbornChild() {
	signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	if path := os.Getenv("TTV_PLAYER_HELPER_CHILD_PIDFILE"); path != "" {
		tmp := path + ".tmp"
		_ = os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o600)
		_ = os.Rename(tmp, path)
	}
	for {
		time.Sleep(time.Hour)
	}
}

// helperSessionOwner plays the TUI in a test about its abrupt death: a Manager
// with a live session, and a process that never gets to call Stop.
func helperSessionOwner() {
	mgr := New(Options{
		Command:     helperCommand("term-parent-stubborn-child"),
		RuntimeDir:  os.Getenv("TTV_PLAYER_HELPER_RUNTIME"),
		RegistryDir: os.Getenv("TTV_PLAYER_HELPER_REGISTRY"),
	})
	if _, err := mgr.Start("owner"); err != nil {
		fmt.Fprintf(os.Stderr, "start session: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(time.Hour)
}

func waitPidFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s not written within %v", path, timeout)
	return 0
}

func TestStopKillsDescendantIgnoringTERMAfterParentExits(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("TTV_PLAYER_HELPER_CHILD_PIDFILE", pidFile)

	const stopWait = 400 * time.Millisecond
	mgr := newTestManager(t, "term-parent-stubborn-child", Options{StopWait: stopWait})
	gen, err := mgr.Start("stubborn")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateStarting, 3*time.Second)
	childPid := waitPidFile(t, pidFile, 5*time.Second)
	t.Cleanup(func() { killPid(childPid) })

	mgr.mu.Lock()
	sess := mgr.session
	mgr.mu.Unlock()
	parentPid := sess.Pid()
	if !groupHoldsPid(t, sess, childPid) {
		t.Fatalf("child %d is not in the group of parent %d", childPid, parentPid)
	}

	start := time.Now()
	mgr.Stop()
	elapsed := time.Since(start)

	if processAlive(childPid) {
		t.Fatalf("descendant %d still alive after Stop returned (elapsed %v)", childPid, elapsed)
	}
	if processAlive(parentPid) {
		t.Fatalf("parent %d still alive after Stop", parentPid)
	}
	if recordedGroup(parentPid).alive() {
		t.Fatalf("process group %d still has members after Stop", parentPid)
	}
	if gracefulTerminate && elapsed < stopWait {
		t.Fatalf("Stop returned after %v, before the %v SIGTERM deadline: the descendant cannot have been SIGKILLed", elapsed, stopWait)
	}
	t.Logf("parent %d and descendant %d gone after %v", parentPid, childPid, elapsed)
}

func TestManagerRegistryRecordLifecycle(t *testing.T) {
	regDir := t.TempDir()
	mgr := newTestManager(t, "play-child", Options{RegistryDir: regDir, StopWait: 300 * time.Millisecond})
	gen, err := mgr.Start("recorded")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)

	matches, _ := filepath.Glob(filepath.Join(regDir, recordPrefix+"*"+recordSuffix))
	if len(matches) != 1 {
		t.Fatalf("records while playing = %v, want exactly one", matches)
	}
	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("live owner's player reported as orphan: %+v", orphans)
	}

	mgr.Stop()
	matches, _ = filepath.Glob(filepath.Join(regDir, recordPrefix+"*"+recordSuffix))
	if len(matches) != 0 {
		t.Fatalf("records after Stop = %v, want none", matches)
	}
}

// deadPid returns the PID of a process that has already exited and been reaped.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=exit-error")
	_ = cmd.Run()
	if cmd.Process == nil {
		t.Fatal("could not spawn short-lived process")
	}
	return cmd.Process.Pid
}

// A record whose endpoint is not the one this OS builds for its directory describes
// a layout this cleanup does not know: it deletes nothing rather than guess what
// the directory holds.
func TestRemoveStaleFilesLeavesUnknownLayoutAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	conf := filepath.Join(dir, inputConfName)
	if err := os.WriteFile(conf, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	removeStaleFiles(Record{Dir: dir, Socket: "ttv-tui-1-2-3"})
	if _, err := os.Stat(conf); err != nil {
		t.Fatalf("input.conf removed for an unknown layout: %v", err)
	}
}

// startIdleGroup spawns a process in its own group, with socket in its argv like
// mpv's --input-ipc-server, and reaps it once it dies.
func startIdleGroup(t *testing.T, socket string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "--", "idle", socket)
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child")
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start idle group: %v", err)
	}
	group, err := startedGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("group of idle process: %v", err)
	}
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = group.kill()
		<-reaped
		group.release()
	})
	return cmd
}

// cannotInspect makes the registry behave as on an OS with no process inspection.
func cannotInspect(t *testing.T) {
	t.Helper()
	origStart, origArg := procStartTime, groupHasArg
	t.Cleanup(func() { procStartTime, groupHasArg = origStart, origArg })
	procStartTime = func(int) (uint64, error) { return 0, errors.ErrUnsupported }
	groupHasArg = func(int, string) (bool, error) { return false, errors.ErrUnsupported }
}

// A second instance on such an OS must not take a live session for a stale one:
// that is how it would delete the first instance's socket.
func TestFindOrphansKeepsWhatItCannotJudge(t *testing.T) {
	base := t.TempDir()
	regDir := filepath.Join(base, "players")
	sockDir := filepath.Join(base, "1-other")
	if err := os.Mkdir(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(sockDir, "i")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := startIdleGroup(t, socket)
	path, err := writeRecord(regDir, Record{
		OwnerPID:   deadPid(t),
		OwnerStart: 1,
		PGID:       cmd.Process.Pid,
		Socket:     socket,
		Dir:        sockDir,
		Generation: 1,
	})
	if err != nil {
		t.Fatalf("writeRecord: %v", err)
	}
	cannotInspect(t)

	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none when nothing can be verified", orphans)
	}
	for _, p := range []string{path, socket} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed without proof the player is gone: %v", filepath.Base(p), err)
		}
	}
}

func TestTerminateRefusesWhatItCannotJudge(t *testing.T) {
	base := t.TempDir()
	socket := filepath.Join(base, "i")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := startIdleGroup(t, socket)
	orphan := Orphan{Record: Record{PGID: cmd.Process.Pid, Socket: socket, Dir: base}}
	cannotInspect(t)

	if err := orphan.Terminate(50 * time.Millisecond); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Terminate = %v, want ErrUnsupported", err)
	}
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("unverified process was signalled")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("socket removed without proof the player is gone: %v", err)
	}
}
