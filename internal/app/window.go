package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// windowBinary is the single-window host, installed next to ttv-tui on Linux.
const windowBinary = "ttv-tui-window"

// installedWindow returns the window executable next to this one, or "". Only the
// sibling counts: a window found on PATH could belong to another version.
func installedWindow() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	path := filepath.Join(filepath.Dir(exe), windowBinary)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return ""
	}
	return path
}

// openWindow starts the window and reports whether it took over. Anything short of
// a confirmed window leaves the app in the terminal, as it always worked.
func openWindow(opts Options) bool {
	if opts.Terminal || os.Getenv(videoHostEnv) != "" ||
		runtime.GOOS != "linux" || os.Getenv("WAYLAND_DISPLAY") == "" {
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
	cmd.Stderr = os.Stderr
	detachWindow(cmd)
	if cmd.Start() != nil {
		return false
	}
	_ = cmd.Process.Release()
	return true
}
