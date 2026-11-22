//go:build !unix && !windows

package filelock

import "os"

func openLockExisting(path string) (*os.File, error) {
	return os.Open(path)
}

func tryLockFile(*os.File) (bool, error) { return true, nil }

func lockFile(*os.File) error { return nil }

func unlockAndClose(f *os.File) error { return f.Close() }

func isSharingViolation(error) bool { return false }
