//go:build windows

package player

// platformIPCPeerGone reports peer-gone errors that only exist on Windows.
//
// STUB: a closed named pipe does not surface as ECONNRESET or EPIPE. It shows
// up as the Win32 broken-pipe family and, for a socket, as a WSA error, and the
// exact values depend on how the pipe is dialed. Until v1.8 ticket 08 adds the
// pipe dial and can observe them, this answers "cannot tell" (false), so a
// closed pipe is read as an ordinary read error rather than the player ending.
func platformIPCPeerGone(error) bool {
	return false
}

// ipcEndpointRefused is always false on Windows: the endpoint is a named pipe,
// not a file, so there is no "present but nobody listens" state to tell apart
// from a pipe that is merely busy or not created yet.
func ipcEndpointRefused(string, error) bool {
	return false
}
