package filelock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	unlock, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock failed: %v", err)
	}

	// Second lock must fail while held
	_, err = LockWithRetries(path, 2, DefaultStaleAge)
	if err == nil {
		t.Fatal("second Lock should fail while first lock is held")
	}

	if err := unlock(); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	// After release, lock can be acquired again
	unlock2, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock after release failed: %v", err)
	}
	_ = unlock2()
}

func TestLockWithContextCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	unlock, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock failed: %v", err)
	}
	defer func() { _ = unlock() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = LockWithContext(ctx, path, DefaultStaleAge)
	if err == nil {
		t.Fatal("expected LockWithContext to fail with context cancellation")
	}
}

func TestStaleLockRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	lockPath := path + ".lock"

	// Create a fake stale lock file with an old modification time
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	unlock, err := LockWithRetries(path, 5, time.Minute)
	if err != nil {
		t.Fatalf("expected Lock to break stale lock, got: %v", err)
	}
	_ = unlock()
}

func TestLockHeartbeat_StaleLockTaken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	// Owner acquires lock without heartbeat renewal and abandons it
	unlock, err := LockWithHeartbeat(context.Background(), path, 150*time.Millisecond, 0, nil)
	if err != nil {
		t.Fatalf("initial lock failed: %v", err)
	}
	// Do NOT call unlock() to simulate an abandoned lock

	// Second process with context should take over the lock once staleAge (150ms) expires
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	unlock2, err := LockWithHeartbeat(ctx, path, 150*time.Millisecond, 0, nil)
	if err != nil {
		t.Fatalf("expected waiter to acquire abandoned lock after staleAge: %v", err)
	}
	defer func() { _ = unlock2() }()

	// Calling the abandoned unlock should safely do nothing because it is no longer the owner
	_ = unlock()
}

func TestLockHeartbeat_RenewsLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	// Owner acquires lock with 40ms heartbeat and 120ms staleAge
	unlock, err := LockWithHeartbeat(context.Background(), path, 120*time.Millisecond, 40*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("initial lock failed: %v", err)
	}

	// While heartbeat is active, a waiter with 250ms timeout must NOT steal the lock
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, err = LockWithHeartbeat(ctx, path, 120*time.Millisecond, 0, nil)
	if err == nil {
		_ = unlock()
		t.Fatal("waiter should NOT steal lock while heartbeat actively renews it")
	}

	// Once owner releases, waiter can acquire
	if err := unlock(); err != nil {
		t.Fatalf("unlock failed: %v", err)
	}

	unlock2, err := LockWithHeartbeat(context.Background(), path, 120*time.Millisecond, 0, nil)
	if err != nil {
		t.Fatalf("lock after release failed: %v", err)
	}
	_ = unlock2()
}

func TestLockWithHeartbeat_OnWaitingInvoked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	unlock, err := Lock(path)
	if err != nil {
		t.Fatalf("initial lock failed: %v", err)
	}
	defer func() { _ = unlock() }()

	var waitingCalled bool
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	_, _ = LockWithHeartbeat(ctx, path, DefaultStaleAge, 0, func() {
		waitingCalled = true
	})

	if !waitingCalled {
		t.Fatal("expected onWaiting callback to be invoked on contended lock")
	}
}
