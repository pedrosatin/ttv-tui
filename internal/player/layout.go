package player

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// sun_path sizes, in bytes and counting the terminating NUL, per OS.
const (
	sunPathSizeLinux  = 108
	sunPathSizeDarwin = 104
	// The other Unix systems this builds for (the BSDs) follow Darwin.
	sunPathSizeOtherUnix = 104
)

// maxSocketPathLenFor bounds a Unix socket path on goos: sun_path less its NUL.
// Zero means no limit: Windows IPC is a named pipe, not a path.
func maxSocketPathLenFor(goos string) int {
	switch goos {
	case "windows":
		return 0
	case "linux":
		return sunPathSizeLinux - 1
	case "darwin":
		return sunPathSizeDarwin - 1
	default:
		return sunPathSizeOtherUnix - 1
	}
}

// maxSocketPathLen is the bound for the running OS. Only the Unix endpoint
// compares against it.
var maxSocketPathLen = maxSocketPathLenFor(runtime.GOOS)

// pipeName is the named-pipe endpoint for a session directory. It is only the
// name, with no separators: streamlink splits --player-args with shlex and mpv
// prefixes `\\.\pipe\` itself, so a name containing slashes would be taken as a
// path. The directory's base name is unique per session; callers add what the
// fallback directory's predictable name cannot promise.
func pipeName(dir string, pid int) string {
	return "ttv-tui-" + strconv.Itoa(pid) + "-" + filepath.Base(dir)
}

// shortFallbackBaseDir is the last place a session can go. It only comes into
// play when the OS temp directory is too long for a Unix socket path; a named
// pipe has no such limit, so Windows never reaches it.
const shortFallbackBaseDir = "/tmp"

var (
	fallbackMu sync.RWMutex
	// fallbackBaseDir replaces the OS temp directory in tests; empty in production.
	fallbackBaseDir string
)

// getFallbackBaseDir is the OS temp directory, read on each use so that a
// $TMPDIR set after startup counts.
func getFallbackBaseDir() string {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	if fallbackBaseDir != "" {
		return fallbackBaseDir
	}
	return os.TempDir()
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

// createFallbackDir makes the temp fallback directory, or reuses one left by an
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
// <OS temp dir>/ttv-tui-<pid>-<gen> when that one is unusable (too long an
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

	fallbackDir, fallbackEndpoint, err := fallbackLocation(gen)
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

// fallbackLocation picks the fallback directory and its endpoint: the OS temp
// directory, or shortFallbackBaseDir when a long $TMPDIR cannot hold the socket.
// It touches nothing on disk.
func fallbackLocation(gen uint64) (dir, endpoint string, err error) {
	name := fmt.Sprintf("ttv-tui-%d-%d", os.Getpid(), gen)
	dir = filepath.Join(getFallbackBaseDir(), name)
	endpoint, err = endpointInDir(dir)
	if err == nil {
		return dir, endpoint, nil
	}
	short := filepath.Join(shortFallbackBaseDir, name)
	if endpoint, shortErr := endpointInDir(short); shortErr == nil {
		return short, endpoint, nil
	}
	return "", "", err
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
