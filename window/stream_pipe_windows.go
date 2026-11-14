//go:build windows

package main

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listenStreamPipe grants only the process user's SID full access. The
// protected DACL prevents inherited grants to other users or broad groups.
func listenStreamPipe(name string) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("SID do usuário: %w", err)
	}
	return winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: "O:" + user.User.Sid.String() + "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
	})
}

// acceptStreamClient discards connections from other processes before any
// stream bytes are written. Failure to inspect a client also denies access.
func acceptStreamClient(l net.Listener, expected uint32) (net.Conn, error) {
	if expected == 0 {
		return nil, fmt.Errorf("PID da janela ausente")
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return nil, err
		}
		f, ok := c.(interface{ Fd() uintptr })
		if !ok {
			_ = c.Close()
			return nil, fmt.Errorf("pipe sem handle")
		}
		var pid uint32
		err = windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid)
		if err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("PID do cliente do pipe: %w", err)
		}
		if pid == expected {
			return c, nil
		}
		_ = c.Close()
	}
}
