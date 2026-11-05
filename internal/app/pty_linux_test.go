package app

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var n uint32
	if err := ioctl(uintptr(fd), syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("TIOCGPTN: %v", err)
	}
	var unlock int32
	if err := ioctl(uintptr(fd), syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("TIOCSPTLCK: %v", err)
	}
	// Non-blocking fd: os.File uses the poller, so Close interrupts the reader.
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	setSize(t, master, 30, 100)
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Fatalf("open slave: %v", err)
	}
	return master, slave
}

// alive treats zombies as dead: they no longer run or play audio.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(data, ')')
	return i < 0 || i+2 >= len(data) || data[i+2] != 'Z'
}
