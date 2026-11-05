//go:build unix

package player

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPrepareSessionUnixEndpointIsSocketInDir(t *testing.T) {
	l, err := prepareSession(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })

	if want := filepath.Join(l.Dir, "i"); l.Endpoint != want {
		t.Fatalf("Endpoint = %q, want %q", l.Endpoint, want)
	}
	fi, err := os.Stat(l.Dir)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("Dir = %v, %v; want a 0700 directory", fi, err)
	}
	if _, err := os.Stat(l.Endpoint); !os.IsNotExist(err) {
		t.Fatalf("the endpoint is the player's to create, found: %v", err)
	}
}

func TestEndpointInDirRejectsLongSocketPath(t *testing.T) {
	_, err := endpointInDir("/" + strings.Repeat("d", maxSocketPathLen))
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("endpointInDir error = %v, want too long", err)
	}
}

func TestFallbackDropsStaleEndpoint(t *testing.T) {
	base := t.TempDir()
	defer setFallbackBaseDirForTest(base)()
	dir := filepath.Join(base, "ttv-tui-"+strconv.Itoa(os.Getpid())+"-9")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	stale := filepath.Join(dir, "i")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	l, err := prepareSession(filepath.Join(t.TempDir(), strings.Repeat("x", 120)), 9)
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	if l.Dir != dir || l.Endpoint != stale {
		t.Fatalf("layout = %+v, want the fallback %q", l, dir)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale endpoint not removed: %v", err)
	}
}
