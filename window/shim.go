package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// findMpv looks for the real mpv on path, skipping the directory of this shim.
func findMpv(path, skip string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || dir == skip {
			continue
		}
		candidate := filepath.Join(dir, "mpv")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// shimMain roda quando o binário é chamado como "mpv". Sem --input-ipc-server
// (consultas como --version) repassa ao mpv real. Com ele, entrega o stdin do
// streamlink ao shell por SCM_RIGHTS e fica vivo enquanto a sessão existir: o
// streamlink e o ttv-tui acompanham este processo como se fosse o player.
func shimMain() {
	ipc := ""
	for _, a := range os.Args[1:] {
		if v, ok := strings.CutPrefix(a, "--input-ipc-server="); ok {
			ipc = v
		}
	}
	sock := os.Getenv("TTV_WINDOW_SOCK")
	if ipc == "" || sock == "" {
		skip := ""
		if sock != "" {
			skip = filepath.Join(filepath.Dir(sock), "bin")
		}
		if real := findMpv(os.Getenv("PATH"), skip); real != "" {
			_ = syscall.Exec(real, append([]string{"mpv"}, os.Args[1:]...), os.Environ())
		}
		os.Exit(127)
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	msg, _ := json.Marshal(map[string]string{"ipc": ipc})
	if _, _, err := c.WriteMsgUnix(msg, syscall.UnixRights(0), nil); err != nil {
		os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	_, _ = io.Copy(io.Discard, c)
}
