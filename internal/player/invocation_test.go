package player

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// testInvocation builds the production argv for the tests that only read its
// shape. The executables are bare names here; Start resolves the real ones to
// absolute paths, which the tests below cover.
var testInvocation = streamlinkInvocation{bin: "streamlink", mpv: "mpv"}

// testCommand is the production argv for an endpoint and input.conf path; an
// empty inputConf drops --input-conf.
func testCommand(target Target, endpoint, inputConf string) *exec.Cmd {
	return testInvocation.build(target, sessionLayout{Endpoint: endpoint}, inputConf)
}

// mpvFileName is what an mpv executable is called in a PATH directory.
func mpvFileName() string {
	if runtime.GOOS == "windows" {
		return "mpv.exe"
	}
	return "mpv"
}

// writeExecutable creates an executable file named name in dir.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// playerFlag is the value of --player in argv.
func playerFlag(t *testing.T, argv []string) string {
	t.Helper()
	i := slices.Index(argv, "--player")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv lacks --player: %q", argv)
	}
	return argv[i+1]
}

// Um mpv plantado no diretório de onde o app foi aberto não entra: o PATH
// vazio, "." ou relativo aponta para lá e fica de fora.
func TestLookAbsPathSkipsRelativeEntries(t *testing.T) {
	launch, real := t.TempDir(), t.TempDir()
	writeExecutable(t, launch, mpvFileName())
	if err := os.Mkdir(filepath.Join(launch, "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(launch, "rel"), mpvFileName())
	want := writeExecutable(t, real, mpvFileName())
	t.Chdir(launch)

	sep := string(os.PathListSeparator)
	got, err := lookAbsPathIn("mpv", sep+"."+sep+"rel"+sep+real)
	if err != nil {
		t.Fatalf("lookAbsPathIn: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("lookAbsPathIn = %q, want %q", got, want)
	}

	_, err = lookAbsPathIn("mpv", sep+"."+sep+"rel")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("only relative entries: err = %v, want ErrNotFound", err)
	}
}

// O comando roda no diretório privado da sessão e recebe o mpv por caminho
// absoluto, igual em todos os sistemas.
func TestInvocationRunsInSessionDirWithAbsolutePlayer(t *testing.T) {
	bin := t.TempDir()
	mpv, err := lookAbsPathIn("mpv", filepath.Dir(writeExecutable(t, bin, mpvFileName())))
	if err != nil {
		t.Fatalf("lookAbsPathIn: %v", err)
	}
	streamlink := filepath.Join(bin, "streamlink")
	sess := privateSessionDir(t)
	layout := sessionLayout{Dir: sess, Endpoint: filepath.Join(sess, "i"), PluginDir: filepath.Join(sess, pluginDirName)}
	inv := streamlinkInvocation{bin: streamlink, mpv: mpv}

	for name, cmd := range map[string]*exec.Cmd{
		"command":            inv.command(testTarget("gaules"), layout),
		"commandNoInputConf": inv.commandNoInputConf(testTarget("gaules"), layout),
	} {
		if cmd.Dir == "" || cmd.Dir != sess {
			t.Errorf("%s: Dir = %q, want the session directory %q", name, cmd.Dir, sess)
		}
		if got := playerFlag(t, cmd.Args); !filepath.IsAbs(got) || got != mpv {
			t.Errorf("%s: --player %q, want the absolute %q", name, got, mpv)
		}
		if cmd.Path != streamlink {
			t.Errorf("%s: Path = %q, want %q", name, cmd.Path, streamlink)
		}
		if i := slices.Index(cmd.Args, "--plugin-dir"); i < 0 || cmd.Args[i+1] != layout.PluginDir {
			t.Errorf("%s: argv lacks --plugin-dir %q: %q", name, layout.PluginDir, cmd.Args)
		}
	}
}

// Sem mpv num diretório absoluto do PATH, a falha continua a de antes: mpv
// ausente, sem processo nenhum.
func TestManagerMissingMpvWhenOnlyRelativeOnPath(t *testing.T) {
	launch := t.TempDir()
	writeExecutable(t, launch, mpvFileName())
	t.Chdir(launch)
	t.Setenv("PATH", "."+string(os.PathListSeparator)+t.TempDir())

	mgr := New(Options{
		StreamlinkPath: writeExecutable(t, t.TempDir(), "streamlink"),
		RuntimeDir:     privateRuntimeDir(t),
	})
	t.Cleanup(mgr.Close)
	gen, err := mgr.Start(testTarget("gaules"))
	if err == nil {
		t.Fatal("Start must fail without an mpv on an absolute PATH entry")
	}
	if got := FailureOf(err); got != FailureMPVMissing {
		t.Fatalf("FailureOf(%v) = %q, want %q", err, got, FailureMPVMissing)
	}
	if !strings.Contains(err.Error(), "mpv not found") {
		t.Fatalf("error %q does not say mpv was not found", err)
	}
	if ev := waitError(t, mgr, gen, time.Second); ev.Failure != FailureMPVMissing {
		t.Fatalf("event Failure = %q, want %q", ev.Failure, FailureMPVMissing)
	}
}

// Uma busca injetada que devolve caminho relativo não vira --player.
func TestManagerRefusesRelativeMpv(t *testing.T) {
	mgr := New(Options{
		StreamlinkPath: writeExecutable(t, t.TempDir(), "streamlink"),
		RuntimeDir:     privateRuntimeDir(t),
		LookPath:       func(file string) (string, error) { return filepath.Join(".", file), nil },
	})
	t.Cleanup(mgr.Close)
	_, err := mgr.Start(testTarget("gaules"))
	if got := FailureOf(err); got != FailureMPVMissing {
		t.Fatalf("FailureOf(%v) = %q, want %q", err, got, FailureMPVMissing)
	}
}

// O Manager roda o streamlink no diretório da sessão, nunca no de onde o app
// foi aberto, e passa o mpv do PATH por caminho absoluto.
func TestManagerRunsStreamlinkInSessionDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o streamlink falso é um script sh")
	}
	work := t.TempDir()
	launch := filepath.Join(work, "launch")
	if err := os.Mkdir(launch, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, launch, "mpv")
	mpv := writeExecutable(t, t.TempDir(), "mpv")
	t.Chdir(launch)
	t.Setenv("PATH", "."+string(os.PathListSeparator)+filepath.Dir(mpv)+string(os.PathListSeparator)+os.Getenv("PATH"))

	script := filepath.Join(work, "streamlink")
	body := `#!/bin/sh
pwd -P > "$0.pwd"
: > "$0.out"
for a in "$@"; do printf '%s\n' "$a" >> "$0.out"; done
echo "[cli][error] fake streamlink" >&2
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(work, "run")
	mgr := New(Options{
		StreamlinkPath: script,
		RuntimeDir:     runtimeDir,
		SocketWait:     2 * time.Second,
		StopWait:       500 * time.Millisecond,
	})
	t.Cleanup(mgr.Close)

	gen, err := mgr.Start(Target{URL: "https://kick.com/xqc", Label: "kick:xqc", Plugins: []Plugin{{Name: "kick", Source: []byte("# plugin\n")}}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StateError, 10*time.Second)

	pwd, err := os.ReadFile(script + ".pwd")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	cwd := strings.TrimSpace(string(pwd))
	if filepath.Dir(cwd) != root {
		t.Fatalf("streamlink ran in %q, want a session directory under %q", cwd, root)
	}
	args, err := os.ReadFile(script + ".out")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(args)), "\n")
	if got := playerFlag(t, argv); got != mpv {
		t.Fatalf("--player %q, want the absolute %q", got, mpv)
	}
	i := slices.Index(argv, "--plugin-dir")
	if i < 0 || filepath.Base(argv[i+1]) != pluginDirName || filepath.Dir(argv[i+1]) != filepath.Join(runtimeDir, filepath.Base(cwd)) {
		t.Fatalf("--plugin-dir is not inside the session %q: %q", cwd, argv)
	}
}
