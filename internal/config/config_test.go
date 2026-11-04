package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNonExistentReturnsDefaultsWithoutCreatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom non-existent file returned error: %v", err)
	}
	if cfg != Default() {
		t.Fatalf("got %+v, want default %+v", cfg, Default())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file not to exist on disk, got stat err: %v", err)
	}
}

func TestLoadDoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := "# user comment\nlanguage = \"pt-BR\"\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}
	if cfg.Language != "pt-BR" {
		t.Fatalf("got language %q, want %q", cfg.Language, "pt-BR")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file after load: %v", err)
	}
	if string(after) != raw {
		t.Fatalf("existing config was modified by LoadFrom: got %q, want %q", string(after), raw)
	}
}

func TestSaveToAtomicRoundTripAndTokenPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.toml")

	// Pre-create with loose 0644 permissions to verify overwrite tightens to 0600.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("language = \"en\"\n"), 0o644); err != nil {
		t.Fatalf("write loose file: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}

	want := Config{
		Language:    "pt-BR",
		Username:    "streamer",
		AccessToken: "secret-access-token",
	}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
		t.Fatalf("got file permissions %#o, want 0600", gotPerm)
	}

	// Ensure no leftover temporary files remain in directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("unexpected directory entries after atomic save: %v", entries)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom saved config: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

func TestNormalizeToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"oauth:abc123xyz", "abc123xyz"},
		{"OAuth:abc123xyz", "abc123xyz"},
		{"  oauth:  abc123xyz  ", "abc123xyz"},
		{"abc123xyz", "abc123xyz"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeToken(tc.in); got != tc.want {
			t.Errorf("NormalizeToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadTwtFrom(t *testing.T) {
	dir := t.TempDir()

	// Missing file returns os.ErrNotExist.
	missingPath := filepath.Join(dir, "missing.toml")
	if _, err := LoadTwtFrom(missingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadTwtFrom missing = %v, want os.ErrNotExist", err)
	}

	// File with [twitch] section, oauth: prefix, and non-string frontend keys.
	twtWithPrefix := filepath.Join(dir, "twt_prefix.toml")
	rawWithPrefix := "[twitch]\nusername = \"pedrosatin\"\nchannel = \"gaules\"\ntoken = \"oauth:tok30chars_with_prefix_1234567\"\n\n[frontend]\nshow_datetimes = false\nbadges = true\n"
	if err := os.WriteFile(twtWithPrefix, []byte(rawWithPrefix), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTwtFrom(twtWithPrefix)
	if err != nil {
		t.Fatalf("LoadTwtFrom with prefix: %v", err)
	}
	if cfg.Username != "pedrosatin" {
		t.Fatalf("username = %q, want pedrosatin", cfg.Username)
	}
	if cfg.AccessToken != "tok30chars_with_prefix_1234567" {
		t.Fatalf("access_token = %q, want normalized token", cfg.AccessToken)
	}

	// File without oauth: prefix.
	twtNoPrefix := filepath.Join(dir, "twt_noprefix.toml")
	rawNoPrefix := "[twitch]\nusername = \"pedrosatin\"\ntoken = \"tok30chars_plain_value_1234567\"\n"
	if err := os.WriteFile(twtNoPrefix, []byte(rawNoPrefix), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadTwtFrom(twtNoPrefix)
	if err != nil {
		t.Fatalf("LoadTwtFrom without prefix: %v", err)
	}
	if cfg2.AccessToken != "tok30chars_plain_value_1234567" {
		t.Fatalf("access_token = %q, want tok30chars_plain_value_1234567", cfg2.AccessToken)
	}
}

func TestLoadLegacyConfigWithRefreshToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy_config.toml")
	legacyTOML := "language = \"pt-BR\"\nusername = \"pedrosatin\"\naccess_token = \"access_token_12345\"\nrefresh_token = \"legacy_refresh_token_67890\"\n"
	if err := os.WriteFile(path, []byte(legacyTOML), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed on legacy config with refresh_token: %v", err)
	}
	if cfg.Language != "pt-BR" || cfg.Username != "pedrosatin" || cfg.AccessToken != "access_token_12345" {
		t.Fatalf("loaded config mismatch: %+v", cfg)
	}

	if err := SaveTo(path, cfg); err != nil {
		t.Fatalf("SaveTo failed: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	savedStr := string(saved)
	if strings.Contains(savedStr, "refresh_token") {
		t.Fatalf("saved config must not contain refresh_token, got:\n%s", savedStr)
	}
	if !strings.Contains(savedStr, "access_token = \"access_token_12345\"") {
		t.Fatalf("saved config missing access_token, got:\n%s", savedStr)
	}
}
