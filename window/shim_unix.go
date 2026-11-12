//go:build !windows

package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

const exeSuffix = ""

// mpvNames are the file names the real mpv can have in a PATH directory.
var mpvNames = []string{"mpv"}

func sameDir(a, b string) bool { return a == b }

func isExecutable(st os.FileInfo) bool { return st.Mode()&0o111 != 0 }

// installShim links this binary as "mpv" in binDir.
func installShim(exe, binDir string) error {
	return os.Symlink(exe, filepath.Join(binDir, "mpv"))
}

// shimPATH puts the shim first on the TUI's PATH.
func shimPATH(binDir, _, path string) string {
	return binDir + string(os.PathListSeparator) + path
}

func execRealMpv(real string, args []string) {
	_ = syscall.Exec(real, append([]string{"mpv"}, args...), os.Environ())
}

// sendShimStream hands streamlink's stdin to the window by SCM_RIGHTS.
func sendShimStream(c *net.UnixConn, ipc string) error {
	msg, _ := json.Marshal(map[string]string{"ipc": ipc})
	_, _, err := c.WriteMsgUnix(msg, syscall.UnixRights(0), nil)
	return err
}
