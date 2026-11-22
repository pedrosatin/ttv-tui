//go:build unix

package filelock

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

func openLockExisting(path string) (*os.File, error) {
	return os.Open(path)
}

func tryLockFile(f *os.File) (bool, error) {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		runtime.KeepAlive(f)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return false, nil
		}
		return false, err
	}
}

func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		runtime.KeepAlive(f)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

func unlockAndClose(f *os.File) error {
	return f.Close()
}

func isSharingViolation(error) bool { return false }
