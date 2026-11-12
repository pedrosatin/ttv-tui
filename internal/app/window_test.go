package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Wayland e X11 chegam ao probe; o erro dele vai para o log.
func TestOpenWindowReportsProbeError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o probe falso é um script de shell")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
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
	for name, env := range map[string][2]string{
		"wayland": {"wayland-test", ""},
		"x11":     {"", ":0"},
	} {
		t.Setenv("WAYLAND_DISPLAY", env[0])
		t.Setenv("DISPLAY", env[1])
		var capture bytes.Buffer
		if openWindow(Options{Log: &capture}) {
			t.Fatalf("%s: failed probe opened window", name)
		}
		if out := capture.String(); !strings.Contains(out, "injected window error") {
			t.Fatalf("%s: window error discarded; stderr = %q", name, out)
		}
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

// Sem Wayland nem X11 o probe nem roda: o console de texto fica no terminal.
func TestOpenWindowNeedsADisplayOnLinux(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("Windows e macOS deixam a decisão para o probe")
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	t.Setenv(videoHostEnv, "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(exe), windowBinary)
	marker := filepath.Join(t.TempDir(), "probe-ran")
	script := "#!/bin/sh\ntouch '" + marker + "'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if openWindow(Options{}) {
		t.Fatal("sem Wayland nem X11 o app fica no terminal")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("o probe rodou sem display")
	}
}

// No Windows a janela pode ficar numa subpasta com as DLLs, fora do PATH.
func TestInstalledWindowInSubfolderOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a subpasta com as DLLs só existe no Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(filepath.Dir(exe), windowBinary)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, windowBinary+".exe")
	if err := os.WriteFile(path, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := installedWindow(); got != path {
		t.Fatalf("installedWindow() = %q; quero %q", got, path)
	}
}

func TestFindInstalledWindowCandidates(t *testing.T) {
	// Tests Windows candidate detection (flat and subfolder) on any OS.
	t.Run("windows flat", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, windowBinary+".exe")
		if err := os.WriteFile(path, nil, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := findInstalledWindow(dir, "windows"); got != path {
			t.Fatalf("findInstalledWindow() = %q, want %q", got, path)
		}
	})

	t.Run("windows subfolder", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, windowBinary)
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(sub, windowBinary+".exe")
		if err := os.WriteFile(path, nil, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := findInstalledWindow(dir, "windows"); got != path {
			t.Fatalf("findInstalledWindow() = %q, want %q", got, path)
		}
	})
}
