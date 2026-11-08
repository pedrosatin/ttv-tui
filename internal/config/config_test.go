package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// assertPrivateMode checks the 0600 mode. Windows has no such bits: Go reports
// 0666 there whatever the file's ACL says.
func assertPrivateMode(t *testing.T, info os.FileInfo) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
		t.Fatalf("got file permissions %#o, want 0600", gotPerm)
	}
}

func assertConfigEqual(t *testing.T, got, want Config) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadNonExistentReturnsDefaultsWithoutCreatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom non-existent file returned error: %v", err)
	}
	assertConfigEqual(t, cfg, Default())
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

func TestSaveToNewWithoutTokenPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if p := os.Getenv("TTV_TEST_CONFIG_PATH"); p != "" {
		path = p
	}

	want := Config{
		Language: "pt-BR",
	}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	assertPrivateMode(t, info)
	if out, err := exec.Command("stat", "-c", "%a", path).Output(); err == nil {
		t.Logf("stat -c %%a %s: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom saved config: %v", err)
	}
	assertConfigEqual(t, got, want)
}

func TestSaveToPreexistingLoosePermissionsTightenedWithoutToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(path, []byte("language = \"en\"\n"), 0o644); err != nil {
		t.Fatalf("write loose file: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}
	if before, err := exec.Command("stat", "-c", "%a", path).Output(); err == nil {
		t.Logf("before SaveTo stat -c %%a %s: %s", filepath.Base(path), strings.TrimSpace(string(before)))
	}

	want := Config{
		Language: "pt-BR",
	}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	assertPrivateMode(t, info)
	if after, err := exec.Command("stat", "-c", "%a", path).Output(); err == nil {
		t.Logf("after SaveTo stat -c %%a %s: %s", filepath.Base(path), strings.TrimSpace(string(after)))
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom saved config: %v", err)
	}
	assertConfigEqual(t, got, want)
}

func TestSaveToNewWithTokenPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

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
	assertPrivateMode(t, info)

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom saved config: %v", err)
	}
	assertConfigEqual(t, got, want)
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
	assertPrivateMode(t, info)

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
	assertConfigEqual(t, got, want)
}

func TestChannelFilterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	want := Config{
		Language:        "en",
		FilterLanguages: []string{"pt", "en"},
		MinViewers:      500,
	}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `languages = ["pt", "en"]`) {
		t.Fatalf("missing languages array in %q", text)
	}
	if !strings.Contains(text, "min_viewers = 500") {
		t.Fatalf("missing min_viewers in %q", text)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	assertConfigEqual(t, got, want)

	// Load also accepts uppercase codes and normalizes them.
	if err := os.WriteFile(path, []byte("languages = [\"PT\", \"es\"]\nmin_viewers = 1000\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err = LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom uppercase: %v", err)
	}
	assertConfigEqual(t, got, Config{
		FilterLanguages: []string{"pt", "es"},
		MinViewers:      1000,
	})
}

func TestLegacyWindowControlKeyIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	for _, raw := range []string{
		`window_control = "hyprland"` + "\n",
		`window_control = "Hypr"` + "\n",
		`window_control = "off"` + "\n",
		"language = \"en\"\nwindow_control = \"hyprland\"\nmin_viewers = 10\n",
	} {
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom with legacy window_control must succeed: %v\nraw=%q", err, raw)
		}
		if strings.Contains(raw, "language") {
			assertConfigEqual(t, got, Config{Language: "en", MinViewers: 10})
		} else {
			assertConfigEqual(t, got, Config{})
		}
	}

	if err := SaveTo(path, Config{Language: "pt-BR"}); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if strings.Contains(string(saved), "window_control") {
		t.Fatalf("SaveTo must not write removed window_control key: %q", saved)
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

func TestVideoChatKeysLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	for _, tc := range []struct {
		raw     string
		rows    int
		pos     string
		onTop   bool
		comment string
	}{
		{"", 0, "", false, "absent keys keep the defaults"},
		{"video_chat_rows = 12\n", 12, "", false, "a valid height"},
		{"video_chat_rows = 3\n", 3, "", false, "the lowest height"},
		{"video_chat_rows = 30\n", 30, "", false, "the highest height"},
		{"video_chat_rows = 2\n", 0, "", false, "below the range"},
		{"video_chat_rows = 31\n", 0, "", false, "above the range"},
		{"video_chat_rows = -5\n", 0, "", false, "negative"},
		{"video_chat_rows = \"ten\"\n", 0, "", false, "not a number"},
		{"video_chat_rows = 4.5\n", 0, "", false, "not an integer"},
		{"video_chat_position = \"top\"\n", 0, "top", true, "top"},
		{"video_chat_position = \"TOP\"\n", 0, "top", true, "case does not matter"},
		{"video_chat_position = \"bottom\"\n", 0, "bottom", false, "explicit bottom"},
		{"video_chat_position = \"left\"\n", 0, "", false, "unknown position"},
		{"video_chat_position = top\n", 0, "", false, "unquoted position"},
		{"[ui]\nvideo_chat_rows = 6\nvideo_chat_position = 'top'\n", 6, "top", true, "inside [ui]"},
		{"language = \"en\"\nvideo_chat_rows = 99\nvideo_chat_position = \"middle\"\nmin_viewers = 7\n", 0, "", false, "bad values leave the rest alone"},
	} {
		if err := os.WriteFile(path, []byte(tc.raw), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("%s: a bad video chat value must not fail the load: %v", tc.comment, err)
		}
		if got.VideoChatRows != tc.rows || got.VideoChatPosition != tc.pos || got.VideoChatOnTop() != tc.onTop {
			t.Fatalf("%s: rows %d pos %q top %v, want %d %q %v", tc.comment, got.VideoChatRows, got.VideoChatPosition, got.VideoChatOnTop(), tc.rows, tc.pos, tc.onTop)
		}
		if strings.Contains(tc.raw, "min_viewers") && (got.Language != "en" || got.MinViewers != 7) {
			t.Fatalf("%s: other keys lost: %+v", tc.comment, got)
		}
	}
}

func TestVideoChatKeysRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	want := Config{Language: "en", MinViewers: 50, VideoChatRows: 12, VideoChatPosition: VideoChatTop}
	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, line := range []string{"video_chat_rows = 12\n", "video_chat_position = \"top\"\n"} {
		if !strings.Contains(string(raw), line) {
			t.Fatalf("missing %q in %q", line, raw)
		}
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	assertConfigEqual(t, got, want)

	// A later save of another setting, as the channel filter does, keeps them.
	got.MinViewers = 100
	if err := SaveTo(path, got); err != nil {
		t.Fatalf("SaveTo again: %v", err)
	}
	again, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom again: %v", err)
	}
	want.MinViewers = 100
	assertConfigEqual(t, again, want)

	// An explicit bottom survives too; the defaults write nothing.
	if err := SaveTo(path, Config{VideoChatPosition: VideoChatBottom}); err != nil {
		t.Fatalf("SaveTo bottom: %v", err)
	}
	if raw, _ = os.ReadFile(path); string(raw) != "video_chat_position = \"bottom\"\n" {
		t.Fatalf("explicit bottom saved as %q", raw)
	}
	if err := SaveTo(path, Config{VideoChatRows: 99, VideoChatPosition: "left"}); err != nil {
		t.Fatalf("SaveTo invalid: %v", err)
	}
	if raw, _ = os.ReadFile(path); len(raw) != 0 {
		t.Fatalf("invalid values were written: %q", raw)
	}
}
