package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	roleMain    = "main"
	rolePreview = "preview"
)

// parseShimArgs extrai o caminho do IPC e o papel da sessão (principal ou
// prévia) a partir dos argumentos que o streamlink repassou ao shim.
func parseShimArgs(args []string) (ipc, role string) {
	role = roleMain
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--input-ipc-server="); ok {
			ipc = v
		}
		if a == "--focus-on=never" || strings.HasPrefix(a, "--autofit=") {
			role = rolePreview
		}
	}
	return ipc, role
}

// findMpv looks for the real mpv on path, skipping the directory of this shim
// and every relative entry: "", "." and the like name the working directory,
// where nothing of the player chain is looked up.
func findMpv(path, skip string) string {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) || sameDir(dir, skip) {
			continue
		}
		for _, name := range mpvNames {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() && isExecutable(st) {
				return candidate
			}
		}
	}
	return ""
}

// shimMain roda quando o binário é chamado como "mpv". Sem --input-ipc-server
// (consultas como --version) repassa ao mpv real. Com ele, entrega o stdin do
// streamlink ao shell (SCM_RIGHTS no Unix, named pipe no Windows) e fica vivo
// enquanto a sessão existir: o streamlink e o ttv-tui acompanham este processo
// como se fosse o player.
func shimMain() {
	ipc, role := parseShimArgs(os.Args[1:])
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
		_, _ = os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := sendShimStream(c, ipc, role); err != nil {
		_, _ = os.Stderr.WriteString("shim: " + err.Error() + "\n")
		os.Exit(1)
	}
	_, _ = io.Copy(io.Discard, c)
}
