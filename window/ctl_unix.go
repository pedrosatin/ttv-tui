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

// readStart reads the shim's request: the IPC path in JSON and streamlink's
// stdin by SCM_RIGHTS, which libmpv opens as fdclose://N.
func readStart(c *net.UnixConn) (ctlMsg, error) {
	data, oob := make([]byte, 8192), make([]byte, 128)
	n, oobn, _, _, err := c.ReadMsgUnix(data, oob)
	if err != nil {
		return ctlMsg{}, err
	}
	var req struct {
		IPC string `json:"ipc"`
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
	if !validIPC(req.IPC) {
		closeFD(fd)
		return ctlMsg{}, fmt.Errorf("IPC fora do runtime privado")
	}
	return ctlMsg{kind: "start", ipc: req.IPC, fd: fd, url: fmt.Sprintf("fdclose://%d", fd), conn: c}, nil
}

// validIPC accepts only the session socket in the app's private runtime.
// Resolve directories to reject symlinks escaping the expected base.
func validIPC(ipc string) bool {
	if !filepath.IsAbs(ipc) || filepath.Clean(ipc) != ipc || filepath.Base(ipc) != "i" {
		return false
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	base = filepath.Join(base, "ttv-tui")
	dir := filepath.Dir(ipc)
	// The player falls back to a private temp directory when the runtime
	// cannot hold a Unix socket. Its name carries the TUI pid and generation.
	parts := strings.Split(filepath.Base(dir), "-")
	if len(parts) == 4 && parts[0] == "ttv" && parts[1] == "tui" {
		pid, pidErr := strconv.ParseUint(parts[2], 10, 64)
		_, genErr := strconv.ParseUint(parts[3], 10, 64)
		if pidErr != nil || genErr != nil || pid == 0 {
			return false
		}
		parent := filepath.Dir(dir)
		if parent != filepath.Clean(os.TempDir()) && parent != "/tmp" {
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
	return err == nil && filepath.Dir(realDir) == realBase
}

func privateIPCDir(path string) bool {
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() || st.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
