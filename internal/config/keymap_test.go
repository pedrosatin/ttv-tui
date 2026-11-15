package config

import (
	"path/filepath"
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
