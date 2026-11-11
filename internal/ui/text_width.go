package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// normalizeScreenText keeps the first element of a ZWJ sequence and removes
// skin tones and presentation selectors. Terminals disagree on the cell width
// of these sequences. Combining accents and CJK are deliberately preserved.
// This is a display transform: source data, outgoing chat and clipboard stay raw.
func normalizeScreenText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	skipJoined := false
	lastWasEmoji := false
	for _, r := range s {
		switch {
		case r == '\u200d':
			if lastWasEmoji {
				skipJoined = true
			}
		case r == '\ufe0e' || r == '\ufe0f' || (r >= 0x1f3fb && r <= 0x1f3ff):
			continue
		default:
			if skipJoined {
				skipJoined = false
				lastWasEmoji = isEmoji(r)
				continue
			}
			lastWasEmoji = isEmoji(r)
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1f300 && r <= 0x1faff:
		return true
	case r >= 0x2600 && r <= 0x27bf:
		return true
	case r >= 0x2300 && r <= 0x23ff:
		return true
	case r >= 0x2b50 && r <= 0x2b55:
		return true
	case r >= 0x1f1e6 && r <= 0x1f1ff:
		return true
	}
	return false
}

func sanitizeScreenText(s string) string {
	return normalizeScreenText(sanitizeChatText(s))
}

// exactCellWidths is set in the hosted window. Its terminal emulator measures text
// the same way this package does, so the guards for terminals that draw ambiguous
// glyphs two cells wide only cost columns there, and cut text that fits.
var exactCellWidths bool

// fitScreenLines leaves one cell unused on rows containing wide characters:
// even isolated emoji can occupy one or two cells depending on the terminal.
func fitScreenLines(view string, width int) string {
	if width <= 0 || exactCellWidths {
		return view
	}
	rows := strings.Split(view, "\n")
	for i, row := range rows {
		if hasWideRune(row) && ansi.StringWidth(row) >= width {
			if width == 1 {
				rows[i] = ""
				continue
			}
			if ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " ")) < width {
				rows[i] = ansi.Truncate(row, width-1, "")
			} else if ansi.StringWidth(row) > width {
				rows[i] = TruncateVisual(row, width-1)
			}
		}
	}
	return strings.Join(rows, "\n")
}
