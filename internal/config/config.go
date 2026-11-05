package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config represents the user configuration stored at ~/.config/ttv-tui/config.toml.
type Config struct {
	Language    string
	AccessToken string
	Username    string
	// FilterLanguages are preferred stream language codes (lowercase: pt, en).
	// Empty means no language filter. TOML key: languages.
	FilterLanguages []string
	// MinViewers is the minimum audience for channel lists. 0 means no floor.
	// TOML key: min_viewers.
	MinViewers int
}

// Default returns a Config initialized with in-memory defaults.
func Default() Config {
	return Config{}
}

// NormalizeToken trims whitespace and strips any leading "oauth:" prefix (case-insensitive).
func NormalizeToken(raw string) string {
	s := strings.TrimSpace(raw)
	for len(s) >= 6 && strings.EqualFold(s[:6], "oauth:") {
		s = strings.TrimSpace(s[6:])
	}
	return s
}

// DefaultPath resolves the default configuration file path (~/.config/ttv-tui/config.toml).
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ttv-tui", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "ttv-tui", "config.toml"), nil
}

// DefaultTwtPath resolves the default twitch-tui (twt) configuration file path (~/.config/twt/config.toml).
func DefaultTwtPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "twt", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".config", "twt", "config.toml"), nil
}

// DefaultStateDir resolves the default state directory path (~/.local/state/ttv-tui).
func DefaultStateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ttv-tui"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "ttv-tui"), nil
}

// Load reads the configuration from DefaultPath. If the file does not exist,
// it returns Default() without creating or modifying any file on disk.
func Load() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Default(), fmt.Errorf("resolve default config path: %w", err)
	}
	return LoadFrom(path)
}

// LoadFrom reads the configuration from path. If path does not exist,
// it returns Default() without writing to disk.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Default(), fmt.Errorf("read config %s: %w", path, err)
	}
	cfg, err := parseTOML(string(data))
	if err != nil {
		return Default(), fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// LoadTwt reads and normalizes credentials from the default twitch-tui config (~/.config/twt/config.toml).
func LoadTwt() (Config, error) {
	path, err := DefaultTwtPath()
	if err != nil {
		return Default(), fmt.Errorf("resolve twt config path: %w", err)
	}
	return LoadTwtFrom(path)
}

// LoadTwtFrom reads and normalizes credentials from a twitch-tui config file at path.
// Unlike LoadFrom, a missing file returns an error wrapping os.ErrNotExist.
func LoadTwtFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default(), fmt.Errorf("read twt config %s: %w", path, err)
	}
	cfg, err := parseTOML(string(data))
	if err != nil {
		return Default(), fmt.Errorf("parse twt config %s: %w", path, err)
	}
	cfg.AccessToken = NormalizeToken(cfg.AccessToken)
	return cfg, nil
}

// HasToken reports whether the configuration holds any authentication token.
func (c Config) HasToken() bool {
	return c.AccessToken != ""
}

// Save writes cfg atomically to DefaultPath.
func Save(cfg Config) error {
	path, err := DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve default config path: %w", err)
	}
	return SaveTo(path, cfg)
}

// SaveTo writes cfg atomically to path using a temporary file in the same
// directory followed by os.Rename. File permissions are enforced as 0600
// even when overwriting an existing file with looser permissions.
func SaveTo(path string, cfg Config) error {
	cfg.AccessToken = NormalizeToken(cfg.AccessToken)
	cfg.FilterLanguages = normalizeFilterLanguages(cfg.FilterLanguages)
	if cfg.MinViewers < 0 {
		cfg.MinViewers = 0
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temp config in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}

	content := formatTOML(cfg)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp config to %s: %w", path, err)
	}
	cleanup = false

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod config %s: %w", path, err)
	}
	return nil
}

func formatTOML(cfg Config) string {
	var b strings.Builder
	if cfg.Language != "" {
		fmt.Fprintf(&b, "language = %s\n", quoteTOML(cfg.Language))
	}
	if cfg.Username != "" {
		fmt.Fprintf(&b, "username = %s\n", quoteTOML(cfg.Username))
	}
	if cfg.AccessToken != "" {
		fmt.Fprintf(&b, "access_token = %s\n", quoteTOML(cfg.AccessToken))
	}
	if langs := normalizeFilterLanguages(cfg.FilterLanguages); len(langs) > 0 {
		fmt.Fprintf(&b, "languages = %s\n", formatTOMLStringArray(langs))
	}
	if cfg.MinViewers > 0 {
		fmt.Fprintf(&b, "min_viewers = %d\n", cfg.MinViewers)
	}
	return b.String()
}

func formatTOMLStringArray(vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = quoteTOML(v)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// normalizeFilterLanguages lowercases, trims, drops empties and duplicates
// (first occurrence wins), preserving order.
func normalizeFilterLanguages(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		lang := strings.ToLower(strings.TrimSpace(raw))
		if lang == "" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func quoteTOML(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func parseTOML(input string) (Config, error) {
	cfg := Default()
	scanner := bufio.NewScanner(strings.NewReader(input))
	lineNum := 0
	section := ""
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return Config{}, fmt.Errorf("line %d: missing '='", lineNum)
		}
		key := strings.TrimSpace(line[:eq])
		rawVal := strings.TrimSpace(line[eq+1:])
		fullKey := key
		if section != "" {
			fullKey = section + "." + key
		}
		switch fullKey {
		case "language", "ui.language":
			val, err := unquoteTOML(rawVal)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
			cfg.Language = val
		case "access_token", "token", "oauth_token",
			"auth.access_token", "auth.token", "auth.oauth_token",
			"twitch.access_token", "twitch.token", "twitch.oauth_token":
			val, err := unquoteTOML(rawVal)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
			cfg.AccessToken = NormalizeToken(val)
		case "username", "login", "auth.username", "auth.login", "twitch.username", "twitch.login":
			val, err := unquoteTOML(rawVal)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
			cfg.Username = val
		case "languages", "filters.languages", "channel.languages":
			vals, err := parseTOMLStringArray(rawVal)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
			cfg.FilterLanguages = normalizeFilterLanguages(vals)
		case "min_viewers", "filters.min_viewers", "channel.min_viewers":
			n, err := parseTOMLInt(rawVal)
			if err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
			if n < 0 {
				n = 0
			}
			cfg.MinViewers = n
		case "window_control", "player.window_control", "ui.window_control":
			// Removed in v1.6: geometry belongs to the compositor. Accept the
			// legacy key so old configs keep loading without error.
			if _, err := unquoteTOML(rawVal); err != nil {
				return Config{}, fmt.Errorf("line %d: key %q: %w", lineNum, key, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("scan config: %w", err)
	}
	cfg.FilterLanguages = normalizeFilterLanguages(cfg.FilterLanguages)
	return cfg, nil
}

func parseTOMLInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("expected integer value")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("expected integer value: %w", err)
	}
	return n, nil
}

func parseTOMLStringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, errors.New("expected string array value")
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return nil, nil
	}
	var out []string
	for len(inner) > 0 {
		inner = strings.TrimSpace(inner)
		if inner == "" {
			break
		}
		if inner[0] != '"' && inner[0] != '\'' {
			return nil, errors.New("expected quoted string in array")
		}
		quote := inner[0]
		end := -1
		escaped := false
		for i := 1; i < len(inner); i++ {
			c := inner[i]
			if escaped {
				escaped = false
				continue
			}
			if quote == '"' && c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unclosed string in array")
		}
		val, err := unquoteTOML(inner[:end+1])
		if err != nil {
			return nil, err
		}
		out = append(out, val)
		inner = strings.TrimSpace(inner[end+1:])
		if inner == "" {
			break
		}
		if inner[0] != ',' {
			return nil, errors.New("expected comma between array values")
		}
		inner = strings.TrimSpace(inner[1:])
	}
	return out, nil
}

func stripTOMLComment(line string) string {
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '#' && !inDouble && !inSingle {
			return line[:i]
		}
	}
	return line
}

func unquoteTOML(raw string) (string, error) {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		inner := raw[1 : len(raw)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] != '\\' {
				b.WriteByte(inner[i])
				continue
			}
			i++
			if i >= len(inner) {
				return "", errors.New("trailing backslash in string")
			}
			switch inner[i] {
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				return "", fmt.Errorf("unsupported escape \\%c", inner[i])
			}
		}
		return b.String(), nil
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	return "", errors.New("expected quoted string value")
}
