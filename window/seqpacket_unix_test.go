//go:build !windows

package main

import (
	"io"
	"os"
	"syscall"
	"testing"
)

// seqpacketPair imita o PTY com um socket SOCK_SEQPACKET, que entrega cada
// escrita numa leitura própria.
func seqpacketPair(t *testing.T) (ptyDevice, io.WriteCloser) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, w := os.NewFile(uintptr(fds[0]), "pty"), os.NewFile(uintptr(fds[1]), "tui")
	t.Cleanup(func() { _, _ = r.Close(), w.Close() })
	return mockPTYDevice{r}, w
}
