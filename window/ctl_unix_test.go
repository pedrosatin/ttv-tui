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
	if _, _, err := c.WriteMsgUnix([]byte(`{"ipc":"/outside/i"}`), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := readStart(server); err == nil {
		t.Fatal("outside IPC message accepted")
	}
	if validIPC(filepath.Join(base, "outside", "i")) {
		t.Fatal("outside runtime accepted")
	}
	if !validIPC(filepath.Join(dir, "i")) {
		t.Fatal("private runtime rejected")
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
