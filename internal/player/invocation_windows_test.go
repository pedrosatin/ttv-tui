package player

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/pathlookup"
)

// No modo janela, a janela põe as cópias do shim (mpv.exe e mpv.com) num bin à
// frente do PATH e o diretório do próprio executável logo depois, para as DLLs.
// Um mpv.exe e um SDL3.dll plantados no diretório de onde o app foi aberto, com
// "." no PATH, não entram: o --player é a cópia do shim por caminho absoluto e o
// streamlink roda no diretório da sessão, onde a cópia não acha DLL nenhuma e
// segue para o PATH.
func TestInvocationUsesWindowShimByAbsolutePath(t *testing.T) {
	launch := t.TempDir()
	for _, name := range []string{"mpv.exe", "mpv.com", "SDL3.dll", "libmpv-2.dll"} {
		writeExecutable(t, launch, name)
	}
	shell := t.TempDir()
	binDir := filepath.Join(shell, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mpv.exe", "mpv.com"} {
		writeExecutable(t, binDir, name)
	}
	exeDir := t.TempDir()
	for _, name := range []string{"ttv-tui-window.exe", "SDL3.dll", "libmpv-2.dll"} {
		writeExecutable(t, exeDir, name)
	}
	t.Chdir(launch)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	sep := string(os.PathListSeparator)
	path := "." + sep + binDir + sep + exeDir + sep + os.Getenv("PATH")

	mpv, err := pathlookup.AbsIn("mpv", path)
	if err != nil {
		t.Fatalf("pathlookup.AbsIn: %v", err)
	}
	if !filepath.IsAbs(mpv) || !strings.EqualFold(filepath.Dir(mpv), binDir) {
		t.Fatalf("mpv = %q, want the shim copy in %q", mpv, binDir)
	}
	if base := strings.ToLower(filepath.Base(mpv)); base != "mpv.com" && base != "mpv.exe" {
		t.Fatalf("mpv = %q, want mpv.com or mpv.exe", mpv)
	}

	sess := privateSessionDir(t)
	layout := sessionLayout{Dir: sess, Endpoint: pipeName(sess, os.Getpid()) + "-x", PluginDir: filepath.Join(sess, pluginDirName)}
	streamlink := filepath.Join(t.TempDir(), "streamlink.exe")
	cmd := streamlinkInvocation{bin: streamlink, mpv: mpv}.command(Target{URL: "https://kick.com/xqc", Label: "kick:xqc"}, layout)
	if cmd.Dir == "" || !strings.EqualFold(cmd.Dir, sess) || strings.EqualFold(cmd.Dir, launch) {
		t.Fatalf("Dir = %q, want the session directory %q", cmd.Dir, sess)
	}
	if got := playerFlag(t, cmd.Args); got != mpv {
		t.Fatalf("--player %q, want %q", got, mpv)
	}
	if !strings.EqualFold(cmd.Path, streamlink) {
		t.Fatalf("Path = %q, want %q", cmd.Path, streamlink)
	}
}
