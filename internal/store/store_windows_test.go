//go:build windows

package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

// markDeletePending leaves the lock file in "delete pending" and returns the open
// handle that keeps it there: deleted without POSIX semantics, the name stays
// until the last handle closes, as when an antivirus or indexer holds the old
// lock. It skips the test where the volume frees the name at once.
func markDeletePending(t *testing.T, path16 *uint16) windows.Handle {
	t.Helper()
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	h, err := windows.CreateFile(path16, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		share, nil, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	deleteFile := byte(1)
	if err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &deleteFile, 1); err != nil {
		_ = windows.CloseHandle(h)
		t.Skipf("volume does not accept FileDispositionInfo: %v", err)
	}
	probe, err := windows.CreateFile(path16, windows.GENERIC_READ|windows.GENERIC_WRITE,
		share, nil, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		_ = windows.CloseHandle(probe)
		_ = windows.CloseHandle(h)
		t.Skip("volume freed the name at once; no delete pending to test")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		_ = windows.CloseHandle(h)
		t.Fatalf("CREATE_NEW on a delete-pending lock = %v, want ERROR_ACCESS_DENIED", err)
	}
	return h
}

func TestStore_WaitsForDeletePendingLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lock := path + ".lock"

	path16, err := windows.UTF16PtrFromString(lock)
	if err != nil {
		t.Fatal(err)
	}

	h := markDeletePending(t, path16)

	var mu sync.Mutex
	closed := false
	closeHandle := func() {
		mu.Lock()
		defer mu.Unlock()
		if !closed {
			closed = true
			_ = windows.CloseHandle(h)
		}
	}
	t.Cleanup(closeHandle)

	// CI saw the old lock pending for more than a second, past the 450-495ms of
	// contention retries; the save has to outlast that.
	time.AfterFunc(1200*time.Millisecond, closeHandle)

	if err := SaveTo(path, State{Favorites: []Channel{{Login: "another"}}}); err != nil {
		t.Fatalf("save with delete-pending lock: %v", err)
	}

	st, err := LoadFrom(path)
	if err != nil || len(st.Favorites) != 1 || st.Favorites[0].Login != "another" {
		t.Fatalf("save after delete-pending contention: %+v, %v", st, err)
	}
}

func TestStore_DeletePendingLockReturnsRealError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lock := path + ".lock"

	path16, err := windows.UTF16PtrFromString(lock)
	if err != nil {
		t.Fatal(err)
	}

	h := markDeletePending(t, path16)
	t.Cleanup(func() {
		_ = windows.CloseHandle(h)
	})

	started := time.Now()
	err = SaveTo(path, State{})
	if err == nil {
		t.Fatal("expected lock error")
	}
	// A lock that stays pending gives up after filelock.DeletePendingWait with the real error.
	if elapsed := time.Since(started); elapsed < filelock.DeletePendingWait-100*time.Millisecond || elapsed > filelock.DeletePendingWait+2*time.Second {
		t.Fatalf("lock wait = %v, want about %v", elapsed, filelock.DeletePendingWait)
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("expected ERROR_ACCESS_DENIED, got: %v", err)
	}
}
