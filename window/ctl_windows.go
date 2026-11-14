//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

// closeFD has nothing to close: the stream comes through the shim's named pipe.
func closeFD(int) {}

// readStart first identifies this window process to the shim, then reads its
// request with the IPC path and the named pipe libmpv opens. The shim checks
// the pipe client's PID before pumping streamlink's stdin into it.
func readStart(c *net.UnixConn) (ctlMsg, error) {
	if err := json.NewEncoder(c).Encode(struct {
		PID int `json:"pid"`
	}{os.Getpid()}); err != nil {
		return ctlMsg{}, err
	}
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return ctlMsg{}, err
	}
	var req struct {
		IPC  string `json:"ipc"`
		Pipe string `json:"pipe"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return ctlMsg{}, err
	}
	if !validStreamPipe(req.Pipe) || !validIPCName(req.IPC) {
		return ctlMsg{}, fmt.Errorf("pipe fora do runtime esperado")
	}
	return ctlMsg{kind: "start", ipc: req.IPC, fd: -1, url: req.Pipe, conn: c}, nil
}

func validStreamPipe(name string) bool {
	return strings.HasPrefix(name, `\\.\pipe\ttv-win-stream-`) && !strings.ContainsAny(strings.TrimPrefix(name, `\\.\pipe\ttv-win-stream-`), `/\`)
}

func validIPCName(name string) bool {
	return strings.HasPrefix(name, "ttv-tui-") && !strings.ContainsAny(name, `/\`)
}
