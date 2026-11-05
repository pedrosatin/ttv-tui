package store

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const staleLockAge = time.Minute

// Serialize local file transactions so goroutines do not exhaust the bounded
// cross-process lock retries competing with their own process.
var stateFileMu sync.Mutex

// lockState uses exclusive file creation rather than flock, which is not
// portable to Windows. Readers also lock because corrupt-file backup renames
// the state file. No lock descriptor remains open across a rename on Windows.
func lockState(path string) (func() error, error) {
	stateFileMu.Lock()
	release, err := lockStateFile(path)
	if err != nil {
		stateFileMu.Unlock()
		return nil, err
	}
	return func() error {
		defer stateFileMu.Unlock()
		return release()
	}, nil
}

func lockStateFile(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	lockPath := path + ".lock"
	for attempt := 0; attempt < 5; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			info, statErr := f.Stat()
			closeErr := f.Close()
			if err := errors.Join(statErr, closeErr); err != nil {
				return nil, errors.Join(fmt.Errorf("close state lock: %w", err), os.Remove(lockPath))
			}
			return func() error {
				current, err := os.Stat(lockPath)
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if err != nil {
					return fmt.Errorf("stat state lock: %w", err)
				}
				// Do not remove a successor's lock after this owner's lease expired.
				if !os.SameFile(info, current) {
					return nil
				}
				if err := os.Remove(lockPath); err != nil {
					return fmt.Errorf("release state lock: %w", err)
				}
				return nil
			}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create state lock: %w", err)
		}
		info, err := os.Stat(lockPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat state lock: %w", err)
		}
		if err == nil && time.Since(info.ModTime()) > staleLockAge {
			// Recheck identity so competing stale-lock recovery does not knowingly
			// remove a different owner's newly acquired lock.
			current, statErr := os.Stat(lockPath)
			if statErr == nil && os.SameFile(info, current) {
				if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("remove stale state lock: %w", err)
				}
			}
		}
		if attempt < 4 {
			// Four bounded waits total 70-90ms; jitter spreads simultaneous writers.
			time.Sleep(time.Duration(10+attempt*5+rand.IntN(6)) * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("acquire state lock %s: %w", lockPath, os.ErrExist)
}
