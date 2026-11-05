package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "ttv-tui"

// pathEnv is everything the path resolvers read from the machine. Production
// code uses osPathEnv; tests pass a fake, so no test needs the operator's home,
// the real environment or the disk.
type pathEnv struct {
	goos    string
	getenv  func(string) string
	home    func() (string, error)
	tempDir func() string
}

func osPathEnv() pathEnv {
	return pathEnv{
		goos:    runtime.GOOS,
		getenv:  os.Getenv,
		home:    os.UserHomeDir,
		tempDir: os.TempDir,
	}
}

func (e pathEnv) userHome() (string, error) {
	home, err := e.home()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return home, nil
}

// homeJoin is a path under the home directory.
func (e pathEnv) homeJoin(elem ...string) (string, error) {
	home, err := e.userHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, elem...)...), nil
}

// envOrHome is the Windows known folder held in an environment variable
// (APPDATA, LOCALAPPDATA), or its default location under the home directory
// when the variable is empty.
func (e pathEnv) envOrHome(name string, fallback ...string) (string, error) {
	if v := e.getenv(name); v != "" {
		return v, nil
	}
	return e.homeJoin(fallback...)
}

// localAppData is the Windows per-user folder that stays on this machine.
func (e pathEnv) localAppData() (string, error) {
	return e.envOrHome("LOCALAPPDATA", "AppData", "Local")
}

// configBase is the directory that holds each application's config directory:
// $XDG_CONFIG_HOME on every OS, otherwise the OS convention. XDG goes first
// everywhere because that is how tests keep away from the operator's real
// config (and its access_token).
func (e pathEnv) configBase() (string, error) {
	if xdg := e.getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, nil
	}
	switch e.goos {
	case "darwin":
		return e.homeJoin("Library", "Application Support")
	case "windows":
		return e.envOrHome("APPDATA", "AppData", "Roaming")
	default:
		return e.homeJoin(".config")
	}
}

// configDir is the application's config directory.
func (e pathEnv) configDir() (string, error) {
	base, err := e.configBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName), nil
}

// twtDir is twitch-tui's config directory, beside ours.
func (e pathEnv) twtDir() (string, error) {
	base, err := e.configBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "twt"), nil
}

// stateDir is the application's state directory. $XDG_STATE_HOME wins on every
// OS; macOS has no state directory, so it lives under Application Support.
func (e pathEnv) stateDir() (string, error) {
	if xdg := e.getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirName), nil
	}
	switch e.goos {
	case "darwin":
		return e.homeJoin("Library", "Application Support", appDirName, "state")
	case "windows":
		local, err := e.localAppData()
		if err != nil {
			return "", err
		}
		return filepath.Join(local, appDirName), nil
	default:
		return e.homeJoin(".local", "state", appDirName)
	}
}

// runtimeDir is the base directory for the player's per-session directories.
// It never fails: when nothing better is known it is the OS temp directory.
func (e pathEnv) runtimeDir() string {
	if xdg := e.getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, appDirName)
	}
	switch e.goos {
	case "darwin":
		// TMPDIR is a per-user directory on macOS, unlike /tmp.
		if tmp := e.getenv("TMPDIR"); tmp != "" {
			return filepath.Join(tmp, appDirName)
		}
	case "windows":
		if local, err := e.localAppData(); err == nil {
			return filepath.Join(local, appDirName, "run")
		}
	}
	return filepath.Join(e.tempDir(), appDirName)
}

// RuntimeDir is the directory under which the player creates its sessions:
// $XDG_RUNTIME_DIR/ttv-tui on every OS when set, otherwise $TMPDIR/ttv-tui on
// macOS, %LOCALAPPDATA%\ttv-tui\run on Windows and <os.TempDir()>/ttv-tui
// elsewhere.
func RuntimeDir() string {
	return osPathEnv().runtimeDir()
}

// LocalAppData is %LOCALAPPDATA%, or AppData\Local under the home directory when
// the variable is empty. Unlike the directories above it ignores XDG: the player
// asks for it to learn which folder Windows keeps private to this user, wherever
// the sessions happen to live.
func LocalAppData() (string, error) {
	return osPathEnv().localAppData()
}
