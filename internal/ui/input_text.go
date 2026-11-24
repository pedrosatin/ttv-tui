package ui

import (
	"strings"
	"unicode/utf8"

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
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		// Line/paragraph separators and the Hangul fillers become a space: the
		// fillers draw as an empty cell, but the layout and glibc's wcwidth
		// disagree on its width (ticket 26).
		case r == '\n' || r == '\r' || r == '\t' || r == '\u2028' || r == '\u2029' ||
			r == '\u3164' || r == '\u115f' || r == '\uffa0':
			b.WriteByte(' ')
		case r == '\U0001F3F4':
			// A subdivision flag keeps its tag sequence; loose tags are dropped below.
			n := flagTagSequenceLen(s[i:])
			b.WriteString(s[i-size : i+n])
			i += n
		case isInvisibleFormat(r), r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Dropped.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isInvisibleFormat reports the format characters removed from screen, copy
// and filter: bidi controls, CGJ, tags outside a flag, and the zero-width
// characters that hide inside a word (BOM, ZWSP, word joiner, Mongolian vowel
// separator, soft hyphen). ZWJ and ZWNJ stay: they build emoji and spelling.
func isInvisibleFormat(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r >= 0xe0000 && r <= 0xe007f:
		return true
	}
	switch r {
	case 0x200e, 0x200f, 0x061c, 0x034f, 0xfeff, 0x200b, 0x2060, 0x180e, 0x00ad:
		return true
	}
	return false
}

// flagTagSequenceLen returns the byte length of the tag sequence that follows
// U+1F3F4 in an emoji tag sequence. The tags spell a CLDR subdivision id (UTS
// #51): two region letters and one to four more of 0-9 and a-z, closed by
// U+E007F. Anything else returns 0 and its tags are dropped as loose tags, so
// a long run of tags cannot smuggle hidden text through a fake flag.
func flagTagSequenceLen(s string) int {
	n, specs := 0, 0
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		letter := r >= 0xe0061 && r <= 0xe007a
		digit := r >= 0xe0030 && r <= 0xe0039
		switch {
		case r == 0xe007f:
			if specs < 3 {
				return 0
			}
			return n + size
		case specs < 2 && letter, specs >= 2 && specs < 6 && (letter || digit):
			specs++
			n += size
		default:
			return 0
		}
	}
	return 0
}

// filterQuery normalizes the list filter the same way field comparison does.
func filterQuery(s string) string {
	return strings.ToLower(strings.TrimSpace(filterText(s)))
}

// filterContains reports whether haystack contains needle after normalizing
// haystack. needle must already be a filterQuery result.
func filterContains(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(filterText(haystack)), needle)
}

// filterText is the text the list shows, so a filter copied from the screen
// ("❤ live" for "❤️ live") matches. Printable ASCII is already screen text and
// skips the sanitizer: each keystroke compares the whole listing.
func filterText(s string) string {
	if plainASCII(s) {
		return s
	}
	return sanitizeScreenText(s)
}

// plainASCII reports whether s has only printable ASCII bytes, which
// sanitizeScreenText returns unchanged.
func plainASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}
