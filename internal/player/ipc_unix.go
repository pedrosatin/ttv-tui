//go:build unix

package player

import (
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

// dialIPCOnce makes one attempt to connect to the socket file at endpoint.
func dialIPCOnce(endpoint string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, timeout)
}

// platformIPCPeerGone reports peer-gone errors that only exist on this OS.
// Unix has none beyond ECONNRESET, EPIPE and ECONNABORTED, which
// isIPCPeerGone already covers.
func platformIPCPeerGone(error) bool {
	return false
}

// ipcEndpointRefused reports whether a failed dial hit an endpoint that exists
// but refused the connection (a socket file with no listener behind it).
func ipcEndpointRefused(endpoint string, err error) bool {
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return false
	}
	_, statErr := os.Lstat(endpoint)
	return statErr == nil
}
