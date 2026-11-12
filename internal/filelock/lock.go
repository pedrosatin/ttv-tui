package filelock

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultStaleAge is the duration after which an unreleased lock is considered stale.
const DefaultStaleAge = time.Minute

// DeletePendingWait bounds the wait for a lock file that is being deleted (see
// isDeletePending). In CI an old lock stayed pending for more than a second,
// well past the 450-495ms of contention retries, so this wait is separate.
const DeletePendingWait = 3 * time.Second

const deletePendingPoll = 25 * time.Millisecond

// errDeletePending marks a lock name that is still held by a file being deleted;
// it wraps the real error so a wait that runs out reports it.
type errDeletePending struct{ err error }

func (e errDeletePending) Error() string { return e.err.Error() }
func (e errDeletePending) Unwrap() error { return e.err }

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
	var pendingUntil time.Time
	for attempt := 0; attempt < maxAttempts; {
		release, _, err := tryAcquire(lockPath, staleAge)
		if err == nil {
			return release, nil
		}
		var pending errDeletePending
		if errors.As(err, &pending) {
			// The name frees up when the last handle on the old lock closes. This
			// wait has its own deadline and does not spend the contention attempts.
			if pendingUntil.IsZero() {
				pendingUntil = time.Now().Add(DeletePendingWait)
			}
			if time.Now().After(pendingUntil) {
				return nil, fmt.Errorf("acquire lock %s: %w", lockPath, pending.err)
			}
			time.Sleep(deletePendingPoll)
			continue
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		attempt++
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt*10+rand.IntN(6)) * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("acquire lock %s: %w", lockPath, os.ErrExist)
}

// LockWithContext continuously tries to acquire the lock until ctx is canceled or the lock is acquired.
func LockWithContext(ctx context.Context, path string, staleAge time.Duration) (func() error, error) {
	return LockWithHeartbeat(ctx, path, staleAge, 0, nil)
}

// LockWithHeartbeat continuously tries to acquire the lock until ctx is canceled or the lock is acquired.
// Once acquired, if heartbeat > 0, a background goroutine touches the lock file's modification time every
// heartbeat interval until release is invoked. If onWaiting is non-nil, it is called when the first attempt
// encounters a contended lock.
func LockWithHeartbeat(ctx context.Context, path string, staleAge, heartbeat time.Duration, onWaiting func()) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	lockPath := path + ".lock"
	attempt := 0
	waited := false
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		release, info, err := tryAcquire(lockPath, staleAge)
		if err == nil {
			if heartbeat <= 0 {
				return release, nil
			}
			return startHeartbeat(lockPath, info, release, heartbeat), nil
		}
		var pending errDeletePending
		if !errors.Is(err, os.ErrExist) && !errors.As(err, &pending) {
			return nil, err
		}
		if !waited {
			waited = true
			if onWaiting != nil {
				onWaiting()
			}
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

func startHeartbeat(lockPath string, initialInfo os.FileInfo, release func() error, heartbeat time.Duration) func() error {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				current, err := os.Stat(lockPath)
				if err != nil {
					return
				}
				if !os.SameFile(initialInfo, current) {
					return
				}
				now := time.Now()
				_ = os.Chtimes(lockPath, now, now)
			}
		}
	}()

	var stopOnce sync.Once
	return func() error {
		stopOnce.Do(func() {
			close(stopCh)
			<-doneCh
		})
		return release()
	}
}

func tryAcquire(lockPath string, staleAge time.Duration) (func() error, os.FileInfo, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		info, statErr := f.Stat()
		closeErr := f.Close()
		if err := errors.Join(statErr, closeErr); err != nil {
			return nil, nil, errors.Join(fmt.Errorf("close lock: %w", err), os.Remove(lockPath))
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
		}, info, nil
	}

	if isDeletePending(err) {
		return nil, nil, errDeletePending{fmt.Errorf("create lock: %w", err)}
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, nil, fmt.Errorf("create lock: %w", err)
	}

	info, err := os.Stat(lockPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if isDeletePending(err) {
			return nil, nil, errDeletePending{fmt.Errorf("stat lock: %w", err)}
		}
		return nil, nil, fmt.Errorf("stat lock: %w", err)
	}
	if err == nil && time.Since(info.ModTime()) > staleAge {
		// Recheck identity so competing stale-lock recovery does not knowingly
		// remove a different owner's newly acquired lock.
		current, statErr := os.Stat(lockPath)
		if statErr == nil && os.SameFile(info, current) {
			if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) && !isDeletePending(err) {
				return nil, nil, fmt.Errorf("remove stale lock: %w", err)
			}
		}
	}
	return nil, nil, os.ErrExist
}
