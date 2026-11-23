package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

// rewriteConfig (via Update) relê e regrava só os campos do Config. Sem Keys no
// struct, a seção [keys] some na primeira mudança de largura ou filtro.
func TestRewriteConfigPreservaKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	raw := "language = \"en\"\nlist_columns = 30\n\n[keys]\n\"action.toggle_list\" = [\"ctrl+b\"]\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewriteConfig(path, func(c *config.Config) { c.ListColumns = 40 }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "[keys]") || !strings.Contains(text, "action.toggle_list") {
		t.Fatalf("mudar a largura apagou [keys]:\n%s", text)
	}

	if err := rewriteConfig(path, func(c *config.Config) {
		c.FilterLanguages = []string{"pt"}
		c.MinViewers = 100
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListColumns != 40 || !slices.Equal(cfg.FilterLanguages, []string{"pt"}) || cfg.MinViewers != 100 {
		t.Fatalf("filtros/largura = %+v", cfg)
	}
	if got := cfg.Keys["action.toggle_list"]; !slices.Equal(got, []string{"ctrl+b"}) {
		t.Fatalf("Keys = %#v, quer action.toggle_list=[ctrl+b]", cfg.Keys)
	}
}
