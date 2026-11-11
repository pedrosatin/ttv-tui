package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenWindowReportsProbeError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("single window requires Linux")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(videoHostEnv, "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(exe), windowBinary)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	_, err = f.WriteString("#!/bin/sh\necho 'injected window error' >&2\nexit 1\n")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	capture, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = capture.Close() }()
	os.Stderr = capture
	defer func() { os.Stderr = old }()
	if openWindow(Options{}) {
		t.Fatal("failed probe opened window")
	}
	out, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "injected window error") {
		t.Fatalf("window error discarded; stderr = %q", out)
	}
}

func TestOpenWindowStaysInTheTerminalWhenAsked(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(videoHostEnv, "")
	for name, opts := range map[string]Options{
		"--terminal":   {Terminal: true},
		"sem a janela": {}, // o binário de teste não tem ttv-tui-window ao lado
	} {
		if openWindow(opts) {
			t.Fatalf("%s: abriu a janela", name)
		}
	}
}

func TestOpenWindowNeverLoopsInsideTheWindow(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv(videoHostEnv, "/tmp/qualquer.sock")
	if openWindow(Options{}) {
		t.Fatal("dentro da janela o TUI não pode abrir outra")
	}
}

func TestOpenWindowNeedsWayland(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv(videoHostEnv, "")
	if openWindow(Options{}) {
		t.Fatal("sem Wayland o app fica no terminal")
	}
}
