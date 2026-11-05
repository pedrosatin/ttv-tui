package player

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// sessionLayout is where one playback lives. The two fields are different
// things and must not be derived from each other:
//
//   - Dir is a private directory on disk. It holds the input.conf and is removed
//     as a whole with the session.
//   - Endpoint is what mpv receives in --input-ipc-server and what the TUI dials.
//     On Unix it is a socket file, which this layout puts inside Dir. On Windows
//     it is the bare name of a named pipe, which is not a file in any directory.
type sessionLayout struct {
	Dir      string
	Endpoint string
}

// maxSocketPathLen bounds a Unix socket path: sun_path holds 104 bytes on
// macOS and 108 on Linux. Windows named pipes have no such limit.
const maxSocketPathLen = 100

// socketFileName is the Unix socket's name inside the session directory.
const socketFileName = "i"

// pipeName is the named-pipe endpoint for a session directory. It is only the
// name, with no separators: streamlink splits --player-args with shlex and mpv
// prefixes `\\.\pipe\` itself, so a name containing slashes would be taken as a
// path. The directory's base name is already unique per session.
func pipeName(dir string, pid int) string {
	return "ttv-tui-" + strconv.Itoa(pid) + "-" + filepath.Base(dir)
}

var (
	fallbackMu      sync.RWMutex
	fallbackBaseDir = "/tmp"
)

func getFallbackBaseDir() string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	return fallbackBaseDir
}

func setFallbackBaseDirForTest(dir string) func() {
	fallbackMu.Lock()
	orig := fallbackBaseDir
	fallbackBaseDir = dir
	fallbackMu.Unlock()
	return func() {
		fallbackMu.Lock()
		fallbackBaseDir = orig
		fallbackMu.Unlock()
	}
}

func checkDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// createFallbackDir makes the /tmp fallback directory, or reuses one left by an
// earlier run with the same pid and generation, dropping the stale endpoint.
func createFallbackDir(dir, endpoint string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		if os.IsExist(err) {
			if err := checkDirWritable(dir); err != nil {
				return fmt.Errorf("fallback directory %q not writable: %w", dir, err)
			}
			removeEndpoint(endpoint)
			return nil
		}
		return fmt.Errorf("create fallback directory %q: %w", dir, err)
	}
	if err := checkDirWritable(dir); err != nil {
		_ = os.RemoveAll(dir)
		return fmt.Errorf("fallback directory %q not writable: %w", dir, err)
	}
	return nil
}

// prepareSession creates the session's private directory and works out its IPC
// endpoint. It prefers a directory under runtimeDir and falls back to
// <fallback base>/ttv-tui-<pid>-<gen> when that one is unusable (too long an
// endpoint, not writable). The directory is the same on every OS; the endpoint
// comes from endpointInDir, which is per OS.
func prepareSession(runtimeDir string, gen uint64) (sessionLayout, error) {
	var primaryErr error
	if runtimeDir != "" {
		l, err := prepareIn(runtimeDir, gen)
		if err == nil {
			return l, nil
		}
		primaryErr = err
	}

	fallbackDir := filepath.Join(getFallbackBaseDir(), fmt.Sprintf("ttv-tui-%d-%d", os.Getpid(), gen))
	fallbackEndpoint, err := endpointInDir(fallbackDir)
	if err != nil {
		if primaryErr != nil {
			return sessionLayout{}, fmt.Errorf("player: fallback %w (primary %q: %v)", err, runtimeDir, primaryErr)
		}
		return sessionLayout{}, fmt.Errorf("player: fallback %w", err)
	}

	if err := createFallbackDir(fallbackDir, fallbackEndpoint); err != nil {
		if primaryErr != nil {
			return sessionLayout{}, fmt.Errorf("player: %w (primary %q: %v)", err, runtimeDir, primaryErr)
		}
		return sessionLayout{}, fmt.Errorf("player: %w", err)
	}

	return sessionLayout{Dir: fallbackDir, Endpoint: fallbackEndpoint}, nil
}

// prepareIn builds the preferred layout under runtimeDir. Any error means "use
// the fallback"; nothing is left on disk when it fails.
func prepareIn(runtimeDir string, gen uint64) (sessionLayout, error) {
	if err := checkRuntimeDirFits(runtimeDir, gen); err != nil {
		return sessionLayout{}, err
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return sessionLayout{}, err
	}
	dir, err := os.MkdirTemp(runtimeDir, fmt.Sprintf("%d-", gen))
	if err != nil {
		return sessionLayout{}, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return sessionLayout{}, err
	}
	endpoint, err := endpointInDir(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return sessionLayout{}, err
	}
	if err := checkDirWritable(dir); err != nil {
		_ = os.RemoveAll(dir)
		return sessionLayout{}, fmt.Errorf("directory %q not writable: %w", dir, err)
	}
	return sessionLayout{Dir: dir, Endpoint: endpoint}, nil
}
