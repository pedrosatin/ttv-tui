//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// closeFD has nothing to close: the stream comes through the shim's named pipe.
func closeFD(int) {}

// readStart reads the shim's request, one JSON line with the IPC path and the
// named pipe libmpv opens; the shim pumps streamlink's stdin into that pipe.
func readStart(c *net.UnixConn) (ctlMsg, error) {
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
	if !validStreamPipe(req.Pipe) || !strings.HasPrefix(req.IPC, "ttv-tui-") || strings.ContainsAny(req.IPC, `/\`) {
		return ctlMsg{}, fmt.Errorf("pipe fora do runtime esperado")
	}
	return ctlMsg{kind: "start", ipc: req.IPC, fd: -1, url: req.Pipe, conn: c}, nil
}

func validStreamPipe(name string) bool {
	return strings.HasPrefix(name, `\\.\pipe\ttv-win-stream-`) && !strings.ContainsAny(strings.TrimPrefix(name, `\\.\pipe\ttv-win-stream-`), `/\`)
}
