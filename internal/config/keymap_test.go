package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestKeymapRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, name := range []string{"classic", "default"} {
		cfg := Default()
		cfg.Keymap = name
		if err := SaveTo(path, cfg); err != nil {
			t.Fatal(err)
		}
		got, err := LoadFrom(path)
		if err != nil || got.Keymap != name || !got.KeymapNoticeSeen {
			t.Fatalf("round trip: %+v %v", got, err)
		}
	}
	if _, err := parseTOML(`keymap = "invalid"`); err == nil {
		t.Fatal("mapa inválido aceito")
	}
}

// Quem põe keymap = "classic" no fim de um config que termina em [ui] volta ao
// mapa clássico, como nos outros campos que valem sob [ui]; regravar o arquivo
// guarda a chave uma vez só.
func TestKeymapSobUI(t *testing.T) {
	cfg, err := parseTOML("language = \"pt-BR\"\n\n[ui]\nchat_columns = 40\nkeymap = \"classic\"\nkeymap_notice_seen = true\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keymap != "classic" || !cfg.KeymapNoticeSeen {
		t.Fatalf("[ui] ignorado: keymap=%q notice_seen=%v", cfg.Keymap, cfg.KeymapNoticeSeen)
	}
	text := formatTOML(cfg)
	if strings.Count(text, "keymap = ") != 1 || strings.Count(text, "keymap_notice_seen = ") != 1 {
		t.Fatalf("chave perdida ou repetida:\n%s", text)
	}
	got, err := parseTOML(text)
	if err != nil || got.Keymap != "classic" || !got.KeymapNoticeSeen {
		t.Fatalf("ida e volta: keymap=%q notice_seen=%v err=%v", got.Keymap, got.KeymapNoticeSeen, err)
	}
	if _, err := parseTOML("[ui]\nkeymap = \"vim\"\n"); err == nil {
		t.Fatal("mapa inválido sob [ui] aceito")
	}
}
