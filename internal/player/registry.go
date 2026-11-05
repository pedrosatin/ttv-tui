package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Record is the on-disk registration of one live playback. A session writes it
// right after spawning the player and removes it once the whole process group is
// gone, so a record left behind by a run that died abruptly (SIGKILL, crash)
// points at a player that may still be playing. On Windows the kernel ends that
// player with its owner, so such a record only marks files to clean up, and PGID
// is the PID of the spawned process: no group id exists there.
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

// What the registry asks the OS, answered in registry_<goos>.go. An error means
// "cannot tell", which is not "gone": a record is only removed on a real answer.
// They are variables so a test on any OS can take the unsupported path.
var (
	// procStartTime returns the start time of pid, or 0 when pid is gone.
	procStartTime = osProcStartTime
	// groupHasArg reports whether a live member of the group carries arg in its
	// command line.
	groupHasArg = osGroupHasArg
)

// errOwnerUnproven is the answer for a running PID whose record has no start time
// to compare: the writer could not read its own, so that PID may well be it.
var errOwnerUnproven = errors.New("player: record has no owner start time")

// selfStart is the start time of this process, used to tell a live owner from a
// recycled PID. It stays 0 where the OS cannot answer.
var selfStart = func() uint64 {
	start, _ := osProcStartTime(os.Getpid())
	return start
}()

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
// orphaned. So are records the OS cannot judge, which may belong to a live
// instance. A missing dir yields no orphans.
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
		owned, err := ownerAlive(rec)
		if err != nil || owned {
			continue
		}
		isPlayer, err := groupIsPlayer(rec.PGID, rec.Socket)
		if err != nil {
			continue
		}
		if !isPlayer {
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
// recycled process group is never signalled, and it stops without touching
// anything when the OS cannot confirm that identity.
func (o Orphan) Terminate(wait time.Duration) error {
	isPlayer, err := groupIsPlayer(o.PGID, o.Socket)
	if err != nil {
		return fmt.Errorf("player: orphan group %d: %w", o.PGID, err)
	}
	if isPlayer {
		group := recordedGroup(o.PGID)
		_ = group.terminate()
		if !waitGroupGone(group, nil, wait) {
			_ = group.kill()
			if !waitGroupGone(group, nil, groupKillWait) {
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

// removeStaleFiles deletes the endpoint, the input.conf and their private dir,
// only when the record describes the layout a session of this OS creates and the
// dir is empty after. Any other layout is left alone: nothing proves the dir is
// ours.
func removeStaleFiles(rec Record) {
	if rec.Dir == "" || !filepath.IsAbs(rec.Dir) || !recordIsOurLayout(rec) {
		return
	}
	removeEndpoint(rec.Socket)
	_ = os.Remove(filepath.Join(rec.Dir, inputConfName))
	_ = os.Remove(rec.Dir)
}

// ownerAlive reports whether the ttv-tui process that wrote rec still runs. An
// error means the OS cannot tell.
func ownerAlive(rec Record) (bool, error) {
	if rec.OwnerPID <= 0 {
		return false, nil
	}
	if rec.OwnerPID == os.Getpid() {
		return rec.OwnerStart == selfStart, nil
	}
	st, err := procStartTime(rec.OwnerPID)
	if err != nil {
		return false, err
	}
	if st == 0 {
		return false, nil
	}
	if rec.OwnerStart == 0 {
		return false, errOwnerUnproven
	}
	return st == rec.OwnerStart, nil
}

// waitGroupGone polls until leaderDone is closed (nil means "no leader to wait
// for") and the group has no members left, or until d elapses.
func waitGroupGone(group processGroup, leaderDone <-chan struct{}, d time.Duration) bool {
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
		if leaderDone == nil && !group.alive() {
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
// An error means the OS cannot tell.
func groupIsPlayer(pgid int, socket string) (bool, error) {
	if pgid <= 1 || socket == "" || !recordedGroup(pgid).alive() {
		return false, nil
	}
	return groupHasArg(pgid, socket)
}
