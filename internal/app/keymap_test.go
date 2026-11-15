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

// Um config que já volta ao classic sob [ui] não recebe o aviso, e regravar o
// arquivo (como a troca de idioma faz) não perde nem repete a chave.
func TestKeymapClassicSobUINaoMigraEResisteARegravacao(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("language = \"en\"\n\n[ui]\nkeymap = \"classic\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cat, _ := i18n.New("en")
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if notice, err := migrateKeymap(&cfg, path, cat); err != nil || notice != "" {
		t.Fatalf("classic sob [ui] recebeu o aviso: %q %v", notice, err)
	}
	for range 2 {
		if err := rewriteConfig(path, func(c *config.Config) { c.Language = "pt-BR" }); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(data), "keymap = "); n != 1 {
			t.Fatalf("keymap aparece %d vezes:\n%s", n, data)
		}
		if cfg, err := config.LoadFrom(path); err != nil || cfg.Keymap != "classic" {
			t.Fatalf("regravação perdeu o classic: %q %v", cfg.Keymap, err)
		}
	}
}
