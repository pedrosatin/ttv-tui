// Package components manages external binary dependencies (such as streamlink)
// downloaded and maintained in a per-user data directory.
package components

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

// Origin indicates how a component binary was located.
type Origin string

const (
	// OriginConfig means the binary path was explicitly specified in the user configuration.
	OriginConfig Origin = "config"
	// OriginManaged means the binary was installed by ttv-tui in its components directory.
	OriginManaged Origin = "managed"
	// OriginPath means the binary was found on the system PATH.
	OriginPath Origin = "PATH"
)

// ErrNotFound indicates that the requested component was not found in any resolution source.
var ErrNotFound = errors.New("component not found")

// Result contains the resolved binary path and its execution environment.
type Result struct {
	Path    string
	Version string
	Origin  Origin
	Env     []string
}

// Resolver resolves component executables following the order:
// 1. Explicit config path
// 2. Managed component in data dir
// 3. System PATH
type Resolver struct {
	DataDir  string
	GOOS     string
	GOARCH   string
	LookPath func(string) (string, error)
	HasFUSE  func() bool
}

// NewResolver returns a Resolver configured with system defaults.
func NewResolver() *Resolver {
	dataDir, _ := config.DataDir()
	return &Resolver{
		DataDir:  dataDir,
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		LookPath: defaultLookPath,
		HasFUSE:  DefaultHasFUSE,
	}
}

func defaultLookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// DefaultHasFUSE reports whether the Linux FUSE device /dev/fuse is available.
func DefaultHasFUSE() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/dev/fuse")
	return err == nil
}

// StreamlinkBaseDir returns the base directory where managed streamlink versions are stored:
// $XDG_DATA_HOME/ttv-tui/components/streamlink on Linux,
// %LOCALAPPDATA%\ttv-tui\components\streamlink on Windows.
func StreamlinkBaseDir(dataDir string) string {
	if dataDir == "" {
		dataDir, _ = config.DataDir()
	}
	return filepath.Join(dataDir, "components", "streamlink")
}

// StreamlinkVersionDir returns the directory for a specific streamlink version.
func StreamlinkVersionDir(dataDir, version string) string {
	return filepath.Join(StreamlinkBaseDir(dataDir), version)
}

// ResolveStreamlink resolves the streamlink binary path according to the configured precedence.
func (r *Resolver) ResolveStreamlink(configPath string) (Result, error) {
	// 1. Explicit path in config
	if configPath != "" {
		if fi, err := os.Stat(configPath); err == nil && !fi.IsDir() {
			return Result{
				Path:   configPath,
				Origin: OriginConfig,
			}, nil
		}
		return Result{}, fmt.Errorf("configured streamlink_path not found: %s", configPath)
	}

	// 2. Managed component in data directory
	if r.DataDir != "" && r.GOOS != "darwin" {
		if res, ok := r.findManagedStreamlink(); ok {
			return res, nil
		}
	}

	// 3. System PATH
	lookPath := r.LookPath
	if lookPath == nil {
		lookPath = defaultLookPath
	}
	path, err := lookPath("streamlink")
	if err == nil && path != "" {
		return Result{
			Path:   path,
			Origin: OriginPath,
		}, nil
	}
	if err != nil && errors.Is(err, exec.ErrDot) {
		return Result{}, fmt.Errorf("lookup streamlink in PATH: %w", err)
	}

	return Result{}, ErrNotFound
}

// managedInstall is one installed streamlink version with its executable.
type managedInstall struct {
	Version string
	Bin     string
}

// installedStreamlinks lists the usable managed versions, newest first. A
// version marked bad is skipped, and so is a directory without an executable.
func (r *Resolver) installedStreamlinks(bad UpdateState) []managedInstall {
	baseDir := StreamlinkBaseDir(r.DataDir)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && !bad.IsBad(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}

	// Sort versions descending (newest first) numerically by components.
	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})

	var out []managedInstall
	for _, v := range versions {
		if bin := findStreamlinkBinaryInDir(filepath.Join(baseDir, v), r.GOOS); bin != "" {
			out = append(out, managedInstall{Version: v, Bin: bin})
		}
	}
	return out
}

// ManagedVersions reports the newest managed version, the one in use, and the
// previous one kept to go back to; either is empty when absent. It only reads.
func (r *Resolver) ManagedVersions() (current, previous string) {
	installed := r.installedStreamlinks(LoadUpdateState(r.DataDir))
	if len(installed) > 0 {
		current = installed[0].Version
	}
	if len(installed) > 1 {
		previous = installed[1].Version
	}
	return current, previous
}

// findManagedStreamlink looks for installed streamlink versions in the data directory.
func (r *Resolver) findManagedStreamlink() (Result, bool) {
	installed := r.installedStreamlinks(LoadUpdateState(r.DataDir))
	if len(installed) == 0 {
		return Result{}, false
	}
	return r.managedResult(installed[0]), true
}

func (r *Resolver) managedResult(m managedInstall) Result {
	var env []string
	if r.GOOS == "linux" && isAppImage(m.Bin) {
		hasFuse := r.HasFUSE
		if hasFuse == nil {
			hasFuse = DefaultHasFUSE
		}
		if !hasFuse() {
			env = append(env, "APPIMAGE_EXTRACT_AND_RUN=1")
		}
	}
	return Result{
		Path:    m.Bin,
		Version: m.Version,
		Origin:  OriginManaged,
		Env:     env,
	}
}

// compareVersions compares two version strings component-wise.
// Returns >0 if v1 > v2, <0 if v1 < v2, and 0 if v1 == v2.
func compareVersions(v1, v2 string) int {
	parts1 := splitVersionParts(v1)
	parts2 := splitVersionParts(v2)
	maxLen := max(len(parts1), len(parts2))
	for i := 0; i < maxLen; i++ {
		if i >= len(parts1) {
			return -1
		}
		if i >= len(parts2) {
			return 1
		}
		p1, p2 := parts1[i], parts2[i]
		n1, err1 := strconv.Atoi(p1)
		n2, err2 := strconv.Atoi(p2)
		if err1 == nil && err2 == nil {
			if n1 != n2 {
				return n1 - n2
			}
			continue
		}
		if p1 != p2 {
			if p1 > p2 {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVersionParts(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '_'
	})
}

func isAppImage(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".appimage") || strings.EqualFold(filepath.Base(path), "streamlink")
}

// hasExecBit reports whether fi is executable. Windows reports no execute bits,
// so there any regular file counts; this only matters when tests lay out a
// Linux installation on a Windows host.
func hasExecBit(fi os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return fi.Mode().IsRegular()
	}
	return fi.Mode()&0o111 != 0
}

// findStreamlinkBinaryInDir searches for the streamlink executable in a version directory.
func findStreamlinkBinaryInDir(dir, goos string) string {
	if goos == "windows" {
		candidates := []string{
			filepath.Join(dir, "bin", "streamlink.exe"),
			filepath.Join(dir, "streamlink.exe"),
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				return c
			}
		}
		// Search subdirectories recursively if needed
		var found string
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.EqualFold(info.Name(), "streamlink.exe") {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		return found
	}

	candidates := []string{
		filepath.Join(dir, "squashfs-root", "AppRun"),
		filepath.Join(dir, "AppRun"),
		filepath.Join(dir, "streamlink"),
		filepath.Join(dir, "streamlink.AppImage"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && hasExecBit(fi) {
			return c
		}
	}
	// Also check any .AppImage file in dir
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".appimage") {
				p := filepath.Join(dir, e.Name())
				if fi, err := os.Stat(p); err == nil && hasExecBit(fi) {
					return p
				}
			}
		}
	}
	return ""
}
