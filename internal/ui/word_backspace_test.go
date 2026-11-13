package ui

import "testing"

func TestTrimLastWord(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"fix", ""},
		{"fix ab", "fix "},
		{"fix ab  ", "fix "},
		{"fix ", ""},
		{"   ", ""},
		{"um dois três", "um dois "},
		{"olá, ação", "olá, "},
	}
	for _, c := range cases {
		if got := trimLastWord(c.in); got != c.want {
			t.Errorf("trimLastWord(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Alt+Backspace, que é também o Ctrl+Backspace da janela e dos terminais, apaga
// a palavra anterior nos três campos de escrita, sem esconder a lista.
func TestAltBackspaceApagaPalavra(t *testing.T) {
	cases := []struct {
		name  string
		keys  string
		key   string
		value func(Model) string
		want  string
	}{
		{"busca", "ctrl+f gaules space ao space vivo", "alt+backspace", func(m Model) string { return m.list.search.input }, "gaules ao "},
		{"filtro", "/ gaules space ao", "alt+backspace", Model.Filter, "gaules "},
		{"mensagem", "enter enter i salve space chat", "alt+backspace", Model.ComposeDraft, "salve "},
		{"backspace sem alt apaga uma letra", "ctrl+f gaules space ao", "backspace", func(m Model) string { return m.list.search.input }, "gaules a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newScenario(t, 120, 40)
			m = play(t, m, c.keys)
			layout := m.LayoutMode()
			m = play(t, m, c.key)
			if got := c.value(m); got != c.want {
				t.Fatalf("campo = %q, want %q", got, c.want)
			}
			if m.LayoutMode() != layout {
				t.Fatalf("o layout mudou de %q para %q", layout, m.LayoutMode())
			}
		})
	}
}
