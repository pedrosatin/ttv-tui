//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/Microsoft/go-winio"
)

func execRealMpv(real string, args []string) {
	cmd := exec.Command(real, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	if cmd.ProcessState != nil {
		os.Exit(cmd.ProcessState.ExitCode())
	}
	os.Exit(1)
}

func sendShimStream(c *net.UnixConn, ipc string) error {
	pipeName := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		return err
	}
	defer l.Close()

	msg, _ := json.Marshal(map[string]string{
		"ipc":  ipc,
		"pipe": pipeName,
	})
	if _, err := c.Write(append(msg, '\n')); err != nil {
		return err
	}

	// Wait for libmpv in the host process to connect to this pipe
	conn, err := l.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Pump streamlink's stdout (our stdin) directly to libmpv!
	_, _ = io.Copy(conn, os.Stdin)
	return nil
}
