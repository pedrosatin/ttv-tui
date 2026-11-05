//go:build unix

package player

import (
	"fmt"
	"os"
	"path/filepath"
)

// socketFileName is the Unix socket's name inside the session directory.
const socketFileName = "i"

// checkRuntimeDirFits refuses a runtime directory that cannot hold the session
// socket, before anything is created: runtimeDir + "/" + "<gen>-" + the "i"
// socket is the shortest path the layout can produce.
func checkRuntimeDirFits(runtimeDir string, gen uint64) error {
	minLen := len(runtimeDir) + 1 + len(fmt.Sprintf("%d-", gen)) + 2
	if minLen > maxSocketPathLen {
		return fmt.Errorf("socket path exceeds %d bytes (%d)", maxSocketPathLen, minLen)
	}
	return nil
}

// endpointInDir is the Unix endpoint: a socket file inside the session
// directory. It touches nothing on disk.
func endpointInDir(dir string) (string, error) {
	socket := filepath.Join(dir, socketFileName)
	if len(socket) > maxSocketPathLen {
		return "", fmt.Errorf("socket path %q too long (%d > %d bytes)", socket, len(socket), maxSocketPathLen)
	}
	return socket, nil
}

// recordIsOurLayout reports whether rec has the socket file inside its directory.
func recordIsOurLayout(rec Record) bool {
	return rec.Socket == filepath.Join(rec.Dir, socketFileName)
}

// removeEndpoint deletes the socket file an endpoint names, if there is one.
func removeEndpoint(endpoint string) {
	if endpoint != "" {
		_ = os.Remove(endpoint)
	}
}
