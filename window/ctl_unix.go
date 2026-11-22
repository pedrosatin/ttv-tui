//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// closeFD closes a descriptor libmpv never took, so streamlink sees the pipe close.
func closeFD(fd int) {
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}

func parseRole(raw string) (string, error) {
	switch raw {
	case "", roleMain:
		return roleMain, nil
	case rolePreview:
		return rolePreview, nil
	default:
		return "", fmt.Errorf("papel de sessão inválido: %q", raw)
	}
}

// readStart reads the shim's request: the IPC path and role in JSON, plus
// streamlink's stdin by SCM_RIGHTS, which libmpv opens as fdclose://N.
func readStart(c *net.UnixConn) (ctlMsg, error) {
	data, oob := make([]byte, 8192), make([]byte, 128)
	n, oobn, _, _, err := c.ReadMsgUnix(data, oob)
	if err != nil {
		return ctlMsg{}, err
	}
	var req struct {
		IPC  string `json:"ipc"`
		Role string `json:"role"`
	}
	fd := -1
	if ms, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil {
		for _, m := range ms {
			if fds, err := syscall.ParseUnixRights(&m); err == nil {
				for _, extra := range fds {
					if fd < 0 {
						fd = extra
					} else {
						closeFD(extra)
					}
				}
			}
		}
	}
	if err := json.Unmarshal(data[:n], &req); err != nil {
		closeFD(fd)
		return ctlMsg{}, err
	}
	role, err := parseRole(req.Role)
	if err != nil {
		closeFD(fd)
		return ctlMsg{}, err
	}
	if !validIPCForRole(req.IPC, role) {
		closeFD(fd)
		return ctlMsg{}, fmt.Errorf("IPC fora do runtime privado")
	}
	return ctlMsg{kind: "start", role: role, ipc: req.IPC, fd: fd, url: fmt.Sprintf("fdclose://%d", fd), conn: c}, nil
}

// validIPC accepts only the main session socket in the app's private runtime.
func validIPC(ipc string) bool {
	return validIPCForRole(ipc, roleMain)
}

// validIPCForRole accepts only the session socket in the app's private runtime
// for role ("main" in ttv-tui/<sess>/i, "preview" in ttv-tui/preview/<sess>/i,
// or private temp fallback ttv-tui-<gen>-<suffix>/i). Resolve directories to
// reject symlinks escaping the expected base.
func validIPCForRole(ipc, role string) bool {
	if !filepath.IsAbs(ipc) || filepath.Clean(ipc) != ipc || filepath.Base(ipc) != "i" {
		return false
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	base, err := filepath.Abs(filepath.Join(base, "ttv-tui"))
	if err != nil {
		return false
	}
	dir := filepath.Dir(ipc)
	// The player falls back to a private temp directory when the runtime
	// cannot hold a Unix socket. Its name carries the generation and a random decimal suffix.
	parts := strings.Split(filepath.Base(dir), "-")
	if len(parts) == 4 && parts[0] == "ttv" && parts[1] == "tui" {
		gen, genErr := strconv.ParseUint(parts[2], 10, 64)
		_, suffixErr := strconv.ParseUint(parts[3], 10, 64)
		if genErr != nil || suffixErr != nil || gen == 0 {
			return false
		}
		parent := filepath.Dir(dir)
		tempDir, err := filepath.Abs(os.TempDir())
		if err != nil {
			return false
		}
		if parent != tempDir && parent != "/tmp" {
			return false
		}
		return privateIPCDir(dir)
	}
	if !privateIPCDir(base) || !privateIPCDir(dir) {
		return false
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	if role == rolePreview {
		previewDir := filepath.Dir(dir)
		if filepath.Base(previewDir) != "preview" || !privateIPCDir(previewDir) {
			return false
		}
		realPreviewDir, err := filepath.EvalSymlinks(previewDir)
		if err != nil || filepath.Dir(realPreviewDir) != realBase || filepath.Base(realPreviewDir) != "preview" {
			return false
		}
		return filepath.Dir(realDir) == realPreviewDir
	}
	return filepath.Dir(realDir) == realBase
}

func privateIPCDir(path string) bool {
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
