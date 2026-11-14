package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// sanitizeChatText is shared by text inputs and untrusted plain-text output.
// Preserve Unicode (including emoji sequences); flatten line breaks and tabs.
func sanitizeChatText(s string) string {
	// The ANSI parser operates on bytes. Translate Unicode C1 controls to their
	// seven-bit ESC equivalents without changing UTF-8 continuation bytes.
	var escaped strings.Builder
	escaped.Grow(len(s))
	for _, r := range s {
		if r >= 0x80 && r <= 0x9f {
			escaped.WriteByte(0x1b)
			escaped.WriteByte(byte(r - 0x40))
		} else {
			escaped.WriteRune(r)
		}
	}
	s = ansi.Strip(escaped.String())
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
			continue
		}
		if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) ||
			r == 0x200e || r == 0x200f || r == 0x061c || r == 0x034f || (r >= 0xe0000 && r <= 0xe007f) {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
