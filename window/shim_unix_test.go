//go:build !windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestSendShimStreamCarriesRole(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	mainDir := mustPrivateDir(t, filepath.Join(base, "ttv-tui", "1-session"))
	previewDir := mustPrivateDir(t, filepath.Join(base, "ttv-tui", "preview", "1-session"))

	for _, tc := range []struct {
		role string
		ipc  string
	}{
		{roleMain, filepath.Join(mainDir, "i")},
		{rolePreview, filepath.Join(previewDir, "i")},
	} {
		t.Run(tc.role, func(t *testing.T) {
			sockDir, err := os.MkdirTemp("/tmp", "shim-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(sockDir) }()
			ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(sockDir, "ctl"), Net: "unix"})
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

			if err := sendShimStream(c, tc.ipc, tc.role); err != nil {
				t.Fatal(err)
			}
			msg, err := readStart(server)
			if err != nil {
				t.Fatal(err)
			}
			closeFD(msg.fd)
			if msg.role != tc.role || msg.ipc != tc.ipc {
				t.Fatalf("readStart = (role=%q, ipc=%q), quero (%q, %q)", msg.role, msg.ipc, tc.role, tc.ipc)
			}
		})
	}
}
