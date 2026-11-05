package app

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// zombieStat is SZOMB in kp_proc.p_stat.
const zombieStat = 5

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// openPTY is the macOS form of the Linux one: grant, unlock and ask the kernel
// for the slave's name, which has no number to build a path from.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	// NewFile before SetNonblock keeps the master off kqueue, which is unreliable
	// for devices on macOS. Reads then return EAGAIN instead of blocking, so Close
	// still ends the reader, and that loop retries on EAGAIN.
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = master.Close()
		t.Fatalf("SetNonblock: %v", err)
	}
	if err := ioctl(fd, syscall.TIOCPTYGRANT, nil); err != nil {
		_ = master.Close()
		t.Fatalf("TIOCPTYGRANT: %v", err)
	}
	if err := ioctl(fd, syscall.TIOCPTYUNLK, nil); err != nil {
		_ = master.Close()
		t.Fatalf("TIOCPTYUNLK: %v", err)
	}
	var name [128]byte
	if err := ioctl(fd, syscall.TIOCPTYGNAME, unsafe.Pointer(&name[0])); err != nil {
		_ = master.Close()
		t.Fatalf("TIOCPTYGNAME: %v", err)
	}
	end := bytes.IndexByte(name[:], 0)
	if end < 0 {
		_ = master.Close()
		t.Fatal("TIOCPTYGNAME: name is not terminated")
	}
	slave, err = os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		t.Fatalf("open slave: %v", err)
	}
	setSize(t, master, 30, 100)
	return master, slave
}

// alive treats zombies as dead, like the Linux helper: they no longer run or play
// audio.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false
	}
	return proc.Proc.P_stat != zombieStat
}
