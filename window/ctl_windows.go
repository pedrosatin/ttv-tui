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

// readStart first identifies this window process to the shim, then reads its
// request with the IPC path, role, and the named pipe libmpv opens. The shim
// checks the pipe client's PID before pumping streamlink's stdin into it.
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
		Role string `json:"role"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return ctlMsg{}, err
	}
	role, err := parseRole(req.Role)
	if err != nil {
		return ctlMsg{}, err
	}
	if !validStreamPipeForRole(req.Pipe, role) || !validIPCNameForRole(req.IPC, role) {
		return ctlMsg{}, fmt.Errorf("pipe fora do runtime esperado")
	}
	return ctlMsg{kind: "start", role: role, ipc: req.IPC, fd: -1, url: req.Pipe, conn: c}, nil
}

func validStreamPipe(name string) bool {
	return validStreamPipeForRole(name, roleMain)
}

func validStreamPipeForRole(name, role string) bool {
	const prefix = `\\.\pipe\ttv-win-stream-`
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || rest == "" || strings.ContainsAny(rest, `/\`) {
		return false
	}
	if role == rolePreview {
		tail, ok := strings.CutPrefix(rest, "preview-")
		return ok && tail != ""
	}
	return !strings.HasPrefix(rest, "preview-")
}

func validIPCName(name string) bool {
	return validIPCNameForRole(name, roleMain)
}

func validIPCNameForRole(name, role string) bool {
	const prefix = "ttv-tui-"
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || rest == "" || strings.ContainsAny(name, `/\`) {
		return false
	}
	if role == rolePreview {
		tail, ok := strings.CutPrefix(rest, "preview-")
		return ok && tail != ""
	}
	return !strings.HasPrefix(rest, "preview-")
}
