package player

import (
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
	child := exec.Command(os.Args[0], "--", login, socket)
	child.Env = append(os.Environ(), "TTV_PLAYER_HELPER_MODE=stubborn-child")
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start child: %v\n", err)
		os.Exit(1)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	ln, err := net.Listen("unix", socket)
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

// processAlive treats zombies as dead: they no longer run or play audio.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	fields := procStatFields(pid)
	return len(fields) > 0 && fields[0] != "Z"
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
	t.Cleanup(func() { _ = syscall.Kill(childPid, syscall.SIGKILL) })

	mgr.mu.Lock()
	parentPid := mgr.session.Pid()
	mgr.mu.Unlock()
	pgid, err := syscall.Getpgid(childPid)
	if err != nil {
		t.Fatalf("Getpgid(child): %v", err)
	}
	if pgid != parentPid {
		t.Fatalf("child pgid = %d, want parent's group %d", pgid, parentPid)
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
	if groupAlive(pgid) {
		t.Fatalf("process group %d still has members after Stop", pgid)
	}
	if elapsed < stopWait {
		t.Fatalf("Stop returned after %v, before the %v SIGTERM deadline: the descendant cannot have been SIGKILLed", elapsed, stopWait)
	}
	t.Logf("parent %d exited on SIGTERM; descendant %d ignored it and was SIGKILLed after %v", parentPid, childPid, elapsed)
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

func TestFindOrphansDetectsAndTerminatesLeftoverPlayer(t *testing.T) {
	base := t.TempDir()
	regDir := filepath.Join(base, "players")
	sockDir := filepath.Join(base, "7-orphan")
	if err := os.Mkdir(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(sockDir, "i")
	pidFile := filepath.Join(base, "child.pid")

	// A player left behind by a run that died: its own process group, a parent
	// that obeys SIGTERM and a descendant that ignores it.
	cmd := exec.Command(os.Args[0], "--", "orphaned", socket)
	cmd.Env = append(os.Environ(),
		"TTV_PLAYER_HELPER=1",
		"TTV_PLAYER_HELPER_MODE=term-parent-stubborn-child",
		"TTV_PLAYER_HELPER_CHILD_PIDFILE="+pidFile,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	// Reap the leader promptly, as init would for a real orphan.
	go func() { _ = cmd.Wait() }()
	pgid := cmd.Process.Pid
	childPid := waitPidFile(t, pidFile, 5*time.Second)
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	if _, err := writeRecord(regDir, Record{
		OwnerPID:   deadPid(t),
		OwnerStart: 1,
		PGID:       pgid,
		Socket:     socket,
		Dir:        sockDir,
		Login:      "orphaned",
		Generation: 7,
		StartedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("writeRecord: %v", err)
	}

	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].PGID != pgid || orphans[0].Login != "orphaned" {
		t.Fatalf("orphans = %+v, want the leftover group %d", orphans, pgid)
	}

	if err := orphans[0].Terminate(300 * time.Millisecond); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if processAlive(childPid) || groupAlive(pgid) {
		t.Fatalf("orphan group %d (descendant %d) survived Terminate", pgid, childPid)
	}
	if _, err := os.Stat(orphans[0].path); !os.IsNotExist(err) {
		t.Fatalf("record not removed after Terminate: %v", err)
	}
	if _, err := os.Stat(sockDir); !os.IsNotExist(err) {
		t.Fatalf("socket dir not removed after Terminate: %v", err)
	}
	if again, _ := FindOrphans(regDir); len(again) != 0 {
		t.Fatalf("orphans after Terminate = %+v, want none", again)
	}
}

func TestFindOrphansIgnoresStaleRecycledAndLiveOwner(t *testing.T) {
	regDir := t.TempDir()
	dead := deadPid(t)

	// Player group already gone: stale record, removed silently.
	stale, err := writeRecord(regDir, Record{OwnerPID: dead, OwnerStart: 1, PGID: dead, Socket: "/nonexistent/a/i", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Group id alive but recycled (our own test group, no member mentions the
	// socket): must not be reported, and must never be signalled.
	recycled, err := writeRecord(regDir, Record{OwnerPID: dead, OwnerStart: 1, PGID: syscall.Getpgrp(), Socket: "/nonexistent/b/i", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	// Owner still running (this process): not an orphan, record kept.
	live, err := writeRecord(regDir, Record{OwnerPID: os.Getpid(), OwnerStart: selfStart, PGID: syscall.Getpgrp(), Socket: "/nonexistent/c/i", Generation: 3})
	if err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(regDir, recordPrefix+"0-9"+recordSuffix)
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none", orphans)
	}
	for _, p := range []string{stale, recycled, corrupt} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("record %s should have been removed", filepath.Base(p))
		}
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("live owner's record removed: %v", err)
	}
}
