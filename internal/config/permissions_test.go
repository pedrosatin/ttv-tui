package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
			denied := func(*os.File, os.FileMode) error { calls++; return errors.New("chmod denied") }
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
	chmod := func(*os.File, os.FileMode) error { calls++; return nil }
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

// symlinkFixture troca o config padrão por um symlink para real, criado por make.
func symlinkFixture(t *testing.T, make func(real string)) (path, real, resolved string) {
	t.Helper()
	path = permissionFixture(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	real = filepath.Join(t.TempDir(), "target")
	make(real)
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Dir(real))
	if err != nil {
		t.Fatal(err)
	}
	return path, real, filepath.Join(resolved, filepath.Base(real))
}

func writeLeakyConfig(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(name, []byte("access_token = \"velho\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSymlink(t *testing.T, path, real string) {
	t.Helper()
	link, err := os.Readlink(path)
	if err != nil || link != real {
		t.Fatal("symlink do config não foi preservado")
	}
}

func TestFileSymlinkTightensOwnTarget(t *testing.T) {
	path, real, resolved := symlinkFixture(t, func(real string) { writeLeakyConfig(t, real) })
	var chmodded []string
	chmod := func(f *os.File, mode os.FileMode) error {
		chmodded = append(chmodded, f.Name())
		return fchmod(f, mode)
	}
	notice, err := tightenPermissions(path, chmod)
	if err != nil || !notice.Fixed || notice.Failed || notice.Path != resolved {
		t.Fatalf("aviso inesperado: %+v, %v", notice, err)
	}
	if len(chmodded) != 1 || chmodded[0] != resolved {
		t.Fatalf("chmod aplicado a %v", chmodded)
	}
	info, err := os.Stat(real)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("alvo do symlink continua legível")
	}
	assertSymlink(t, path, real)
	notice, err = Secure(path)
	if err != nil || notice != (PermissionNotice{}) {
		t.Fatal("alvo já restrito não deve repetir o aviso")
	}
}

func TestSaveToFileSymlinkReplacesTarget(t *testing.T) {
	path, real, _ := symlinkFixture(t, func(real string) { writeLeakyConfig(t, real) })
	if err := SaveTo(path, Config{AccessToken: "novo"}); err != nil {
		t.Fatal(err)
	}
	assertSymlink(t, path, real)
	info, err := os.Stat(real)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("alvo gravado sem 0600")
	}
	cfg, err := LoadFrom(path)
	if err != nil || cfg.AccessToken != "novo" {
		t.Fatal("conteúdo novo não chegou ao alvo")
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(real)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := filepath.Join(dir, entry.Name())
			info, err := os.Lstat(name)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if name != real {
				t.Fatalf("arquivo extra após gravar: %s", name)
			}
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "velho") || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("token exposto em %s", name)
			}
		}
	}
}

func TestFileSymlinkNonRegularTargetFails(t *testing.T) {
	path, real, resolved := symlinkFixture(t, func(real string) {
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatal(err)
		}
	})
	chmod := func(*os.File, os.FileMode) error {
		t.Error("chmod em alvo que não é arquivo regular")
		return nil
	}
	notice, err := tightenPermissions(path, chmod)
	if err != nil || !notice.Failed || notice.Fixed || notice.Path != resolved {
		t.Fatalf("aviso inesperado: %+v, %v", notice, err)
	}
	if _, err := saveTo(path, Config{AccessToken: "novo"}, chmod); err == nil {
		t.Fatal("gravação sobre alvo que não é arquivo deve falhar")
	}
	assertSymlink(t, path, real)
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
		t.Fatal("alvo não regular foi alterado")
	}
}

func TestFileSymlinkOtherOwnerFails(t *testing.T) {
	const other = "/etc/hosts"
	info, err := os.Lstat(other)
	if err != nil || !info.Mode().IsRegular() || ownedByCurrentUser(info) || info.Mode().Perm()&0o077 == 0 {
		t.Skip("sem arquivo regular legível de outro usuário")
	}
	path := permissionFixture(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	chmod := func(*os.File, os.FileMode) error {
		t.Error("chmod em arquivo de outro usuário")
		return nil
	}
	// O aviso cita o alvo resolvido: no macOS, /etc é um link para /private/etc.
	want, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := tightenPermissions(path, chmod)
	if err != nil || !notice.Failed || notice.Fixed || notice.Path != want {
		t.Fatalf("aviso inesperado: %+v, %v (alvo %s)", notice, err, want)
	}
}

func TestSaveToDanglingSymlinkFails(t *testing.T) {
	path, real, _ := symlinkFixture(t, func(string) {})
	if err := SaveTo(path, Config{AccessToken: "novo"}); err == nil {
		t.Fatal("symlink quebrado deve falhar")
	}
	if _, err := os.Lstat(real); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("gravação criou o alvo do symlink quebrado")
	}
	notice, err := Secure(path)
	if err != nil || notice != (PermissionNotice{}) {
		t.Fatal("symlink quebrado não guarda segredo e deve ser ignorado")
	}
}

func TestDirectorySymlinkTightensRegularFile(t *testing.T) {
	path := permissionFixture(t)
	dir := filepath.Dir(path)
	real := filepath.Join(t.TempDir(), "target")
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
	chmod := func(f *os.File, mode os.FileMode) error {
		if f.Name() != path || mode != 0o600 {
			t.Error("chmod aplicado ao componente errado")
		}
		return fchmod(f, mode)
	}
	notice, err := tightenPermissions(path, chmod)
	if err != nil || !notice.Fixed || notice.Failed || notice.Path != "" {
		t.Fatalf("aviso inesperado: %+v, %v", notice, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("arquivo regular no diretório-symlink não foi restringido")
	}
	if _, err := saveTo(path, Config{}, chmod); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(real)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("diretório alvo do symlink foi alterado")
	}
}

func TestRestrictRefusesSwappedPath(t *testing.T) {
	path := permissionFixture(t)
	other := filepath.Join(t.TempDir(), "other")
	writeLeakyConfig(t, other)
	want, err := os.Lstat(other)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	chmod := func(*os.File, os.FileMode) error {
		t.Error("chmod depois da troca do caminho")
		return nil
	}
	if err := restrict(path, want, 0o600, chmod); !errors.Is(err, errFileChanged) {
		t.Fatalf("arquivo trocado não foi detectado: %v", err)
	}
	if err := restrict(link, want, 0o600, chmod); err == nil {
		t.Fatal("restrict seguiu symlink")
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
