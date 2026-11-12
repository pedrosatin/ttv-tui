package filelock

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"
)

// DefaultStaleAge is the duration after which an unreleased lock is considered stale.
const DefaultStaleAge = time.Minute

// Lock attempts to acquire the lock at path + ".lock" with up to 10 attempts
// and 1 minute stale age, matching the default store locking behavior.
func Lock(path string) (func() error, error) {
	return LockWithRetries(path, 10, DefaultStaleAge)
}

// LockWithRetries attempts to acquire the lock at path + ".lock" up to maxAttempts times.
func LockWithRetries(path string, maxAttempts int, staleAge time.Duration) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	lockPath := path + ".lock"
	for attempt := 0; attempt < maxAttempts; attempt++ {
		release, err := tryAcquire(lockPath, staleAge)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if attempt < maxAttempts-1 {
			time.Sleep(time.Duration(10+attempt*10+rand.IntN(6)) * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("acquire lock %s: %w", lockPath, os.ErrExist)
}

// LockWithContext continuously tries to acquire the lock until ctx is canceled or the lock is acquired.
func LockWithContext(ctx context.Context, path string, staleAge time.Duration) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	lockPath := path + ".lock"
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		release, err := tryAcquire(lockPath, staleAge)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		attempt++
		sleepDuration := time.Duration(min(50+attempt*15, 300)+rand.IntN(20)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleepDuration):
		}
	}
}

func tryAcquire(lockPath string, staleAge time.Duration) (func() error, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		info, statErr := f.Stat()
		closeErr := f.Close()
		if err := errors.Join(statErr, closeErr); err != nil {
			return nil, errors.Join(fmt.Errorf("close lock: %w", err), os.Remove(lockPath))
		}
		return func() error {
			current, err := os.Stat(lockPath)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("stat lock: %w", err)
			}
			// Do not remove a successor's lock after this owner's lease expired.
			if !os.SameFile(info, current) {
				return nil
			}
			if err := os.Remove(lockPath); err != nil {
				return fmt.Errorf("release lock: %w", err)
			}
			return nil
		}, nil
	}

	if !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create lock: %w", err)
	}

	info, err := os.Stat(lockPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat lock: %w", err)
	}
	if err == nil && time.Since(info.ModTime()) > staleAge {
		// Recheck identity so competing stale-lock recovery does not knowingly
		// remove a different owner's newly acquired lock.
		current, statErr := os.Stat(lockPath)
		if statErr == nil && os.SameFile(info, current) {
			if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("remove stale lock: %w", err)
			}
		}
	}
	return nil, os.ErrExist
}
