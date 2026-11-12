package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// findMpv looks for the real mpv on path, skipping the directory of this shim.
func findMpv(path, skip string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || strings.EqualFold(dir, skip) {
			continue
		}
		if runtime.GOOS == "windows" {
			for _, name := range []string{"mpv.exe", "mpv.com"} {
				candidate := filepath.Join(dir, name)
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
					return candidate
				}
			}
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
// streamlink ao shell e fica vivo enquanto a sessão existir.
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
			execRealMpv(real, os.Args[1:])
		}
		os.Exit(127)
	}
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := sendShimStream(c, ipc); err != nil {
		os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	_, _ = io.Copy(io.Discard, c)
}
