package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// windowBinary is the single-window host, installed next to ttv-tui.
const windowBinary = "ttv-tui-window"

// installedWindow returns the window executable next to this one, or "". Only the
// sibling counts: a window found on PATH could belong to another version. On
// Windows it may also sit in a ttv-tui-window folder beside this binary, with
// its DLLs, so they stay off the user's PATH.
func installedWindow() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	candidates := []string{filepath.Join(dir, windowBinary)}
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(dir, windowBinary+".exe"),
			filepath.Join(dir, windowBinary, windowBinary+".exe"),
		}
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return ""
}

// openWindow starts the window and reports whether it took over. Anything short of
// a confirmed window leaves the app in the terminal, as it always worked.
func openWindow(opts Options) bool {
	if opts.Terminal || os.Getenv(videoHostEnv) != "" || !hasDisplay() {
		return false
	}
	path := installedWindow()
	if path == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, path, "--probe")
	probe.Stderr = opts.Log
	if probe.Stderr == nil {
		probe.Stderr = os.Stderr
	}
	if probe.Run() != nil {
		return false
	}
	var args []string
	if opts.Lang != "" {
		args = append(args, "--lang", opts.Lang)
	}
	cmd := exec.Command(path, args...)
	// The window runs this same ttv-tui, wherever each of them is installed.
	if exe, err := os.Executable(); err == nil {
		cmd.Env = append(os.Environ(), "TTV_WINDOW_TUI="+exe)
	}
	cmd.Stderr = os.Stderr
	detachWindow(cmd)
	if cmd.Start() != nil {
		return false
	}
	_ = cmd.Process.Release()
	return true
}

// hasDisplay skips the probe where no window can open: a Linux or BSD console
// without Wayland or X11. Windows and macOS leave the answer to the probe.
func hasDisplay() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return true
	}
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != ""
}
