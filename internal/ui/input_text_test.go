package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextInputSanitizesPaste(t *testing.T) {
	for _, mode := range []string{"search", "filter", "compose"} {
		for _, tc := range []struct{ name, input, want string }{
			{"CSI", "a\x1b[2Jb", "ab"},
			{"OSC_BEL", "a\x1b]0;pwn\ab", "ab"},
			{"OSC_ST", "a\x1b]0;pwn\x1b\\b", "ab"},
			{"C1_CSI", "a\u009b2Jb", "ab"},
			{"C1_OSC", "a\u009d0;pwn\u009cb", "ab"},
			{"controls", "a\x00\x08\x7f\u0085b", "ab"},
			{"unicode", "ação é 中文 🫶🏻 👨‍👩‍👧‍👦 https://example.org/a?q=é", "ação é 中文 🫶🏻 👨‍👩‍👧‍👦 https://example.org/a?q=é"},
			{"multiline", "a\r\nb\nc\td", "a  b c d"},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				m := New(nil)
				m.width, m.height = 200, 24
				switch mode {
				case "search":
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
				case "filter":
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
				case "compose":
					m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
				}
				m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.input), Paste: true})
				got := m.list.search.input
				switch mode {
				case "filter":
					got = m.Filter()
				case "compose":
					got = m.ComposeDraft()
				}
				if got != tc.want {
					t.Errorf("input = %q, want %q", got, tc.want)
				}
				view := m.View()
				for _, hostile := range []string{"\x1b[2J", "\x1b]0", "\u009b", "\u009d", "pwn"} {
					if strings.Contains(view, hostile) {
						t.Errorf("render contains %q", hostile)
					}
				}
				if strings.ContainsAny(got, "\r\n\t") {
					t.Errorf("input contains line/tab controls: %q", got)
				}
			})
		}
	}
}
