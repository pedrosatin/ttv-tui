package player

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// processSignalWait covers the moment a process has left its job's count and its
// handle is not signaled yet: Stop can return in between.
const processSignalWait = 200 * time.Millisecond

// processAlive asks about one process, through the same OpenProcess check the
// production code uses. It sees neither descendants nor a PID reused by a stranger.
func processAlive(pid int) bool {
	deadline := time.Now().Add(processSignalWait)
	for recordedGroup(pid).alive() {
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(groupPoll)
	}
	return false
}

func readRecord(t *testing.T, path string) Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	return rec
}

// residue is what a session left on disk: its directory with the input.conf, and
// a record that says so, written by ownerPID about the process playerPID.
func residue(t *testing.T, ownerPID, playerPID int) (record, dir string) {
	t.Helper()
	base := t.TempDir()
	dir = filepath.Join(base, "1-session")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, inputConfName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := writeRecord(filepath.Join(base, "players"), Record{
		OwnerPID:   ownerPID,
		PGID:       playerPID,
		Socket:     pipeName(dir, ownerPID) + "-ABCDEFGH",
		Dir:        dir,
		Generation: 1,
	})
	if err != nil {
		t.Fatalf("writeRecord: %v", err)
	}
	return record, dir
}

func assertNoOrphans(t *testing.T, record string) {
	t.Helper()
	orphans, err := FindOrphans(filepath.Dir(record))
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v: Windows has none to report", orphans)
	}
}

func assertResidueKept(t *testing.T, record, dir string) {
	t.Helper()
	for _, path := range []string{record, filepath.Join(dir, inputConfName)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s removed without proof the session is over: %v", filepath.Base(path), err)
		}
	}
}

func TestFindOrphansClearsResidueOfFinishedSession(t *testing.T) {
	dead := deadPid(t)
	record, dir := residue(t, dead, dead)

	assertNoOrphans(t, record)

	for _, path := range []string{record, dir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", path, err)
		}
	}
}

// A running owner may be a second instance's live session, or a stranger that
// reused the PID. Nothing tells them apart here, so the record stays.
func TestFindOrphansKeepsSessionOfRunningOwner(t *testing.T) {
	cmd, _ := startIdle(t)
	record, dir := residue(t, cmd.Process.Pid, deadPid(t))

	assertNoOrphans(t, record)

	assertResidueKept(t, record, dir)
}

func TestFindOrphansKeepsSessionOfThisProcess(t *testing.T) {
	record, dir := residue(t, os.Getpid(), deadPid(t))

	assertNoOrphans(t, record)

	assertResidueKept(t, record, dir)
}

// The owner is gone but the recorded PID runs: a stranger that reused it, most
// likely, or the process the job missed. Neither can be told, and neither gets
// signalled.
func TestFindOrphansKeepsSessionWhosePlayerPIDRuns(t *testing.T) {
	cmd, _ := startIdle(t)
	record, dir := residue(t, deadPid(t), cmd.Process.Pid)

	assertNoOrphans(t, record)

	assertResidueKept(t, record, dir)
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("a process the registry cannot judge was killed")
	}
}

func TestOSProcStartTimeOnlyAnswersForGonePID(t *testing.T) {
	if start, err := osProcStartTime(deadPid(t)); start != 0 || err != nil {
		t.Fatalf("osProcStartTime(dead) = %d, %v, want 0 with no error", start, err)
	}
	if _, err := osProcStartTime(os.Getpid()); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("osProcStartTime(self) = %v, want ErrUnsupported", err)
	}
}
