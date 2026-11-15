package app

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeymapMigrationOnce(t *testing.T) {
	for _, lang := range []string{"en", "pt-BR"} {
		t.Run(lang, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(fmt.Sprintf("language = %q\n", lang)), 0600); err != nil {
				t.Fatal(err)
			}
			cat, _ := i18n.New(lang)
			cfg, err := config.LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			notice, err := migrateKeymap(&cfg, path, cat)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(notice, path) || !strings.Contains(notice, `keymap = "classic"`) {
				t.Fatalf("aviso: %s", notice)
			}
			cfg, err = config.LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			notice, err = migrateKeymap(&cfg, path, cat)
			if err != nil || notice != "" {
				t.Fatalf("aviso repetido: %s, %v", notice, err)
			}
		})
	}
}
func TestKeymapNewAndClassicDoNotMigrate(t *testing.T) {
	cat, _ := i18n.New("en")
	for _, cfg := range []config.Config{config.Default(), {Keymap: "classic"}} {
		notice, err := migrateKeymap(&cfg, filepath.Join(t.TempDir(), "missing.toml"), cat)
		if err != nil || notice != "" {
			t.Fatalf("migração desnecessária: %s %v", notice, err)
		}
	}
}
