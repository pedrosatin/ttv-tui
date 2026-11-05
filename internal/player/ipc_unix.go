//go:build unix

package player

// platformIPCPeerGone reports peer-gone errors that only exist on this OS.
// Unix has none beyond ECONNRESET, EPIPE and ECONNABORTED, which
// isIPCPeerGone already covers.
func platformIPCPeerGone(error) bool {
	return false
}
