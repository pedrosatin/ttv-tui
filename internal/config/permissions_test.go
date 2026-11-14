package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func permissionFixture(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permissões Unix")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, Config{Username: "pedro"}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestChmodFailureIsNotice(t *testing.T) {
	for _, target := range []string{"file", "directory"} {
		t.Run(target, func(t *testing.T) {
			path := permissionFixture(t)
			name := path
			mode := os.FileMode(0o644)
			if target == "directory" {
				name = filepath.Dir(path)
				mode = 0o755
			}
			if err := os.Chmod(name, mode); err != nil {
				t.Fatal(err)
			}
			calls := 0
			denied := func(string, os.FileMode) error { calls++; return errors.New("chmod denied") }
			notice, err := tightenPermissions(path, denied)
			if err != nil || !notice.Failed || calls != 1 {
				t.Fatal("chmod failure interrupted load or lost notice")
			}
			notice, err = saveTo(path, Config{Username: "updated"}, denied)
			if err != nil {
				t.Fatal("chmod failure interrupted save")
			}
			if target == "directory" && !notice.Failed {
				t.Fatal("save lost permission notice")
			}
			cfg, err := LoadFrom(path)
			if err != nil || cfg.Username != "updated" {
				t.Fatal("save did not persist")
			}
		})
	}
}

func TestPrivateDirectoryNeedsNoChmod(t *testing.T) {
	path := permissionFixture(t)
	calls := 0
	chmod := func(string, os.FileMode) error { calls++; return nil }
	if _, err := tightenPermissions(path, chmod); err != nil {
		t.Fatal(err)
	}
	if _, err := saveTo(path, Config{}, chmod); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("chmod on private paths")
	}
}

func TestSymlinksDoNotChmodTargets(t *testing.T) {
	for _, target := range []string{"file", "directory"} {
		t.Run(target, func(t *testing.T) {
			path := permissionFixture(t)
			real := filepath.Join(t.TempDir(), "target")
			if target == "file" {
				if err := os.Rename(path, real); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(real, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, path); err != nil {
					t.Fatal(err)
				}
			} else {
				dir := filepath.Dir(path)
				if err := os.Rename(dir, real); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(real, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(real, "config.toml"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, dir); err != nil {
					t.Fatal(err)
				}
			}
			chmod := func(name string, mode os.FileMode) error {
				if target != "directory" || name != path || mode != 0o600 {
					t.Error("chmod aplicado ao componente errado")
				}
				return os.Chmod(name, mode)
			}
			if _, err := tightenPermissions(path, chmod); err != nil {
				t.Fatal(err)
			}
			if target == "directory" {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("arquivo regular no diretório-symlink não foi restringido")
				}
			}
			if _, err := saveTo(path, Config{}, chmod); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(real)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o644)
			if target == "directory" {
				want = 0o755
			}
			if info.Mode().Perm() != want {
				t.Fatal("symlink target permissions changed")
			}
		})
	}
}

func TestUsernameWhitespaceIsNormalized(t *testing.T) {
	cfg, err := parseTOML(`username = " pedro "`)
	if err != nil || cfg.Username != "pedro" {
		t.Fatal("username whitespace rejected")
	}
}

func TestTokenVisibleASCII(t *testing.T) {
	for c := 0; c <= 255; c++ {
		token := "a" + string(rune(c)) + "b"
		err := ValidateCredentials(token, "")
		if (err == nil) != (c >= 0x21 && c <= 0x7e) {
			t.Fatalf("unexpected validation for code point %d", c)
		}
	}
}

func TestDefaultDirectoryPermissions(t *testing.T) {
	path := permissionFixture(t)
	for _, save := range []bool{false, true} {
		if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		var notice PermissionNotice
		var err error
		if save {
			notice, err = SaveToWithNotice(path, Config{})
		} else {
			notice, err = Secure(path)
		}
		if err != nil || !notice.Fixed || notice.Failed {
			t.Fatal("default directory was not restricted")
		}
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatal("default directory permissions differ")
		}
	}
}

func TestLoadFromDoesNotSecure(t *testing.T) {
	path := permissionFixture(t)
	dir := filepath.Dir(path)
	for name, mode := range map[string]os.FileMode{path: 0o644, dir: 0o755} {
		if err := os.Chmod(name, mode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadFrom(path); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{path: 0o644, dir: 0o755} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != want {
			t.Fatal("leitura alterou permissões")
		}
	}
}

func TestSecureMissingAndStatFailure(t *testing.T) {
	path := permissionFixture(t)
	missing := filepath.Join(t.TempDir(), "ausente", "config.toml")
	notice, err := Secure(missing)
	if err != nil || notice != (PermissionNotice{}) {
		t.Fatal("caminho ausente deve ser ignorado")
	}
	_, err = Secure(filepath.Join(path, "config.toml"))
	if err == nil {
		t.Fatal("falha de stat deve ser devolvida")
	}
}

func TestSecureIsIdempotent(t *testing.T) {
	path := permissionFixture(t)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	notice, err := Secure(path)
	if err != nil || !notice.Fixed || notice.Failed {
		t.Fatal("primeiro aperto deve emitir aviso Fixed")
	}
	notice, err = Secure(path)
	if err != nil || notice != (PermissionNotice{}) {
		t.Fatal("permissões restritas não devem repetir o aviso")
	}
}
