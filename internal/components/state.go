package components

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

const (
	stateFileName = "state.json"
	// stateLockStaleAge bounds how long a crashed writer can hold up state
	// changes. State writes take milliseconds, so no heartbeat is needed.
	stateLockStaleAge = 30 * time.Second
)

// UpdateState is the persistent update bookkeeping of the managed streamlink,
// kept beside the version directories. It never holds credentials.
type UpdateState struct {
	// LastCheck is when the latest release was last fetched, zero when never.
	LastCheck time.Time `json:"last_check,omitzero"`
	// Trial is the version installed by an update that has not played a stream
	// yet. Only a version on trial can be undone: an older, long-working version
	// failing later is not the update's fault.
	Trial string `json:"trial,omitempty"`
	// Bad lists versions that failed on trial and are never downloaded or used
	// again.
	Bad []string `json:"bad,omitempty"`
}

// IsBad reports whether version was marked bad.
func (s UpdateState) IsBad(version string) bool {
	return slices.Contains(s.Bad, version)
}

func stateFilePath(dataDir string) string {
	return filepath.Join(StreamlinkBaseDir(dataDir), stateFileName)
}

// LoadUpdateState reads the update state without taking any lock or writing
// anything. A missing or unreadable file yields the zero state: the bookkeeping
// is advisory and must never keep the player from starting.
func LoadUpdateState(dataDir string) UpdateState {
	data, err := os.ReadFile(stateFilePath(dataDir))
	if err != nil {
		return UpdateState{}
	}
	var st UpdateState
	if err := json.Unmarshal(data, &st); err != nil {
		return UpdateState{}
	}
	return st
}

// saveUpdateState replaces the state file atomically. Callers hold the state
// lock, so no other writer is active.
func saveUpdateState(dataDir string, st UpdateState) error {
	baseDir := StreamlinkBaseDir(dataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return fmt.Errorf("create components directory: %w", err)
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode update state: %w", err)
	}
	// The state lock is held, so any leftover is from a writer that died. The
	// name avoids the prefixes cleanStaleTemps removes under the other lock,
	// which could delete a live writer's file.
	if stale, err := filepath.Glob(filepath.Join(baseDir, ".statetmp-*")); err == nil {
		for _, f := range stale {
			_ = os.Remove(f)
		}
	}
	tmp, err := os.CreateTemp(baseDir, ".statetmp-*")
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("write state file: %w", err)
	}
	if err := renameDir(tmpName, stateFilePath(dataDir)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}

// updateState runs fn under the state lock on the current state and saves the
// result when fn reports a change. The lock is short-lived and independent of
// the download lock, so recording a rollback never waits for a download.
func (d *Downloader) updateState(ctx context.Context, fn func(*UpdateState) (changed bool, err error)) error {
	baseDir := StreamlinkBaseDir(d.DataDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return fmt.Errorf("create components directory: %w", err)
	}
	unlock, err := filelock.LockWithContext(ctx, filepath.Join(baseDir, ".state"), stateLockStaleAge)
	if err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	defer func() { _ = unlock() }()

	st := LoadUpdateState(d.DataDir)
	changed, err := fn(&st)
	if err != nil || !changed {
		return err
	}
	return saveUpdateState(d.DataDir, st)
}
