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

func TestValidIPCForRole(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	runtimeDir := filepath.Join(base, "ttv-tui")
	mainDir := mustPrivateDir(t, filepath.Join(runtimeDir, "1-main"))
	previewBase := mustPrivateDir(t, filepath.Join(runtimeDir, "preview"))
	previewDir := mustPrivateDir(t, filepath.Join(previewBase, "1-preview"))

	mainIPC := filepath.Join(mainDir, "i")
	previewIPC := filepath.Join(previewDir, "i")

	if !validIPCForRole(mainIPC, roleMain) {
		t.Fatal("main valid IPC rejected")
	}
	if !validIPCForRole(previewIPC, rolePreview) {
		t.Fatal("preview valid IPC rejected")
	}
	if validIPCForRole(previewIPC, roleMain) {
		t.Fatal("preview IPC accepted for main role")
	}
	if validIPCForRole(mainIPC, rolePreview) {
		t.Fatal("main IPC accepted for preview role")
	}

	// Público ou symlink em preview/ deve ser rejeitado.
	if err := os.Chmod(previewBase, 0o755); err != nil {
		t.Fatal(err)
	}
	if validIPCForRole(previewIPC, rolePreview) {
		t.Fatal("public preview base accepted")
	}
	if err := os.Chmod(previewBase, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := mustPrivateDir(t, filepath.Join(t.TempDir(), "1-escape"))
	escaped := filepath.Join(previewBase, "2-escape")
	if err := os.Symlink(outside, escaped); err != nil {
		t.Fatal(err)
	}
	if validIPCForRole(filepath.Join(escaped, "i"), rolePreview) {
		t.Fatal("symlinked preview session accepted")
	}

	// Fallback em temp privado exige cruzamento caminho↔papel.
	fbMainDir, err := os.MkdirTemp(os.TempDir(), "ttv-tui-2-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(fbMainDir) }()
	if err := os.Chmod(fbMainDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fbMainIPC := filepath.Join(fbMainDir, "i")
	if !validIPCForRole(fbMainIPC, roleMain) {
		t.Fatalf("main fallback IPC rejected: %s", fbMainIPC)
	}
	if validIPCForRole(fbMainIPC, rolePreview) {
		t.Fatalf("main fallback IPC accepted for preview role: %s", fbMainIPC)
	}

	fbPreviewDir, err := os.MkdirTemp(os.TempDir(), "ttv-tui-preview-2-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(fbPreviewDir) }()
	if err := os.Chmod(fbPreviewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fbPreviewIPC := filepath.Join(fbPreviewDir, "i")
	if !validIPCForRole(fbPreviewIPC, rolePreview) {
		t.Fatalf("preview fallback IPC rejected: %s", fbPreviewIPC)
	}
	if validIPCForRole(fbPreviewIPC, roleMain) {
		t.Fatalf("preview fallback IPC accepted for main role: %s", fbPreviewIPC)
	}

	// Nome de fallback dentro do runtimeDir não é aceito fora do temp privado.
	squatPreview := mustPrivateDir(t, filepath.Join(runtimeDir, "ttv-tui-preview-2-999"))
	if validIPCForRole(filepath.Join(squatPreview, "i"), roleMain) || validIPCForRole(filepath.Join(squatPreview, "i"), rolePreview) {
		t.Fatalf("fallback name inside runtimeDir accepted: %s", squatPreview)
	}
}

func TestReadStartRoleAndRejection(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", base)
	mainDir := mustPrivateDir(t, filepath.Join(base, "ttv-tui", "1-main"))
	previewDir := mustPrivateDir(t, filepath.Join(base, "ttv-tui", "preview", "1-preview"))

	sockDir, err := os.MkdirTemp("/tmp", "ctlrole-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(sockDir) }()

	runReq := func(payload string) (ctlMsg, int, error) {
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
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		if _, _, err := c.WriteMsgUnix([]byte(payload), syscall.UnixRights(int(w.Fd())), nil); err != nil {
			t.Fatal(err)
		}
		_ = w.Close()
		msg, readErr := readStart(server)
		if readErr == nil {
			closeFD(msg.fd)
		}
		if err := syscall.SetNonblock(int(r.Fd()), true); err != nil {
			t.Fatal(err)
		}
		var b [1]byte
		n, err := syscall.Read(int(r.Fd()), b[:])
		if err != nil {
			t.Fatalf("read pipe check: %v", err)
		}
		return msg, n, readErr
	}

	// Papel vazio assume principal.
	msg, n, err := runReq(`{"ipc":"` + filepath.Join(mainDir, "i") + `"}`)
	if err != nil || msg.role != roleMain || n != 0 {
		t.Fatalf("empty role: msg=%+v n=%d err=%v", msg, n, err)
	}

	// Papel preview usa diretório preview/.
	msg, n, err = runReq(`{"ipc":"` + filepath.Join(previewDir, "i") + `","role":"preview"}`)
	if err != nil || msg.role != rolePreview || n != 0 {
		t.Fatalf("preview role: msg=%+v n=%d err=%v", msg, n, err)
	}

	// Papel inválido falha e fecha o fd recebido.
	if _, n, err := runReq(`{"ipc":"` + filepath.Join(mainDir, "i") + `","role":"other"}`); err == nil || n != 0 {
		t.Fatalf("invalid role: err=%v n=%d", err, n)
	}

	// Prévia apontando para diretório principal falha e fecha o fd recebido.
	if _, n, err := runReq(`{"ipc":"` + filepath.Join(mainDir, "i") + `","role":"preview"}`); err == nil || n != 0 {
		t.Fatalf("cross-role IPC: err=%v n=%d", err, n)
	}
}
