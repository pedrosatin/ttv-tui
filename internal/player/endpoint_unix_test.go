//go:build unix

package player

import (
	"os"
	"path/filepath"
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
	if filepath.Dir(l.Dir) != shortFallbackBaseDir || !strings.HasPrefix(filepath.Base(l.Dir), "ttv-tui-11-") {
		t.Fatalf("Dir = %q, want %s/ttv-tui-11-<aleatório>", l.Dir, shortFallbackBaseDir)
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

// O fallback só fica no diretório temporário quando o pior nome que o
// MkdirTemp pode sortear ainda cabe no sun_path; senão vai para /tmp.
func TestFallbackBaseCabeNoSunPath(t *testing.T) {
	const gen = 12
	// "<base>/ttv-tui-12-<até 10 dígitos>/i"
	overhead := len("/ttv-tui-12-") + len(mkdirTempSuffixMax) + len("/i")
	for _, tc := range []struct {
		nome    string
		baseLen int
		want    string // "base" ou "short"
	}{
		{"curta", 20, "base"},
		{"no limite", maxSocketPathLen - overhead, "base"},
		{"um byte além", maxSocketPathLen - overhead + 1, "short"},
		{"muito longa", 300, "short"},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			base := "/" + strings.Repeat("b", tc.baseLen-1)
			defer setFallbackBaseDirForTest(base)()
			got, err := fallbackBase(gen)
			if err != nil {
				t.Fatalf("fallbackBase: %v", err)
			}
			want := base
			if tc.want == "short" {
				want = shortFallbackBaseDir
			}
			if got != want {
				t.Fatalf("fallbackBase = %q, want %q", got, want)
			}
		})
	}
}

// Com uma base longa, todo diretório que o fallback cria comporta o socket.
func TestFallbackComBaseLongaCabeNoSunPath(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "longbase-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	overhead := len("/ttv-tui-5-") + len(mkdirTempSuffixMax) + len("/i")
	base := filepath.Join(root, strings.Repeat("l", maxSocketPathLen-overhead-len(root)-1))
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	defer setFallbackBaseDirForTest(base)()
	for range 20 {
		l, err := prepareSession("", 5)
		if err != nil {
			t.Fatalf("prepareSession: %v", err)
		}
		if filepath.Dir(l.Dir) != base {
			t.Fatalf("Dir = %q, want it under %q", l.Dir, base)
		}
		if len(l.Endpoint) > maxSocketPathLen {
			t.Fatalf("endpoint %q has %d bytes > %d", l.Endpoint, len(l.Endpoint), maxSocketPathLen)
		}
		_ = os.RemoveAll(l.Dir)
	}
}
