package player

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPreviewCommandArgv(t *testing.T) {
	endpoint := "/run/user/1000/ttv-tui/preview/1/i"
	layout := sessionLayout{Dir: "/run/user/1000/ttv-tui/preview/1", Endpoint: endpoint}
	cmd := testInvocation.previewCommand(testTarget("gaules"), layout)

	wantArgs := []string{
		"streamlink",
		"--no-config",
		"--loglevel", "info",
		"https://www.twitch.tv/gaules",
		PreviewQuality,
		"--player", "mpv",
		"--player-args", buildPlayerArgs(previewMPVArgs(endpoint)),
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("preview argv:\ngot:  %v\nwant: %v", cmd.Args, wantArgs)
	}
	if slices.Contains(cmd.Args, "best") {
		t.Fatal("preview argv still asks for best quality")
	}
	playerArgs := ""
	for i, a := range cmd.Args {
		if a == "--player-args" && i+1 < len(cmd.Args) {
			playerArgs = cmd.Args[i+1]
		}
	}
	for _, want := range []string{
		"--mute=yes",
		"--autofit=" + PreviewAutofit,
		"--focus-on=never",
		"--input-ipc-server=" + endpoint,
	} {
		if !strings.Contains(playerArgs, want) {
			t.Fatalf("player-args %q missing %q", playerArgs, want)
		}
	}
	if strings.Contains(playerArgs, "--input-conf=") {
		t.Fatalf("preview must omit input.conf, got %q", playerArgs)
	}
}

func TestNewPreviewNestsRuntimeAndRegistry(t *testing.T) {
	runtime := t.TempDir()
	registry := t.TempDir()
	mgr := NewPreview(Options{RuntimeDir: runtime, RegistryDir: registry, Command: helperCommand("play")})
	t.Cleanup(mgr.Close)
	if got, want := mgr.opts.RuntimeDir, filepath.Join(runtime, "preview"); got != want {
		t.Fatalf("RuntimeDir = %q, want %q", got, want)
	}
	if got, want := mgr.opts.RegistryDir, filepath.Join(registry, "preview"); got != want {
		t.Fatalf("RegistryDir = %q, want %q", got, want)
	}
	if !mgr.opts.Preview {
		t.Fatal("Preview flag not set")
	}
}

// FindOrphans on the registry root skips directories, so a leftover preview
// record under registry/preview is invisible unless the caller also scans that
// nested dir (app.handleOrphans does both).
func TestFindOrphansSkipsPreviewSubdir(t *testing.T) {
	regDir := t.TempDir()
	previewDir := filepath.Join(regDir, "preview")
	if err := os.MkdirAll(previewDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dead := deadPid(t)
	if _, err := writeRecord(previewDir, Record{
		OwnerPID: dead, OwnerStart: 1, PGID: dead,
		Socket: "/nonexistent/preview/i", Generation: 1, Login: "prevchan",
	}); err != nil {
		t.Fatal(err)
	}
	orphans, err := FindOrphans(regDir)
	if err != nil {
		t.Fatalf("FindOrphans(root): %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("root scan saw nested preview records: %+v", orphans)
	}
	orphans, err = FindOrphans(previewDir)
	if err != nil {
		t.Fatalf("FindOrphans(preview): %v", err)
	}
	// Dead owner + dead pgid: stale record removed silently, no live orphan.
	if len(orphans) != 0 {
		t.Fatalf("preview orphans = %+v", orphans)
	}
	matches, _ := filepath.Glob(filepath.Join(previewDir, recordPrefix+"*"+recordSuffix))
	if len(matches) != 0 {
		t.Fatalf("stale preview record left behind: %v", matches)
	}
}

// NewPreview with LookPath must pass an absolute --player, run streamlink in
// the private session directory, and place --plugin-dir inside that session.
func TestPreviewProfileAbsolutePlayerPrivateDirAndPlugin(t *testing.T) {
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
	mgr := NewPreview(Options{
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
	// pwd reports the resolved path (/private/tmp on macOS) while the manager
	// builds paths from runtimeDir as given, and the session tree is gone by
	// now: compare paths with their existing prefix resolved.
	root := resolveExistingPrefix(t, filepath.Join(runtimeDir, "preview"))
	cwd := strings.TrimSpace(string(pwd))
	if filepath.Dir(cwd) != root && !strings.HasPrefix(cwd, root) {
		t.Fatalf("streamlink ran in %q, want a session under %q", cwd, root)
	}
	args, err := os.ReadFile(script + ".out")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSpace(string(args)), "\n")
	if got := playerFlag(t, argv); got != mpv {
		t.Fatalf("--player %q, want absolute %q", got, mpv)
	}
	if !slices.Contains(argv, PreviewQuality) {
		t.Fatalf("argv missing %q: %q", PreviewQuality, argv)
	}
	i := slices.Index(argv, "--plugin-dir")
	if i < 0 || filepath.Base(argv[i+1]) != pluginDirName {
		t.Fatalf("--plugin-dir missing or wrong in %q", argv)
	}
	if pluginParent := resolveExistingPrefix(t, filepath.Dir(argv[i+1])); pluginParent != cwd {
		t.Fatalf("--plugin-dir %q not inside session %q", argv[i+1], cwd)
	}
}

func TestPreviewStopLeavesNoOrphan(t *testing.T) {
	t.Parallel()
	mgr := NewPreview(Options{
		RuntimeDir: t.TempDir(),
		Command:    helperCommand("play"),
		StopWait:   500 * time.Millisecond,
	})
	t.Cleanup(mgr.Close)

	gen, err := mgr.Start(testTarget("previewchan"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	pid := 0
	if mgr.session != nil {
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	if pid <= 0 {
		t.Fatal("no session pid")
	}
	mgr.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("preview process %d still alive after Stop", pid)
	}
}

func TestPreviewCloseLeavesNoOrphan(t *testing.T) {
	t.Parallel()
	mgr := NewPreview(Options{
		RuntimeDir: t.TempDir(),
		Command:    helperCommand("play"),
		StopWait:   500 * time.Millisecond,
	})
	gen, err := mgr.Start(testTarget("previewclose"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
	mgr.mu.Lock()
	pid := 0
	if mgr.session != nil {
		pid = mgr.session.Pid()
	}
	mgr.mu.Unlock()
	mgr.Close()
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("preview process %d still alive after Close", pid)
	}
}

// resolveExistingPrefix resolves symlinks in the longest prefix of path that
// still exists and appends the rest unchanged, so a path under a removed
// session can be compared with one reported by the process (pwd).
func resolveExistingPrefix(t *testing.T, path string) string {
	t.Helper()
	rest := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}
