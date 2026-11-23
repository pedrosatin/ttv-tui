package config

import (
	"slices"
	"strings"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	cfg := Config{
		Language: "en",
		Keys: map[string][]string{
			"action.toggle_list": {"ctrl+b"},
			"action.quit":        {},
			"action.search":      {"ctrl+f", "f1"},
		},
	}
	text := formatTOML(cfg)
	if !strings.Contains(text, "[keys]") {
		t.Fatalf("sem [keys]:\n%s", text)
	}
	// Chaves com ponto saem entre aspas para um parser TOML real não aninhar.
	if !strings.Contains(text, `"action.toggle_list" = ["ctrl+b"]`) {
		t.Fatalf("entrada mal formatada:\n%s", text)
	}
	if !strings.Contains(text, `"action.quit" = []`) {
		t.Fatalf("lista vazia ausente:\n%s", text)
	}
	got, err := parseTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Keys["action.toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v", got.Keys["action.toggle_list"])
	}
	if got.Keys["action.quit"] == nil || len(got.Keys["action.quit"]) != 0 {
		t.Fatalf("quit desligado = %#v", got.Keys["action.quit"])
	}
	if !slices.Equal(got.Keys["action.search"], []string{"ctrl+f", "f1"}) {
		t.Fatalf("search = %v", got.Keys["action.search"])
	}
}

func TestKeysParseQuotedAndBare(t *testing.T) {
	cfg, err := parseTOML("[keys]\n\"action.toggle_list\" = [\"ctrl+b\"]\naction.help = [\"?\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Keys["action.toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("quoted = %v", cfg.Keys["action.toggle_list"])
	}
	if !slices.Equal(cfg.Keys["action.help"], []string{"?"}) {
		t.Fatalf("bare = %v", cfg.Keys["action.help"])
	}
}

func TestKeysAusenteFicaNil(t *testing.T) {
	cfg, err := parseTOML("language = \"en\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys != nil {
		t.Fatalf("Keys = %#v, quer nil", cfg.Keys)
	}
	if strings.Contains(formatTOML(cfg), "[keys]") {
		t.Fatal("formatTOML escreveu [keys] sem entradas")
	}
}
