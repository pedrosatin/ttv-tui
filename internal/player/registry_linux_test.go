package player

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// processAlive treats zombies as dead: they no longer run or play audio.
func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	fields := procStatFields(pid)
	return len(fields) > 0 && fields[0] != "Z"
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
	if processAlive(childPid) || recordedGroup(pgid).alive() {
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

// The same record, judged twice: preserved while its player lives, cleared with
// its files once the player is dead. A live owner's record survives both.
func TestFindOrphansPreservesLivePlayerAndClearsDeadOne(t *testing.T) {
	base := t.TempDir()
	regDir := filepath.Join(base, "players")
	sockDir := filepath.Join(base, "1-orphan")
	if err := os.Mkdir(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(sockDir, "i")
	if err := os.WriteFile(socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := startIdleGroup(t, socket)
	pgid := cmd.Process.Pid
	dead := deadPid(t)

	orphanRec, err := writeRecord(regDir, Record{OwnerPID: dead, OwnerStart: 1, PGID: pgid, Socket: socket, Dir: sockDir, Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	ownedRec, err := writeRecord(regDir, Record{OwnerPID: os.Getpid(), OwnerStart: selfStart, PGID: dead, Socket: "/nonexistent/owned/i", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}

	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].PGID != pgid {
		t.Fatalf("orphans = %+v, want the live group %d", orphans, pgid)
	}
	for _, p := range []string{orphanRec, ownedRec, socket} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed while its player lives: %v", filepath.Base(p), err)
		}
	}

	group := recordedGroup(pgid)
	if err := group.kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if !waitGroupGone(group, nil, 3*time.Second) {
		t.Fatalf("group %d still alive after kill", pgid)
	}

	orphans, err = FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none once the player is dead", orphans)
	}
	for _, p := range []string{orphanRec, sockDir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind by a dead player: %v", filepath.Base(p), err)
		}
	}
	if _, err := os.Stat(ownedRec); err != nil {
		t.Fatalf("live owner's record removed: %v", err)
	}
}
