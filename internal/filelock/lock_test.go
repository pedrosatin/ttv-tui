package filelock

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
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

func TestTryAcquireStaleLock_SingleWinner(t *testing.T) {
	const workers = 8
	const rounds = 60

	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		lockPath := filepath.Join(dir, "state.json.lock")
		if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		staleTime := time.Now().Add(-10 * time.Minute)
		if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var winners atomic.Int32
		var winnerRelease func() error

		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rel, _, err := tryAcquire(lockPath, time.Minute)
				if err == nil {
					if winners.Add(1) == 1 {
						winnerRelease = rel
					} else {
						_ = rel()
					}
					return
				}
				if !errors.Is(err, os.ErrExist) {
					t.Errorf("round %d: erro inesperado dos perdedores: %v", round, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		if got := winners.Load(); got != 1 {
			t.Fatalf("round %d: vencedores = %d, quer 1", round, got)
		}
		if _, err := os.Stat(lockPath); err != nil {
			t.Fatalf("round %d: lock do vencedor sumiu: %v", round, err)
		}
		if err := winnerRelease(); err != nil {
			t.Fatalf("round %d: release falhou: %v", round, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("round %d: sobrou arquivo no diretório: %v", round, entries)
		}
	}
}

func TestConcurrentStaleLockRecovery(t *testing.T) {
	const workers = 4
	const rounds = 25

	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		path := filepath.Join(dir, "state.json")
		lockPath := path + ".lock"
		if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
		staleTime := time.Now().Add(-10 * time.Minute)
		if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		var active atomic.Int32
		var acquired atomic.Int32

		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				unlock, err := LockWithContext(ctx, path, time.Minute)
				if err != nil {
					t.Errorf("round %d: LockWithContext: %v", round, err)
					return
				}
				if n := active.Add(1); n != 1 {
					t.Errorf("round %d: %d donos simultâneos do lock", round, n)
				}
				time.Sleep(2 * time.Millisecond)
				active.Add(-1)
				acquired.Add(1)
				if err := unlock(); err != nil {
					t.Errorf("round %d: unlock: %v", round, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		if got := acquired.Load(); got != workers {
			t.Fatalf("round %d: acquired = %d, quer %d", round, got, workers)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("round %d: sobrou arquivo no diretório: %v", round, entries)
		}
	}
}

func TestStaleLockRecovery_TwoProcesses(t *testing.T) {
	if os.Getenv("TTV_LOCK_HELPER") == "1" {
		var b [1]byte
		if _, err := os.Stdin.Read(b[:]); err != nil {
			t.Fatal(err)
		}
		_ = os.Stdin.Close()

		path := os.Getenv("TTV_LOCK_PATH")
		activePath := os.Getenv("TTV_LOCK_ACTIVE")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		unlock, err := LockWithContext(ctx, path, time.Minute)
		if err != nil {
			t.Fatalf("LockWithContext: %v", err)
		}
		f, err := os.OpenFile(activePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = unlock()
			t.Fatalf("exclusão mútua quebrada: %v", err)
		}
		_ = f.Close()
		time.Sleep(30 * time.Millisecond)
		if err := os.Remove(activePath); err != nil {
			_ = unlock()
			t.Fatalf("remover sentinela: %v", err)
		}
		if err := unlock(); err != nil {
			t.Fatalf("unlock: %v", err)
		}
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	lockPath := path + ".lock"
	activePath := path + ".active"
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	type child struct {
		cmd   *exec.Cmd
		start *os.File
	}
	children := make([]child, 0, 2)
	for range 2 {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestStaleLockRecovery_TwoProcesses$")
		cmd.Env = append(os.Environ(),
			"TTV_LOCK_HELPER=1",
			"TTV_LOCK_PATH="+path,
			"TTV_LOCK_ACTIVE="+activePath,
		)
		cmd.Stdin = r
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		children = append(children, child{cmd, w})
	}
	for _, c := range children {
		_, err := c.start.Write([]byte{1})
		_ = c.start.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range children {
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("processo filho falhou: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("sobrou arquivo no diretório: %v", entries)
	}
}
