package player

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Record is the on-disk registration of one live playback. A session writes it
// right after spawning the player and removes it once the whole process group is
// gone, so a record left behind by a run that died abruptly (SIGKILL, crash)
// points at a player that may still be playing.
type Record struct {
	OwnerPID   int       `json:"owner_pid"`
	OwnerStart uint64    `json:"owner_start"`
	PGID       int       `json:"pgid"`
	Socket     string    `json:"socket"`
	Dir        string    `json:"dir"`
	Login      string    `json:"login"`
	Generation uint64    `json:"generation"`
	StartedAt  time.Time `json:"started_at"`
}

// Orphan is a recorded player whose owning ttv-tui process is gone while its
// process group is still alive and still identifies as that player.
type Orphan struct {
	Record
	path string
}

const (
	recordPrefix = "player-"
	recordSuffix = ".json"
	// groupKillWait bounds how long we wait for a SIGKILLed group to be reaped.
	groupKillWait = 2 * time.Second
	groupPoll     = 20 * time.Millisecond
)

// selfStart is the start time of this process, used to tell a live owner from a
// recycled PID.
var selfStart = procStartTime(os.Getpid())

func recordPath(dir string, ownerPID int, gen uint64) string {
	return filepath.Join(dir, fmt.Sprintf("%s%d-%d%s", recordPrefix, ownerPID, gen, recordSuffix))
}

func writeRecord(dir string, rec Record) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("player: create registry dir: %w", err)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("player: encode record: %w", err)
	}
	path := recordPath(dir, rec.OwnerPID, rec.Generation)
	tmp, err := os.CreateTemp(dir, ".record-*")
	if err != nil {
		return "", fmt.Errorf("player: create record: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("player: write record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("player: close record: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("player: commit record: %w", err)
	}
	return path, nil
}

// FindOrphans scans the registry dir for players left alive by a previous run.
// Records whose player is already gone are removed silently. Records owned by a
// ttv-tui process that is still running are left untouched: that player is not
// orphaned. A missing dir yields no orphans.
func FindOrphans(dir string) ([]Orphan, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("player: read registry: %w", err)
	}
	var orphans []Orphan
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, recordPrefix) || !strings.HasSuffix(name, recordSuffix) {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec Record
		if err := json.Unmarshal(data, &rec); err != nil || rec.PGID <= 1 || rec.Socket == "" {
			_ = os.Remove(path)
			continue
		}
		if ownerAlive(rec) {
			continue
		}
		if !groupIsPlayer(rec.PGID, rec.Socket) {
			removeStaleFiles(rec)
			_ = os.Remove(path)
			continue
		}
		orphans = append(orphans, Orphan{Record: rec, path: path})
	}
	return orphans, nil
}

// Terminate stops the orphaned process group (SIGTERM, then SIGKILL after wait)
// and removes its socket and record. It re-checks the group identity first so a
// recycled process group is never signalled.
func (o Orphan) Terminate(wait time.Duration) error {
	if groupIsPlayer(o.PGID, o.Socket) {
		_ = syscall.Kill(-o.PGID, syscall.SIGTERM)
		if !waitGroupGone(o.PGID, nil, wait) {
			_ = syscall.Kill(-o.PGID, syscall.SIGKILL)
			if !waitGroupGone(o.PGID, nil, groupKillWait) {
				return fmt.Errorf("player: orphan group %d survived SIGKILL", o.PGID)
			}
		}
	}
	removeStaleFiles(o.Record)
	if o.path != "" {
		_ = os.Remove(o.path)
	}
	return nil
}

// removeStaleFiles deletes the socket and its private dir, only when the record
// describes the layout the session created (dir/i) and the dir is empty after.
func removeStaleFiles(rec Record) {
	if rec.Dir == "" || !filepath.IsAbs(rec.Dir) || rec.Socket != filepath.Join(rec.Dir, "i") {
		return
	}
	_ = os.Remove(rec.Socket)
	_ = os.Remove(rec.Dir)
}

func ownerAlive(rec Record) bool {
	if rec.OwnerPID <= 0 {
		return false
	}
	if rec.OwnerPID == os.Getpid() {
		return rec.OwnerStart == selfStart
	}
	st := procStartTime(rec.OwnerPID)
	return st != 0 && st == rec.OwnerStart
}

// groupAlive reports whether any process is still a member of pgid.
func groupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// waitGroupGone polls until leaderDone is closed (nil means "no leader to wait
// for") and the process group has no members left, or until d elapses.
func waitGroupGone(pgid int, leaderDone <-chan struct{}, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(groupPoll)
	defer tick.Stop()
	for {
		if leaderDone != nil {
			select {
			case <-leaderDone:
				leaderDone = nil
			default:
			}
		}
		if leaderDone == nil && !groupAlive(pgid) {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-leaderDone:
			leaderDone = nil
		case <-tick.C:
		}
	}
}

// groupIsPlayer reports whether a live member of pgid carries the recorded IPC
// socket path in its command line. The socket lives in a random private dir, so
// a match proves the group is the player we spawned and not a recycled group id.
func groupIsPlayer(pgid int, socket string) bool {
	if pgid <= 1 || socket == "" || !groupAlive(pgid) {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	needle := []byte(socket)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fields := procStatFields(pid)
		if len(fields) < 3 || fields[0] == "Z" {
			continue
		}
		if pgrp, err := strconv.Atoi(fields[2]); err != nil || pgrp != pgid {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if bytes.Contains(cmdline, needle) {
			return true
		}
	}
	return false
}

// procStatFields returns /proc/<pid>/stat fields starting at field 3 (state),
// skipping the parenthesised comm which may contain spaces.
func procStatFields(pid int) []string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil
	}
	i := bytes.LastIndexByte(data, ')')
	if i < 0 || i+2 > len(data) {
		return nil
	}
	return strings.Fields(string(data[i+2:]))
}

// procStartTime returns field 22 (starttime, clock ticks since boot), or 0.
func procStartTime(pid int) uint64 {
	fields := procStatFields(pid)
	if len(fields) < 20 {
		return 0
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return v
}
