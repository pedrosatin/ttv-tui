package player

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// testEndpoint is a pipe name nobody listens on yet.
func testEndpoint(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("ttv-tui-test-%d-%s-%s", os.Getpid(), name, rand.Text()[:pipeSuffixLen])
}

// createPipeInstance makes one server end the way mpv's ipc-win.c does: a plain
// CreateNamedPipe in byte mode, overlapped. winio.ListenPipe would be shorter,
// but it is not what the dial meets in production, and under Wine its dial
// succeeds while Accept never returns (its first handle counts on a pipe opened
// without read or write access staying disconnected).
func createPipeInstance(endpoint string, flags, maxInstances uint32) (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(pipePath(endpoint))
	if err != nil {
		return 0, err
	}
	return windows.CreateNamedPipe(path,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED|flags,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		maxInstances, 4096, 4096, 0, nil)
}

// pipeListener is the fake mpv's side of a named pipe.
type pipeListener struct {
	endpoint string
	closed   windows.Handle // manual-reset event, set by Close

	mu        sync.Mutex
	next      windows.Handle // the instance waiting for a client
	accepting bool           // Accept owns next and closes it if it fails
	done      bool
}

// listenEndpoint is the fake mpv's side of the endpoint. The pipe exists, and a
// client can connect, from the moment it returns.
func listenEndpoint(endpoint string) (net.Listener, error) {
	h, err := createPipeInstance(endpoint, windows.FILE_FLAG_FIRST_PIPE_INSTANCE, windows.PIPE_UNLIMITED_INSTANCES)
	if err != nil {
		return nil, fmt.Errorf("create pipe %s: %w", endpoint, err)
	}
	closed, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return &pipeListener{endpoint: endpoint, closed: closed, next: h}, nil
}

func (l *pipeListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	h := l.next
	done := l.done
	l.accepting = !done
	l.mu.Unlock()
	if done {
		return nil, net.ErrClosed
	}
	if err := l.waitForClient(h); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}

	// The next instance exists before this one is handed over, so the pipe never
	// disappears between two clients.
	next, err := createPipeInstance(l.endpoint, 0, windows.PIPE_UNLIMITED_INSTANCES)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("create pipe instance: %w", err)
	}
	l.mu.Lock()
	l.next = next
	l.accepting = false
	if l.done {
		_ = windows.CloseHandle(next)
	}
	l.mu.Unlock()

	file, err := winio.NewOpenFile(h)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return &pipeServerConn{file: file}, nil
}

// waitForClient blocks until a client connects to h or the listener closes.
func (l *pipeListener) waitForClient(h windows.Handle) error {
	connected, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(connected) }()

	overlapped := windows.Overlapped{HEvent: connected}
	err = windows.ConnectNamedPipe(h, &overlapped)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return fmt.Errorf("ConnectNamedPipe: %w", err)
	}
	event, err := windows.WaitForMultipleObjects([]windows.Handle{connected, l.closed}, false, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait for client: %w", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		// The kernel still holds overlapped: let it go before this frame does.
		_ = windows.CancelIoEx(h, &overlapped)
		var n uint32
		_ = windows.GetOverlappedResult(h, &overlapped, &n, true)
		return net.ErrClosed
	}
	return nil
}

func (l *pipeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil
	}
	l.done = true
	_ = windows.SetEvent(l.closed)
	if !l.accepting {
		_ = windows.CloseHandle(l.next)
	}
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return pipeAddr(l.endpoint)
}

type pipeAddr string

func (pipeAddr) Network() string  { return "pipe" }
func (a pipeAddr) String() string { return string(a) }

// pipeServerConn adapts the server end to net.Conn. The fakes only read, write
// and close; the embedded nil Conn is the rest of the interface, unused.
type pipeServerConn struct {
	net.Conn
	file io.ReadWriteCloser
}

func (c *pipeServerConn) Read(b []byte) (int, error)  { return c.file.Read(b) }
func (c *pipeServerConn) Write(b []byte) (int, error) { return c.file.Write(b) }
func (c *pipeServerConn) Close() error                { return c.file.Close() }

// endpointPresent dials: a pipe has no file to look at.
func endpointPresent(endpoint string) bool {
	conn, err := dialIPCOnce(endpoint, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func endpointGone(endpoint string) bool {
	return !endpointPresent(endpoint)
}
