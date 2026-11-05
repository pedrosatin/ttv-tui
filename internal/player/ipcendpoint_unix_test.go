//go:build unix

package player

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// testEndpoint is an endpoint nobody listens on yet.
func testEndpoint(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// listenEndpoint is the fake mpv's side of the endpoint.
func listenEndpoint(endpoint string) (net.Listener, error) {
	return net.Listen("unix", endpoint)
}

func endpointPresent(endpoint string) bool {
	_, err := os.Stat(endpoint)
	return err == nil
}

func endpointGone(endpoint string) bool {
	_, err := os.Stat(endpoint)
	return os.IsNotExist(err)
}
