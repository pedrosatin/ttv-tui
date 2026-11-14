//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const maxStreamClientRefusals = 16

// streamPipeListener owns one available instance even before Accept. Accept
// creates its successor before returning a client, including a refused one.
// acceptMu serializes Accept; mu protects the handle and cancellation by Close.
type streamPipeListener struct {
	acceptMu  sync.Mutex
	mu        sync.Mutex
	name      *uint16
	sd        *windows.SECURITY_DESCRIPTOR
	next      windows.Handle
	accepting bool
	closed    bool
}

// listenStreamPipe grants only the process user's SID full access. The
// protected DACL prevents inherited grants to other users or broad groups.
func listenStreamPipe(name string) (*streamPipeListener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("SID do usuário: %w", err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	l := &streamPipeListener{name: path, sd: sd}
	l.next, err = l.createInstance(windows.FILE_FLAG_FIRST_PIPE_INSTANCE)
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (l *streamPipeListener) createInstance(flags uint32) (windows.Handle, error) {
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: l.sd}
	return windows.CreateNamedPipe(l.name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|flags,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES, 4096, 4096, 0, &sa)
}

func (l *streamPipeListener) Accept() (*streamPipeConn, error) {
	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, net.ErrClosed
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		l.mu.Unlock()
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(event) }()
	h := l.next
	ov := windows.Overlapped{HEvent: event}
	// Issue the operation while holding mu, so Close cannot miss cancellation.
	err = windows.ConnectNamedPipe(h, &ov)
	l.accepting = true
	l.mu.Unlock()
	if errors.Is(err, windows.ERROR_IO_PENDING) {
		var n uint32
		// Always reap the operation before releasing ov or its event, even on Close.
		err = windows.GetOverlappedResult(h, &ov, &n, true)
	} else if errors.Is(err, windows.ERROR_PIPE_CONNECTED) || errors.Is(err, windows.ERROR_NO_DATA) {
		err = nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.accepting = false
	if l.closed {
		err = net.ErrClosed
	}
	if err == nil {
		// Keep a connectable instance available before a refused client is closed.
		l.next, err = l.createInstance(0)
	}
	if err != nil {
		_ = windows.CloseHandle(h)
		l.closed = true
		return nil, err
	}
	file, err := winio.NewOpenFile(h)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return &streamPipeConn{ReadWriteCloser: file, handle: h}, nil
}

func (l *streamPipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.accepting {
		// Accept retains ownership until the canceled overlapped operation completes.
		err := windows.CancelIoEx(l.next, nil)
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return err
	}
	return windows.CloseHandle(l.next)
}

type streamPipeConn struct {
	io.ReadWriteCloser
	handle windows.Handle
}

// acceptStreamClient discards connections from other processes before any
// stream bytes are written. Uninspectable clients count toward the same refusal
// limit, so a disconnected intruder cannot terminate the session or wait forever.
func acceptStreamClient(l *streamPipeListener, expected uint32) (io.ReadWriteCloser, error) {
	if expected == 0 {
		return nil, fmt.Errorf("PID da janela ausente")
	}
	for refused := 0; refused < maxStreamClientRefusals; refused++ {
		c, err := l.Accept()
		if err != nil {
			return nil, err
		}
		var pid uint32
		err = windows.GetNamedPipeClientProcessId(c.handle, &pid)
		if err == nil && pid == expected {
			return c, nil
		}
		_ = c.Close()
	}
	return nil, fmt.Errorf("limite de %d clientes recusados no pipe", maxStreamClientRefusals)
}
