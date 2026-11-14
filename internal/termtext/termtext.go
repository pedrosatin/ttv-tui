// Package termtext neutralizes third-party text before the app prints it to
// the terminal outside the TUI (prompts on stdin, warnings on stderr). A channel
// login, a streamlink line or a release version may carry ESC, OSC or CSI
// sequences; printed raw they would retitle the window, clear the screen or
// make the terminal answer a query on the input the prompt reads next.
package termtext

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Line returns s as a single printable line. C0 controls (newline included),
// DEL and C1 controls become Go escapes ("\x1b", "\n", "\u009b"); every byte of
// invalid UTF-8 becomes "\xNN". Printable text, accents included, is kept as
// is, and so is a literal backslash: the result is for a person to read, not
// to be unquoted.
func Line(s string) string {
	if isPlain(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteString(`\x`)
			b.WriteString(hex2(s[i]))
		case isControl(r):
			b.WriteString(escapeRune(r))
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// isPlain reports whether s is valid UTF-8 with no control rune, so Line can
// return it without allocating.
func isPlain(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 || c == 0x7f {
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 || isControl(r) {
			return false
		}
		i += size
	}
	return true
}

func isControl(r rune) bool {
	return r < 0x20 || r >= 0x7f && r <= 0x9f
}

func escapeRune(r rune) string {
	switch r {
	case '\n':
		return `\n`
	case '\r':
		return `\r`
	case '\t':
		return `\t`
	}
	if r < 0x80 {
		return `\x` + hex2(byte(r))
	}
	return `\u00` + hex2(byte(r))
}

func hex2(c byte) string {
	h := strconv.FormatUint(uint64(c), 16)
	if len(h) == 1 {
		return "0" + h
	}
	return h
}
