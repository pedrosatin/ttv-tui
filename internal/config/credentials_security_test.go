package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStrictCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, cfg := range []Config{
		{AccessToken: "leakmark\r\nPRIVMSG"},
		{AccessToken: "leakmark\x00"},
		{AccessToken: "leakmark\n"},
		{AccessToken: "leakmark def"},
		{Username: "user\r\nname"},
		{Username: "user name"},
		{Username: "K"}, // Símbolo Kelvin (U+212A), fora do ASCII.
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := SaveTo(path, cfg); err == nil {
			t.Error("SaveTo accepted invalid credentials")
		} else if cfg.AccessToken != "" && strings.Contains(err.Error(), "leakmark") {
			t.Error("SaveTo exposed credentials")
		}
		if err := os.WriteFile(path, []byte(formatTOML(cfg)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFrom(path); err == nil {
			t.Error("LoadFrom accepted invalid credentials")
		} else if cfg.AccessToken != "" && strings.Contains(err.Error(), "leakmark") {
			t.Error("LoadFrom exposed credentials")
		}
	}
}

func TestStrictCredentialNormalization(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, token := range []string{"OAuth:abc", "oauth:oauth:abc", "abc123", "ABC123", "abc-def!~", "a:b/c+d=_%"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := SaveTo(path, Config{AccessToken: token, Username: "User_1"}); err != nil {
			t.Fatal("valid credentials rejected")
		}
		cfg, err := LoadFrom(path)
		if err != nil || cfg.AccessToken != NormalizeToken(token) {
			t.Fatal("normalization differs")
		}
	}
}

func TestStrictConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissões Unix")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("access_token = \"abc\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	notice, err := Secure(path)
	if err != nil {
		t.Fatal(err)
	}
	if !notice.Fixed || notice.Failed {
		t.Fatal("missing permission notice")
	}
	for name, want := range map[string]os.FileMode{path: 0o600, dir: 0o755} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), want)
		}
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, Config{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Error("SaveTo changed arbitrary directory")
	}
}
