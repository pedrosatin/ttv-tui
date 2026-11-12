//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"syscall"
)

func closeFD(fd int) {
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}

func parseCtlMsg(c *net.UnixConn) (string, int, string, error) {
	data, oob := make([]byte, 8192), make([]byte, 128)
	n, oobn, _, _, err := c.ReadMsgUnix(data, oob)
	if err != nil {
		return "", -1, "", err
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
	streamUrl := ""
	if fd >= 0 {
		streamUrl = fmt.Sprintf("fdclose://%d", fd)
	}
	return req.IPC, fd, streamUrl, nil
}
