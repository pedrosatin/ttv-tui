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
