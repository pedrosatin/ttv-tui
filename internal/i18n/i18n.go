// Package i18n provides localized message catalogs (English and Brazilian Portuguese)
// embedded directly into the binary with fallback to English.
//
// How to add a new key to the catalog:
//  1. Add the key and its translated string to both locales/en.json and locales/pt-BR.json.
//  2. Preserve identical placeholders (%s, %d, %w, %q, etc.) across both catalogs.
//  3. Retrieve translated strings with Catalog.T(key) or formatted strings with Catalog.TF(key, args...).
//  4. Run `go test ./internal/i18n/` to verify key parity, placeholder consistency, and absence of loose strings.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Supported language codes.
const (
	LangEN   = "en"
	LangPTBR = "pt-BR"
)

//go:embed locales/*.json
var localesFS embed.FS

// Catalog holds localized message catalogs with fallback to English.
type Catalog struct {
	lang     string
	messages map[string]map[string]string
}

// New loads the embedded translation catalogs and sets the active language
// (normalized, falling back to "en").
func New(lang string) (*Catalog, error) {
	enMap, err := loadLocaleFile("locales/en.json")
	if err != nil {
		return nil, fmt.Errorf("load en catalog: %w", err)
	}
	ptMap, err := loadLocaleFile("locales/pt-BR.json")
	if err != nil {
		return nil, fmt.Errorf("load pt-BR catalog: %w", err)
	}
	return newFromMaps(lang, map[string]map[string]string{
		LangEN:   enMap,
		LangPTBR: ptMap,
	}), nil
}

func newFromMaps(lang string, messages map[string]map[string]string) *Catalog {
	normalized := NormalizeLocale(lang)
	if normalized == "" {
		normalized = LangEN
	}
	return &Catalog{
		lang:     normalized,
		messages: messages,
	}
}

func loadLocaleFile(path string) (map[string]string, error) {
	data, err := localesFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", path, err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return m, nil
}

// Lang returns the active normalized language code ("en" or "pt-BR").
func (c *Catalog) Lang() string {
	return c.lang
}

// T looks up key in the active language catalog, falling back to "en" if missing or empty.
func (c *Catalog) T(key string) string {
	if c == nil {
		return key
	}
	if active, ok := c.messages[c.lang]; ok {
		if val, ok := active[key]; ok && val != "" {
			return val
		}
	}
	if en, ok := c.messages[LangEN]; ok {
		if val, ok := en[key]; ok && val != "" {
			return val
		}
	}
	return key
}

// Has reports whether key has a non-empty message in the active language
// itself, with no fallback to English. T hides a missing translation behind the
// English text; Has is how tests and audits see it.
func (c *Catalog) Has(key string) bool {
	if c == nil {
		return false
	}
	return c.messages[c.lang][key] != ""
}

// Resolve determines the active language using precedence:
// flagLang > configLang > envLocale > "en".
func Resolve(flagLang, configLang, envLocale string) string {
	for _, candidate := range []string{flagLang, configLang, envLocale} {
		if norm := NormalizeLocale(candidate); norm != "" {
			return norm
		}
	}
	return LangEN
}

// EnvLocale returns the first non-empty locale environment variable among
// LC_ALL, LC_MESSAGES, and LANG.
func EnvLocale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

// IsSupportedFlag reports whether raw is an explicitly supported --lang value ("en" or "pt-BR").
func IsSupportedFlag(raw string) bool {
	return raw == LangEN || raw == LangPTBR
}

// NormalizeLocale normalizes locale identifiers such as "pt_BR.UTF-8", "pt-BR",
// "pt", "en_US.UTF-8", or "en" into a supported language tag ("en" or "pt-BR"),
// or "" if unrecognized.
func NormalizeLocale(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Strip encoding (.UTF-8) and modifier (@euro).
	if idx := strings.IndexAny(s, ".@"); idx >= 0 {
		s = s[:idx]
	}
	s = strings.ReplaceAll(s, "_", "-")
	lower := strings.ToLower(s)
	switch {
	case lower == "pt-br" || lower == "pt" || strings.HasPrefix(lower, "pt-"):
		return LangPTBR
	case lower == "en" || strings.HasPrefix(lower, "en-"):
		return LangEN
	default:
		return ""
	}
}
