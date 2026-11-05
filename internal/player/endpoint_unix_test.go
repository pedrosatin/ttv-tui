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

// A $TMPDIR that cannot hold the socket must not cost the session: it used to
// work from /tmp and still does.
func TestFallbackLeavesTempDirTooLongForTheSocket(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("t", maxSocketPathLen))
	useTempDir(t, long)

	l, err := prepareSession(unusableRuntimeDir(t), 11)
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })
	if want := filepath.Join(shortFallbackBaseDir, "ttv-tui-"+strconv.Itoa(os.Getpid())+"-11"); l.Dir != want {
		t.Fatalf("Dir = %q, want %q", l.Dir, want)
	}
	if len(l.Endpoint) > maxSocketPathLen {
		t.Fatalf("endpoint %q exceeds %d bytes", l.Endpoint, maxSocketPathLen)
	}
}

func TestEndpointInDirSocketLimitBoundary(t *testing.T) {
	// "<dir>/i" is len(dir)+2 bytes; the limit is the running OS's sun_path less the NUL.
	fits := "/" + strings.Repeat("d", maxSocketPathLen-3)
	if got, err := endpointInDir(fits); err != nil || len(got) != maxSocketPathLen {
		t.Fatalf("endpointInDir(len %d) = %q, %v; want a %d-byte socket path", len(fits), got, err, maxSocketPathLen)
	}
	if _, err := endpointInDir(fits + "d"); err == nil {
		t.Fatalf("a socket path of %d bytes must be refused", maxSocketPathLen+1)
	}
	if err := checkRuntimeDirFits(strings.Repeat("r", maxSocketPathLen), 1); err == nil {
		t.Fatal("checkRuntimeDirFits must refuse a runtime dir that cannot hold the socket")
	}
}
