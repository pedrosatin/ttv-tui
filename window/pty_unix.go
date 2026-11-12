//go:build !windows

package main

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixPTY struct {
	*os.File
}

func (p *unixPTY) Resize(cols, rows, pw, ph int) error {
	return pty.Setsize(p.File, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
		X:    uint16(pw),
		Y:    uint16(ph),
	})
}

func startPTY(cmd *exec.Cmd, cols, rows, pw, ph int) (ptyDevice, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
		X:    uint16(pw),
		Y:    uint16(ph),
	})
	if err != nil {
		return nil, err
	}
	return &unixPTY{File: f}, nil
}
