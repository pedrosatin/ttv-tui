//go:build unix

package player

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

func TestRuntimeBasePrivacy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         os.FileMode
		symlink      bool
		wantFallback bool
	}{
		{"shared", 0o777, false, true},
		{"group", 0o710, false, true},
		{"other", 0o701, false, true},
		{"symlink", 0o700, true, true},
		{"private", 0o700, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := privateDir(t)
			if err := os.Chmod(base, tc.mode); err != nil {
				t.Fatal(err)
			}
			runtimeDir := base
			if tc.symlink {
				runtimeDir = filepath.Join(privateDir(t), "link")
				if err := os.Symlink(base, runtimeDir); err != nil {
					t.Fatal(err)
				}
			}
			fallback := privateDir(t)
			restore := setFallbackBaseDirForTest(fallback)
			t.Cleanup(restore)
			mgr := newTestManager(t, "play", Options{RuntimeDir: runtimeDir})
			gen, err := mgr.Start(testTarget("privacy"))
			if err != nil {
				t.Fatal(err)
			}
			waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
			mgr.mu.Lock()
			dir := mgr.session.dir
			mgr.mu.Unlock()
			wantBase := runtimeDir
			if tc.wantFallback {
				wantBase = fallback
			}
			if filepath.Dir(dir) != wantBase {
				t.Errorf("session used rejected runtime base; fallback = %v", tc.wantFallback)
			}
			if tc.wantFallback {
				entries, err := os.ReadDir(base)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 0 {
					t.Error("rejected base contains session files")
				}
			}
			mgr.Stop()
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("session directory remains after Stop: %v", err)
			}
		})
	}
}

func TestRuntimeBaseForeignOwner(t *testing.T) {
	base := privateDir(t)
	if err := os.Chown(base, os.Geteuid()+1, -1); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("cannot create a foreign-owned base: %v", err)
		}
		t.Fatal(err)
	}
	fallback := privateDir(t)
	restore := setFallbackBaseDirForTest(fallback)
	t.Cleanup(restore)
	l, err := prepareSession(base, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })
	if filepath.Dir(l.Dir) != fallback {
		t.Error("session used foreign-owned base")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Error("rejected base contains session files")
	}
}

// squatDir cria dir com modo 0755, como faria outro usuário local. O sandbox de
// teste tem um só uid: os bits de grupo e outros caem na mesma checagem que um
// dono estranho.
func squatDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Outro usuário ocupa em /tmp os nomes que o app usava: o diretório de runtime
// sem XDG_RUNTIME_DIR e os fallbacks ttv-tui-<pid>-<gen>. A sessão abre mesmo
// assim, num diretório novo e privado, e o streamlink é iniciado.
func TestSessaoAbreComNomesOcupadosEmTmp(t *testing.T) {
	skipWithoutSocketFile(t)
	base, err := os.MkdirTemp("/tmp", "squat-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	useTempDir(t, base)
	t.Setenv("XDG_RUNTIME_DIR", "")

	runtimeDir := config.RuntimeDir()
	if runtimeDir != filepath.Join(base, "ttv-tui") {
		t.Fatalf("RuntimeDir = %q, want it under the isolated temp dir %q", runtimeDir, base)
	}
	squatDir(t, runtimeDir)
	for gen := 1; gen <= 3; gen++ {
		squatDir(t, filepath.Join(base, fmt.Sprintf("ttv-tui-%d-%d", os.Getpid(), gen)))
	}

	var spawned atomic.Int32
	play := helperCommand("play")
	mgr := New(Options{
		Command: func(target Target, socket string) *exec.Cmd {
			spawned.Add(1)
			return play(target, socket)
		},
		SocketWait: 5 * time.Second,
		StopWait:   500 * time.Millisecond,
	})
	t.Cleanup(mgr.Close)

	for range 2 {
		gen, err := mgr.Start(testTarget("squat"))
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		waitForState(t, mgr.Events(), gen, StatePlaying, 5*time.Second)
		mgr.mu.Lock()
		dir := mgr.session.dir
		mgr.mu.Unlock()
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Fatalf("session dir %q: %v, %v; want a 0700 directory", dir, fi, err)
		}
		if filepath.Dir(dir) != base || !strings.HasPrefix(filepath.Base(dir), fmt.Sprintf("ttv-tui-%d-", gen)) {
			t.Fatalf("session dir = %q, want %s/ttv-tui-%d-<aleatório>", dir, base, gen)
		}
	}
	if got := spawned.Load(); got != 2 {
		t.Fatalf("streamlink started %d times, want 2", got)
	}
	mgr.Stop()
}
