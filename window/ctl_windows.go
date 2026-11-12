//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"net"
)

func closeFD(fd int) {}

func parseCtlMsg(c *net.UnixConn) (string, int, string, error) {
	reader := bufio.NewReader(c)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return "", -1, "", err
	}
	var req struct {
		IPC  string `json:"ipc"`
		Pipe string `json:"pipe"`
	}
	_ = json.Unmarshal(line, &req)
	return req.IPC, -1, req.Pipe, nil
}
