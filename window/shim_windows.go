//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const exeSuffix = ".exe"

// mpvNames are the file names the real mpv can have in a PATH directory.
var mpvNames = []string{"mpv.exe", "mpv.com"}

func sameDir(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

func isExecutable(os.FileInfo) bool { return true }

// installShim copies this binary as mpv.exe and mpv.com: a symlink needs
// developer mode or elevation, and streamlink may pick either extension.
func installShim(exe, binDir string) error {
	for _, name := range []string{"mpv.exe", "mpv.com"} {
		if err := copyFile(exe, filepath.Join(binDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// shimPATH puts the shim first on the TUI's PATH, followed by this binary's
// directory: the copies in binDir load SDL3 and libmpv from there.
func shimPATH(binDir, exeDir, path string) string {
	sep := string(os.PathListSeparator)
	return binDir + sep + exeDir + sep + path
}

// execRealMpv runs the real mpv and exits with its status; Windows has no exec.
func execRealMpv(real string, args []string) {
	cmd := exec.Command(real, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		os.Exit(127)
	}
	os.Exit(cmd.ProcessState.ExitCode())
}

// sendShimStream opens a named pipe, tells the window its name and pumps
// streamlink's stdin into it once the announced window PID connects.
// Windows has no SCM_RIGHTS.
func sendShimStream(c *net.UnixConn, ipc string) error {
	var peer struct {
		PID uint32 `json:"pid"`
	}
	if err := json.NewDecoder(c).Decode(&peer); err != nil {
		return fmt.Errorf("PID da janela: %w", err)
	}
	if peer.PID == 0 {
		return fmt.Errorf("PID da janela ausente")
	}
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		return err
	}
	defer l.Close()
	msg, _ := json.Marshal(map[string]string{"ipc": ipc, "pipe": name})
	if _, err := c.Write(append(msg, '\n')); err != nil {
		return err
	}
	// The window closes the control connection when the session ends or libmpv
	// never started. Without this, Accept would wait forever and streamlink
	// would never see the player go.
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, c)
		close(ended)
		_ = l.Close()
	}()
	conn, err := acceptStreamClient(l, peer.PID)
	if err != nil {
		select {
		case <-ended:
			return nil
		default:
			return err
		}
	}
	go func() {
		<-ended
		_ = conn.Close()
	}()
	_, _ = io.Copy(conn, os.Stdin)
	_ = conn.Close()
	return nil
}
