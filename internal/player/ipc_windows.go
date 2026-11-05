//go:build windows

package player

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// pipePrefix is what mpv puts before the name it gets in --input-ipc-server.
const pipePrefix = `\\.\pipe\`

// pipePath is the full Win32 path of the pipe an endpoint names.
func pipePath(endpoint string) string {
	return pipePrefix + endpoint
}

// dialIPCOnce makes one attempt to connect to the named pipe. A pipe that does
// not exist yet fails at once with ERROR_FILE_NOT_FOUND, and dialIPCUntil tries
// again; one whose instances are all taken is retried by winio until timeout.
func dialIPCOnce(endpoint string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return winio.DialPipeContext(ctx, pipePath(endpoint))
}

// platformIPCPeerGone reports peer-gone errors that only exist on Windows. The
// syscall.ECONNRESET family isIPCPeerGone checks is made up by Go on Windows and
// never comes from the OS.
//
// A read needs little here: winio already turns ERROR_BROKEN_PIPE into io.EOF.
// A write gets the Win32 code raw: ERROR_NO_DATA while the far end is closing,
// ERROR_BROKEN_PIPE or ERROR_PIPE_NOT_CONNECTED after. winio.ErrFileClosed is
// this end's handle closed under a pending call, the pipe's net.ErrClosed. The
// WSA codes are the same three events on a socket.
func platformIPCPeerGone(err error) bool {
	if errors.Is(err, winio.ErrFileClosed) {
		return true
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.ERROR_BROKEN_PIPE,
		windows.ERROR_NO_DATA,
		windows.ERROR_PIPE_NOT_CONNECTED,
		windows.WSAECONNRESET,
		windows.WSAECONNABORTED,
		windows.WSAESHUTDOWN:
		return true
	}
	return false
}

// ipcEndpointRefused is always false on Windows: the endpoint is a named pipe,
// not a file, so there is no "present but nobody listens" state to tell apart
// from a pipe that is merely busy or not created yet. A pipe that never shows up
// is waited for until the deadline.
func ipcEndpointRefused(string, error) bool {
	return false
}
