//go:build unix

package player

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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

// liveSession is a real process group with the socket in its argv, plus the
// record and files a session leaves on disk. Judging it asks the OS only whether
// the group has members; everything else can come from a fakeKernel.
type liveSession struct {
	regDir  string
	sockDir string
	socket  string
	pgid    int
}

func startLiveSession(t *testing.T) liveSession {
	t.Helper()
	base := t.TempDir()
	s := liveSession{regDir: filepath.Join(base, "players"), sockDir: filepath.Join(base, "1-first")}
	if err := os.Mkdir(s.sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	s.socket = filepath.Join(s.sockDir, socketFileName)
	if err := os.WriteFile(s.socket, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.pgid = startIdleGroup(t, s.socket).Process.Pid
	return s
}

func (s liveSession) write(t *testing.T, ownerPID int, ownerStart uint64) string {
	t.Helper()
	path, err := writeRecord(s.regDir, Record{
		OwnerPID:   ownerPID,
		OwnerStart: ownerStart,
		PGID:       s.pgid,
		Socket:     s.socket,
		Dir:        s.sockDir,
		Generation: 1,
	})
	if err != nil {
		t.Fatalf("writeRecord: %v", err)
	}
	return path
}

func (s liveSession) assertKept(t *testing.T, record string) {
	t.Helper()
	for _, p := range []string{record, s.socket, s.sockDir} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed from a session that may be alive: %v", filepath.Base(p), err)
		}
	}
	if !processAlive(s.pgid) {
		t.Fatalf("player %d was signalled", s.pgid)
	}
}

func (s liveSession) assertCleared(t *testing.T, record string) {
	t.Helper()
	for _, p := range []string{record, s.sockDir} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", filepath.Base(p), err)
		}
	}
}

// useKernel makes the registry judge with the Darwin code path, on any OS.
func useKernel(t *testing.T, k fakeKernel) {
	t.Helper()
	origStart, origArg := procStartTime, groupHasArg
	t.Cleanup(func() { procStartTime, groupHasArg = origStart, origArg })
	procStartTime = k.procs().startTime
	groupHasArg = k.procs().groupHasArg
}

func findNoOrphans(t *testing.T, regDir string) {
	t.Helper()
	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v, want none", orphans)
	}
}

const (
	testOwnerPID   = 4242
	testOwnerStart = 1_700_000_000_000_123
)

// The first instance's process, seen by the second, with real start times.
func TestSecondInstanceKeepsLiveSession(t *testing.T) {
	s := startLiveSession(t)
	owner := startIdleGroup(t, "first-instance").Process.Pid
	start, err := procStartTime(owner)
	if err != nil || start == 0 {
		t.Fatalf("procStartTime(%d) = %d, %v", owner, start, err)
	}
	record := s.write(t, owner, start)

	findNoOrphans(t, s.regDir)
	s.assertKept(t, record)
}

func TestSecondInstanceKeepsLiveSessionOnDarwinPath(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		testOwnerPID: {pgid: testOwnerPID, start: testOwnerStart, argv: []string{"ttv-tui"}},
		s.pgid:       {pgid: s.pgid, start: 2, argv: []string{"streamlink"}},
		s.pgid + 1:   {pgid: s.pgid, start: 3, argv: []string{"mpv", "--input-ipc-server=" + s.socket}},
	}})
	record := s.write(t, testOwnerPID, testOwnerStart)

	findNoOrphans(t, s.regDir)
	s.assertKept(t, record)
}

func TestOrphanIsDetectedOnDarwinPath(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		s.pgid: {pgid: s.pgid, start: 2, argv: []string{"mpv", "--input-ipc-server=" + s.socket}},
	}})
	s.write(t, testOwnerPID, testOwnerStart)

	orphans, err := FindOrphans(s.regDir)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].PGID != s.pgid {
		t.Fatalf("orphans = %+v, want the group %d", orphans, s.pgid)
	}
}

func TestDeadPlayerRecordIsClearedOnDarwinPath(t *testing.T) {
	s := startLiveSession(t)
	s.pgid = deadPid(t)
	useKernel(t, fakeKernel{})
	record := s.write(t, testOwnerPID, testOwnerStart)

	findNoOrphans(t, s.regDir)
	s.assertCleared(t, record)
}

// A group left with nothing but zombies has no player, even while it still
// answers to a signal.
func TestZombieGroupIsClearedOnDarwinPath(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		s.pgid: {pgid: s.pgid, start: 2, stat: kinfoStatZombie},
	}})
	record := s.write(t, testOwnerPID, testOwnerStart)

	findNoOrphans(t, s.regDir)
	s.assertCleared(t, record)
}

// Both PIDs in the record were reused: the owner's has another start time and the
// group belongs to a stranger. The record goes, the stranger is left alone.
func TestRecycledPIDIsClearedAndNeverSignalled(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		testOwnerPID: {pgid: testOwnerPID, start: testOwnerStart + 1, argv: []string{"other"}},
		s.pgid:       {pgid: s.pgid, start: 2, argv: []string{"stranger"}},
	}})
	record := s.write(t, testOwnerPID, testOwnerStart)

	findNoOrphans(t, s.regDir)
	s.assertCleared(t, record)
	if !processAlive(s.pgid) {
		t.Fatalf("recycled group %d was signalled", s.pgid)
	}
}

// The group was recycled after FindOrphans reported it, while the user decided.
func TestTerminateNeverSignalsRecycledGroup(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		s.pgid: {pgid: s.pgid, start: 2, argv: []string{"stranger"}},
	}})
	record := s.write(t, testOwnerPID, testOwnerStart)
	orphan := Orphan{Record: Record{PGID: s.pgid, Socket: s.socket, Dir: s.sockDir}, path: record}

	if err := orphan.Terminate(50 * time.Millisecond); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	if !processAlive(s.pgid) {
		t.Fatalf("recycled group %d was signalled", s.pgid)
	}
	s.assertCleared(t, record)
}

func TestWhatCannotBeJudgedIsPreservedOnDarwinPath(t *testing.T) {
	cases := []struct {
		name       string
		ownerStart uint64
		kernel     func(s liveSession) fakeKernel
	}{
		{"owner unreadable", testOwnerStart, func(liveSession) fakeKernel {
			return fakeKernel{procErr: syscall.EPERM}
		}},
		{"group listing refused", testOwnerStart, func(liveSession) fakeKernel {
			return fakeKernel{groupErr: syscall.EPERM}
		}},
		{"live member refused", testOwnerStart, func(s liveSession) fakeKernel {
			return fakeKernel{table: map[int]fakeProc{
				s.pgid: {pgid: s.pgid, start: 2, argsErr: syscall.EINVAL},
			}}
		}},
		{"owner running, record without start time", 0, func(s liveSession) fakeKernel {
			return fakeKernel{table: map[int]fakeProc{
				testOwnerPID: {pgid: testOwnerPID, start: testOwnerStart, argv: []string{"ttv-tui"}},
				s.pgid:       {pgid: s.pgid, start: 2, argv: []string{"mpv", "--input-ipc-server=" + s.socket}},
			}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := startLiveSession(t)
			useKernel(t, c.kernel(s))
			record := s.write(t, testOwnerPID, c.ownerStart)

			findNoOrphans(t, s.regDir)
			s.assertKept(t, record)
		})
	}
}

func TestTerminateRefusesRefusedMemberOnDarwinPath(t *testing.T) {
	s := startLiveSession(t)
	useKernel(t, fakeKernel{table: map[int]fakeProc{
		s.pgid: {pgid: s.pgid, start: 2, argsErr: syscall.EINVAL},
	}})
	record := s.write(t, testOwnerPID, testOwnerStart)
	orphan := Orphan{Record: Record{PGID: s.pgid, Socket: s.socket, Dir: s.sockDir}, path: record}

	if err := orphan.Terminate(50 * time.Millisecond); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("Terminate = %v, want EINVAL", err)
	}
	s.assertKept(t, record)
}

func TestOwnerWithoutStartTimeIsJudgedOnlyWhenGone(t *testing.T) {
	useKernel(t, fakeKernel{})
	if owned, err := ownerAlive(Record{OwnerPID: testOwnerPID}); owned || err != nil {
		t.Fatalf("ownerAlive(gone) = %v, %v, want false with no error", owned, err)
	}
}
