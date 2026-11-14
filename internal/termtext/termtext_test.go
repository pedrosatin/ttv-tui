package termtext

import (
	"testing"
	"unicode/utf8"
)

func TestLine(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"texto normal", "canal_do_zé", "canal_do_zé"},
		{"acentos e emoji", "ação — Ñandú 🎮", "ação — Ñandú 🎮"},
		{"vazio", "", ""},
		{"barra invertida literal", `C:\x1b`, `C:\x1b`},
		{"CSI", "a\x1b[2Jb", `a\x1b[2Jb`},
		{"OSC com BEL", "\x1b]0;X\x07\x1b[2J", `\x1b]0;X\x07\x1b[2J`},
		{"OSC com ST", "\x1b]8;;http://x\x1b\\link", `\x1b]8;;http://x\x1b\link`},
		{"DCS", "\x1bP1$r\x1b\\", `\x1bP1$r\x1b\`},
		{"BEL", "ding\a", `ding\x07`},
		{"NUL e outros C0", "\x00\x01\x1f", `\x00\x01\x1f`},
		{"quebras viram uma linha", "um\ndois\r\ntrês\tquatro", `um\ndois\r\ntrês\tquatro`},
		{"DEL", "a\x7fb", `a\x7fb`},
		{"C1 CSI como byte solto", "a\x9b2Jb", `a\x9b2Jb`},
		{"C1 CSI como rune", "a\u009b2Jb", `a\u009b2Jb`},
		{"C1 NEL e APC", "\u0085\u009f", `\u0085\u009f`},
		{"UTF-8 inválido", "ok\xff\xfe!", `ok\xff\xfe!`},
		{"UTF-8 truncado", "a\xc3", `a\xc3`},
		{"sobrelongo", "\xc0\x80", `\xc0\x80`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Line(tc.in)
			if got != tc.want {
				t.Fatalf("Line(%q) = %q, want %q", tc.in, got, tc.want)
			}
			assertPrintable(t, got)
		})
	}
}

// assertPrintable fails when s still carries a byte a terminal would act on.
func assertPrintable(t *testing.T, s string) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Fatalf("%q is not valid UTF-8", s)
	}
	for _, r := range s {
		if isControl(r) {
			t.Fatalf("%q still carries control %U", s, r)
		}
	}
}

func FuzzLine(f *testing.F) {
	for _, seed := range []string{"", "plain", "\x1b]0;X\x07", "\x9b", "\u009b", "\xff", "é"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		assertPrintable(t, Line(s))
	})
}
