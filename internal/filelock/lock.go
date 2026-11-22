package filelock

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultStaleAge is the duration after which an unreleased lock is considered stale.
const DefaultStaleAge = time.Minute

// DeletePendingWait bounds the wait for a lock file that is being deleted (see
// isDeletePending). In CI an old lock stayed pending for more than a second,
// well past the 450-495ms of contention retries, so this wait is separate.
const DeletePendingWait = 3 * time.Second

const deletePendingPoll = 25 * time.Millisecond

var (
	lockMuMapMu sync.Mutex
	lockMuMap   = map[string]*sync.Mutex{}
	tempSeq     atomic.Uint64
)

func pathMutex(lockPath string) *sync.Mutex {
	key := filepath.Clean(lockPath)
	lockMuMapMu.Lock()
	defer lockMuMapMu.Unlock()
	mu, ok := lockMuMap[key]
	if !ok {
		mu = &sync.Mutex{}
		lockMuMap[key] = mu
	}
	return mu
}

func uniqueTempPath(lockPath, kind string) string {
	seq := tempSeq.Add(1)
	return fmt.Sprintf("%s.%s.%d.%d.%016x", lockPath, kind, os.Getpid(), seq, rand.Uint64())
}

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
	release, info, err := createLockFile(lockPath)
	if !errors.Is(err, os.ErrExist) {
		return release, info, err
	}

	info, err = os.Stat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, os.ErrExist
	}
	if err != nil {
		if isDeletePending(err) {
			return nil, nil, errDeletePending{fmt.Errorf("stat lock: %w", err)}
		}
		return nil, nil, fmt.Errorf("stat lock: %w", err)
	}
	if time.Since(info.ModTime()) > staleAge {
		return recoverStaleLock(lockPath, info, staleAge)
	}
	return nil, nil, os.ErrExist
}

func createLockFile(lockPath string) (func() error, os.FileInfo, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if isDeletePending(err) {
			return nil, nil, errDeletePending{fmt.Errorf("create lock: %w", err)}
		}
		if isSharingViolation(err) || errors.Is(err, os.ErrExist) {
			return nil, nil, os.ErrExist
		}
		return nil, nil, fmt.Errorf("create lock: %w", err)
	}

	info, statErr := f.Stat()
	closeErr := f.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("close lock: %w", err), os.Remove(lockPath))
	}
	return makeRelease(lockPath, info), info, nil
}

func makeRelease(lockPath string, info os.FileInfo) func() error {
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			mu := pathMutex(lockPath)
			mu.Lock()
			defer mu.Unlock()

			var f *os.File
			deadline := time.Now().Add(DeletePendingWait)
			for {
				var err error
				f, err = openLockExisting(lockPath)
				if err == nil {
					break
				}
				if errors.Is(err, os.ErrNotExist) {
					return
				}
				if (isSharingViolation(err) || isDeletePending(err)) && time.Now().Before(deadline) {
					time.Sleep(deletePendingPoll)
					continue
				}
				releaseErr = fmt.Errorf("open lock: %w", err)
				return
			}

			if err := lockFile(f); err != nil {
				_ = f.Close()
				releaseErr = fmt.Errorf("lock for release: %w", err)
				return
			}

			fInfo, err := f.Stat()
			if err != nil {
				_ = unlockAndClose(f)
				releaseErr = fmt.Errorf("stat lock: %w", err)
				return
			}
			current, err := os.Stat(lockPath)
			if errors.Is(err, os.ErrNotExist) {
				_ = unlockAndClose(f)
				return
			}
			if err != nil {
				_ = unlockAndClose(f)
				releaseErr = fmt.Errorf("stat lock: %w", err)
				return
			}
			// Do not remove a successor's lock after this owner's lease expired.
			if !os.SameFile(info, fInfo) || !os.SameFile(info, current) {
				_ = unlockAndClose(f)
				return
			}

			tmpPath := uniqueTempPath(lockPath, "rel")
			if err := os.Rename(lockPath, tmpPath); err != nil {
				_ = unlockAndClose(f)
				if errors.Is(err, os.ErrNotExist) {
					return
				}
				releaseErr = fmt.Errorf("release lock: %w", err)
				return
			}
			_ = unlockAndClose(f)
			if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				releaseErr = fmt.Errorf("release lock: %w", err)
			}
		})
		return releaseErr
	}
}

// recoverStaleLock serializa a recuperação do lock velho com um lock do SO no
// próprio descritor aberto e move o arquivo para um nome único antes de remover.
// Enquanto o arquivo renomeado existe, o sucessor novo recebe um inode próprio
// e nenhum processo atrasado remove o lock recém-criado.
func recoverStaleLock(lockPath string, info os.FileInfo, staleAge time.Duration) (func() error, os.FileInfo, error) {
	mu := pathMutex(lockPath)
	if !mu.TryLock() {
		return nil, nil, os.ErrExist
	}
	defer mu.Unlock()

	f, err := openLockExisting(lockPath)
	if errors.Is(err, os.ErrNotExist) || isSharingViolation(err) {
		return nil, nil, os.ErrExist
	}
	if isDeletePending(err) {
		return nil, nil, errDeletePending{fmt.Errorf("open stale lock: %w", err)}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open stale lock: %w", err)
	}

	locked, err := tryLockFile(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("lock stale lock: %w", err)
	}
	if !locked {
		_ = f.Close()
		return nil, nil, os.ErrExist
	}

	fInfo, err := f.Stat()
	if err != nil {
		_ = unlockAndClose(f)
		return nil, nil, fmt.Errorf("stat stale lock: %w", err)
	}
	if time.Since(fInfo.ModTime()) <= staleAge {
		_ = unlockAndClose(f)
		return nil, nil, os.ErrExist
	}

	current, err := os.Stat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		_ = unlockAndClose(f)
		return nil, nil, os.ErrExist
	}
	if isDeletePending(err) {
		_ = unlockAndClose(f)
		return nil, nil, errDeletePending{fmt.Errorf("stat lock: %w", err)}
	}
	if err != nil {
		_ = unlockAndClose(f)
		return nil, nil, fmt.Errorf("stat lock: %w", err)
	}
	if !os.SameFile(fInfo, current) || !os.SameFile(info, current) || time.Since(current.ModTime()) <= staleAge {
		_ = unlockAndClose(f)
		return nil, nil, os.ErrExist
	}

	stalePath := uniqueTempPath(lockPath, "stale")
	if err := os.Rename(lockPath, stalePath); err != nil {
		_ = unlockAndClose(f)
		if errors.Is(err, os.ErrNotExist) || isSharingViolation(err) {
			return nil, nil, os.ErrExist
		}
		if isDeletePending(err) {
			return nil, nil, errDeletePending{fmt.Errorf("rename stale lock: %w", err)}
		}
		return nil, nil, fmt.Errorf("rename stale lock: %w", err)
	}

	release, newInfo, createErr := createLockFile(lockPath)
	_ = unlockAndClose(f)
	_ = os.Remove(stalePath)
	return release, newInfo, createErr
}
