package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryVideoSurvivesConfigRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("video = \"sixel\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "video = \"sixel\"") {
		t.Fatal("config rewrite lost embedded video opt-in")
	}
}
