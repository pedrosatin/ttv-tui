package store

import (
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/filelock"
)

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
	return filelock.Lock(path)
}
