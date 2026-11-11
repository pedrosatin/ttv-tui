//go:build unix

package player

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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
			gen, err := mgr.Start("privacy")
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

func TestFallbackRejectsSharedDirectory(t *testing.T) {
	restore := setFallbackBaseDirForTest(privateDir(t))
	t.Cleanup(restore)
	dir, _, err := fallbackLocation(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	layout, err := prepareSession("", 1)
	if err == nil {
		t.Fatalf("session accepted shared fallback directory: %q", layout.Dir)
	}
	if layout.Dir != "" {
		t.Errorf("session returned rejected fallback directory: %q", layout.Dir)
	}
}
