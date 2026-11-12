//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type unixPTY struct {
	*os.File
}

func (p *unixPTY) Resize(cols, rows, pw, ph int) error {
	return pty.Setsize(p.File, winsize(cols, rows, pw, ph))
}

func winsize(cols, rows, pw, ph int) *pty.Winsize {
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows), X: uint16(pw), Y: uint16(ph)}
}

func startPTY(cmd *exec.Cmd, cols, rows, pw, ph int) (ptyDevice, error) {
	f, err := pty.StartWithSize(cmd, winsize(cols, rows, pw, ph))
	if err != nil {
		return nil, err
	}
	return &unixPTY{File: f}, nil
}

// askTUIToQuit sends what a closing terminal would: SIGHUP.
func askTUIToQuit(cmd *exec.Cmd, _ ptyDevice) {
	_ = cmd.Process.Signal(syscall.SIGHUP)
}
