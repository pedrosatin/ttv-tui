//go:build !windows

package main

import (
	"encoding/json"
	"net"
	"os"
	"syscall"
)

func execRealMpv(real string, args []string) {
	_ = syscall.Exec(real, append([]string{"mpv"}, args...), os.Environ())
}

func sendShimStream(c *net.UnixConn, ipc string) error {
	msg, _ := json.Marshal(map[string]string{"ipc": ipc})
	_, _, err := c.WriteMsgUnix(msg, syscall.UnixRights(0), nil)
	return err
}
