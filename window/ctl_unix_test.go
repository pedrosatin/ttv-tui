//go:build !windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadStartClosesExtraFD(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	dir := filepath.Join(base, "ttv-tui", "1-session")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "ctl")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	c, err := net.DialUnix("unix", nil, ln.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	server, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	// Sending the write end twice must leave only one received reference alive.
	if _, _, err := c.WriteMsgUnix([]byte(`{"ipc":"`+filepath.Join(dir, "i")+`"}`), syscall.UnixRights(int(w.Fd()), int(w.Fd())), nil); err != nil {
		t.Fatal(err)
	}
	msg, err := readStart(server)
	if err != nil {
		t.Fatal(err)
	}
	closeFD(msg.fd)
	_ = w.Close()
	if err := syscall.SetNonblock(int(r.Fd()), true); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if n, err := syscall.Read(int(r.Fd()), b[:]); n != 0 || err != nil {
		t.Fatalf("extra fd still open: %d %v", n, err)
	}
}

func TestIPCRejectsPublicAndLinkedRuntime(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	runtimeDir := filepath.Join(base, "ttv-tui")
	dir := filepath.Join(runtimeDir, "1-session")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ipc := filepath.Join(dir, "i")
	if !validIPC(ipc) {
		t.Fatal("private session rejected")
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if validIPC(ipc) {
		t.Fatal("public session accepted")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	if validIPC(ipc) {
		t.Fatal("symlink session accepted")
	}
}

func TestReadStartRejectsAndClosesFD(t *testing.T) {
	for _, data := range []string{`{"ipc":"/outside/i"}`, `{`} {
		t.Run(data, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_RUNTIME_DIR", base)
			ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(base, "ctl"), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ln.Close() }()
			c, err := net.DialUnix("unix", nil, ln.Addr().(*net.UnixAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			server, err := ln.AcceptUnix()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			defer func() { _ = w.Close() }()
			if _, _, err := c.WriteMsgUnix([]byte(data), syscall.UnixRights(int(w.Fd())), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := readStart(server); err == nil {
				t.Fatal("invalid request accepted")
			}
			_ = w.Close()
			if err := syscall.SetNonblock(int(r.Fd()), true); err != nil {
				t.Fatal(err)
			}
			var b [1]byte
			if n, err := syscall.Read(int(r.Fd()), b[:]); n != 0 || err != nil {
				t.Fatalf("received fd still open: %d %v", n, err)
			}
		})
	}
}

func TestIPCRelativeRuntime(t *testing.T) {
	cwd, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cwd) }()
	t.Chdir(cwd)
	for _, runtimeDir := range []string{"rt", ""} {
		t.Run(runtimeDir, func(t *testing.T) {
			t.Setenv("TMPDIR", "tmp")
			t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
			base := runtimeDir
			if base == "" {
				base = os.TempDir()
			}
			base, err := filepath.Abs(filepath.Join(base, "ttv-tui"))
			if err != nil {
				t.Fatal(err)
			}
			dir, err := os.MkdirTemp(mustPrivateDir(t, base), "1-")
			if err != nil {
				t.Fatal(err)
			}
			// Match prepareIn: absolute runtime, MkdirTemp session, endpoint i.
			if !validIPC(filepath.Join(dir, "i")) {
				t.Fatal("relative runtime rejected")
			}
			outside := mustPrivateDir(t, filepath.Join(cwd, "outside", "1-session"))
			if validIPC(filepath.Join(outside, "i")) {
				t.Fatal("outside runtime accepted")
			}
		})
	}
}

func mustPrivateDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestIPCFallback(t *testing.T) {
	cwd, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(cwd) }()
	t.Chdir(cwd)
	for _, temp := range []string{t.TempDir(), "tmp"} {
		t.Run(temp, func(t *testing.T) {
			t.Setenv("TMPDIR", temp)
			t.Setenv("XDG_RUNTIME_DIR", "rt")
			base, err := filepath.Abs(os.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			mustPrivateDir(t, base)
			for _, parent := range []string{base, "/tmp"} {
				// Match fallbackBase and prepareFallback, including MkdirTemp's decimal suffix.
				dir, err := os.MkdirTemp(parent, "ttv-tui-1-")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.RemoveAll(dir) }()
				if !validIPC(filepath.Join(dir, "i")) {
					t.Fatalf("fallback rejected: %s", dir)
				}
			}
			outside := mustPrivateDir(t, filepath.Join(cwd, "outside", "ttv-tui-1-123"))
			if validIPC(filepath.Join(outside, "i")) {
				t.Fatal("outside fallback accepted")
			}
		})
	}
}
