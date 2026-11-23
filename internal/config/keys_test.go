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
			"toggle_list": {"ctrl+b"},
			"quit":        {},
			"search":      {"ctrl+f", "f1"},
		},
	}
	text := formatTOML(cfg)
	if !strings.Contains(text, "[keys]") {
		t.Fatalf("sem [keys]:\n%s", text)
	}
	if !strings.Contains(text, `toggle_list = ["ctrl+b"]`) {
		t.Fatalf("entrada mal formatada:\n%s", text)
	}
	if strings.Contains(text, `"toggle_list"`) {
		t.Fatalf("chave curta saiu entre aspas:\n%s", text)
	}
	if !strings.Contains(text, `quit = []`) {
		t.Fatalf("lista vazia ausente:\n%s", text)
	}
	got, err := parseTOML(text)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Keys["toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v", got.Keys["toggle_list"])
	}
	if got.Keys["quit"] == nil || len(got.Keys["quit"]) != 0 {
		t.Fatalf("quit desligado = %#v", got.Keys["quit"])
	}
	if !slices.Equal(got.Keys["search"], []string{"ctrl+f", "f1"}) {
		t.Fatalf("search = %v", got.Keys["search"])
	}
}

func TestKeysParseShortAndLegacyFull(t *testing.T) {
	cfg, err := parseTOML("[keys]\ntoggle_list = [\"ctrl+b\"]\n\"action.help\" = [\"?\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Keys["toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("short = %v", cfg.Keys["toggle_list"])
	}
	if !slices.Equal(cfg.Keys["help"], []string{"?"}) {
		t.Fatalf("legacy full = %v", cfg.Keys["help"])
	}
	if _, ok := cfg.Keys["action.help"]; ok {
		t.Fatal("forma longa não deve ficar como chave do mapa")
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

func TestKeysMalformedNaoDerrubaCarga(t *testing.T) {
	raw := "access_token = \"accesstoken12345\"\nlanguage = \"pt-BR\"\n\n[keys]\ntoggle_list = [\"ctrl+b\"]\nbad = \"ctrl+x\"\nsearch = [\"ctrl+f\"]\n= [\"z\"]\nbroken = [a]\n"
	cfg, err := parseTOML(raw)
	if err != nil {
		t.Fatalf("carga falhou: %v", err)
	}
	if cfg.Language != "pt-BR" {
		t.Fatalf("language = %q", cfg.Language)
	}
	if cfg.AccessToken == "" {
		t.Fatal("access_token perdido")
	}
	if !slices.Equal(cfg.Keys["toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v", cfg.Keys["toggle_list"])
	}
	if !slices.Equal(cfg.Keys["search"], []string{"ctrl+f"}) {
		t.Fatalf("search = %v", cfg.Keys["search"])
	}
	if _, ok := cfg.Keys["bad"]; ok {
		t.Fatal("linha malformada entrou em Keys")
	}
	if len(cfg.KeyWarnings) == 0 {
		t.Fatal("sem KeyWarnings")
	}
	kinds := map[KeyWarningKind]bool{}
	for _, w := range cfg.KeyWarnings {
		kinds[w.Kind] = true
	}
	if !kinds[KeyWarningNotArray] || !kinds[KeyWarningEmptyKey] || !kinds[KeyWarningMalformedArray] {
		t.Fatalf("kinds = %v", cfg.KeyWarnings)
	}
	if strings.Contains(formatTOML(cfg), "KeyWarnings") || strings.Contains(formatTOML(cfg), "bad =") {
		t.Fatalf("formatTOML gravou aviso ou entrada ruim:\n%s", formatTOML(cfg))
	}
}

func TestKeysDuplicateShortAndFullPrefersShort(t *testing.T) {
	cfg, err := parseTOML("[keys]\n\"action.toggle_list\" = [\"ctrl+h\"]\ntoggle_list = [\"ctrl+b\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Keys["toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("curta deveria vencer: %v", cfg.Keys["toggle_list"])
	}
	if len(cfg.KeyWarnings) != 1 || cfg.KeyWarnings[0].Kind != KeyWarningDuplicateForm {
		t.Fatalf("esperava DuplicateForm: %v", cfg.KeyWarnings)
	}
}

func TestKeysDuplicateShortShortLastWins(t *testing.T) {
	cfg, err := parseTOML("[keys]\ntoggle_list = [\"ctrl+h\"]\ntoggle_list = [\"ctrl+b\"]\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Keys["toggle_list"], []string{"ctrl+b"}) {
		t.Fatalf("última linha deveria vencer: %v", cfg.Keys["toggle_list"])
	}
	if len(cfg.KeyWarnings) != 1 || cfg.KeyWarnings[0].Kind != KeyWarningDuplicateShort {
		t.Fatalf("esperava DuplicateShort: %v", cfg.KeyWarnings)
	}
}
