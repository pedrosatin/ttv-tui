//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
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
	_ = json.Unmarshal(data[:n], &req)
	fd := -1
	if ms, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil && len(ms) > 0 {
		if fds, err := syscall.ParseUnixRights(&ms[0]); err == nil && len(fds) > 0 {
			fd = fds[0]
		}
	}
	return ctlMsg{kind: "start", ipc: req.IPC, fd: fd, url: fmt.Sprintf("fdclose://%d", fd), conn: c}, nil
}
