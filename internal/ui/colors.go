package ui

import (
	"hash/fnv"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ANSI 16-color indices used for orientation. Numbers are the terminal palette
// slots (0–15), so the user's theme controls the actual hues.
const (
	ansiRed          = lipgloss.ANSIColor(1)
	ansiGreen        = lipgloss.ANSIColor(2)
	ansiYellow       = lipgloss.ANSIColor(3)
	ansiBlue         = lipgloss.ANSIColor(4)
	ansiMagenta      = lipgloss.ANSIColor(5)
	ansiCyan         = lipgloss.ANSIColor(6)
	ansiWhite        = lipgloss.ANSIColor(7)
	ansiBrightYellow = lipgloss.ANSIColor(11)
	ansiBrightBlue   = lipgloss.ANSIColor(12)
	ansiBrightWhite  = lipgloss.ANSIColor(15)
)

// nickPalette is the only foreground set allowed for chat nicks (SGR 34/94/37/97).
// Semantic colors (red/green/yellow/magenta/cyan) stay reserved for UI state.
var nickPalette = []lipgloss.TerminalColor{
	ansiBlue,
	ansiBrightBlue,
	ansiWhite,
	ansiBrightWhite,
}

var (
	colorMu     sync.RWMutex
	colorsOn    bool
	colorInited bool
)

// configureOrientationColors decides whether semantic foreground colors are
// painted. NO_COLOR, CLICOLOR=0 and TERM=dumb turn colors off. Bold/reverse/
// underline stay available: when the env disables color, the lipgloss profile
// is forced to ANSI so those attributes are not stripped by the Ascii profile.
func configureOrientationColors() {
	colorMu.Lock()
	defer colorMu.Unlock()
	disabled := envDisablesColor()
	colorsOn = !disabled
	colorInited = true
	if disabled {
		lipgloss.SetColorProfile(termenv.ANSI)
	}
}

func envDisablesColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	if term := os.Getenv("TERM"); term == "dumb" {
		return true
	}
	// CLICOLOR=0 disables color the same way as NO_COLOR, including when
	// CLICOLOR_FORCE is set in the parent environment (ticket 13).
	if os.Getenv("CLICOLOR") == "0" {
		return true
	}
	return false
}

// colorsEnabled reports whether orientation foreground colors should be applied.
func colorsEnabled() bool {
	colorMu.RLock()
	inited := colorInited
	on := colorsOn
	colorMu.RUnlock()
	if !inited {
		configureOrientationColors()
		colorMu.RLock()
		on = colorsOn
		colorMu.RUnlock()
	}
	return on
}

// forceOrientationColorsForTest overrides the color gate and lipgloss profile.
// on=true forces ANSI colors; on=false keeps bold/reverse but paints no
// foreground. Tests must call this before rendering Views that assert SGR.
func forceOrientationColorsForTest(on bool) {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorsOn = on
	colorInited = true
	lipgloss.SetColorProfile(termenv.ANSI)
}

// resetOrientationColorsForTest clears the init gate so the next colorsEnabled
// / configureOrientationColors call re-reads the process environment.
func resetOrientationColorsForTest() {
	colorMu.Lock()
	defer colorMu.Unlock()
	colorInited = false
}

func withForeground(style lipgloss.Style, color lipgloss.TerminalColor) lipgloss.Style {
	if !colorsEnabled() {
		return style
	}
	return style.Foreground(color)
}

func styleFocus(s string) string {
	// Cyan (36) + bold stays readable on light and dark themes; bright cyan
	// (96) washes out on light backgrounds (ticket 14).
	return withForeground(lipgloss.NewStyle().Bold(true), ansiCyan).Render(s)
}

func styleHighlight(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiCyan).Render(s)
}

func styleSelectedRow(s string) string {
	return lipgloss.NewStyle().Bold(true).Reverse(true).Render(s)
}

func styleFavorite(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiYellow).Render(s)
}

func styleLive(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiGreen).Render(s)
}

func stylePlayingChannel(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}

func styleOffline(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiRed).Render(s)
}

func styleUnverified(s string) string {
	// White (37) replaces bright black (90), which disappears on light themes.
	return withForeground(lipgloss.NewStyle(), ansiWhite).Render(s)
}

func styleWarning(s string) string {
	// Warnings stay bold and use magenta (system family), never yellow: yellow
	// means pause in the player and the favorite ★ only.
	return withForeground(lipgloss.NewStyle().Bold(true), ansiMagenta).Render(s)
}

func styleError(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiRed).Render(s)
}

func styleSystem(s string) string {
	return withForeground(lipgloss.NewStyle().Italic(true), ansiMagenta).Render(s)
}

func styleNotice(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiMagenta).Render(s)
}

func styleReject(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiRed).Render(s)
}

func styleMention(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true).Underline(true), ansiBrightWhite).Render(s)
}

func styleChatHighlight(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true).Reverse(true), ansiBrightYellow).Render(s)
}

// styleNick maps a Twitch nick onto the non-semantic blue/white palette.
// Twitch hex is ignored as a hue source so nicks never collide with live/error
// colors; the hash only picks among 34/94/37/97.
func styleNick(name, _ string) string {
	if name == "" {
		return name
	}
	style := lipgloss.NewStyle().Bold(true)
	if !colorsEnabled() {
		return style.Render(name)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	color := nickPalette[int(h.Sum32())%len(nickPalette)]
	return style.Foreground(color).Render(name)
}

// statusTone is what the status bar's lead message reports.
type statusTone int

const (
	toneNeutral statusTone = iota
	toneStarting
	tonePlaying
	tonePaused
	toneBreak
	toneEnded
	toneWarning
	toneError
)

// playerTone maps an active player state onto its status tone.
func playerTone(state PlayerState) statusTone {
	switch state {
	case PlayerStateStarting:
		return toneStarting
	case PlayerStatePlaying:
		return tonePlaying
	case PlayerStatePaused:
		return tonePaused
	case PlayerStateBreak:
		return toneBreak
	default:
		return toneNeutral
	}
}

// glyph is the shape that tells this state apart without color, or "" when the
// message's own words already do.
func (t statusTone) glyph() string {
	switch t {
	case tonePlaying:
		return "▶"
	case tonePaused:
		return "‖"
	case toneWarning:
		return "!"
	case toneError:
		return "✖"
	default:
		return ""
	}
}

// styleStatusBar is the plain bar. Every run of the bar starts from it.
func styleStatusBar() lipgloss.Style {
	return lipgloss.NewStyle().Reverse(true)
}

// style paints this tone on the bar. It keeps the reverse: a run that dropped it
// would punch a hole in the background.
func (t statusTone) style() lipgloss.Style {
	bar := styleStatusBar()
	switch t {
	case toneStarting:
		// White (37) + bold: bright black (90) vanishes on light themes.
		return withForeground(bar.Bold(true), ansiWhite)
	case tonePlaying:
		return withForeground(bar, ansiGreen)
	case tonePaused:
		return withForeground(bar, ansiYellow)
	case toneBreak:
		return withForeground(bar, ansiCyan)
	case toneEnded:
		return withForeground(bar.Bold(true), ansiWhite)
	case toneWarning:
		// Magenta (system family), never yellow: yellow means pause and ★ only.
		return withForeground(bar.Bold(true), ansiMagenta)
	case toneError:
		return withForeground(bar.Bold(true), ansiRed)
	default:
		return bar
	}
}

// mentionsSelf reports whether body addresses the logged-in chat username.
func (m Model) mentionsSelf(body string) bool {
	user := strings.TrimSpace(m.chat.panel.username)
	if user == "" || body == "" {
		return false
	}
	lowerBody := strings.ToLower(body)
	lowerUser := strings.ToLower(user)
	at := string(rune('@'))
	if strings.Contains(lowerBody, at+lowerUser) {
		return true
	}
	// Bare login as its own token (spaces/punctuation around it).
	for _, part := range strings.FieldsFunc(lowerBody, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == ':' || r == '!' || r == '?' || r == '.' || r == '(' || r == ')'
	}) {
		if part == lowerUser {
			return true
		}
	}
	return false
}

// colorizeHelpLegend paints the known color terms inside the help.colors line
// so the legend itself demonstrates the palette. Unknown text stays plain.
func colorizeHelpLegend(plain string) string {
	if plain == "" || !colorsEnabled() {
		return plain
	}
	type term struct {
		needle string
		paint  func(string) string
	}
	terms := []term{
		{"amarelo", styleFavorite},
		{"vermelho", styleError},
		{"magenta", styleSystem},
		{"menção", styleMention},
		{"mention", styleMention},
		{"yellow", styleFavorite},
		{"green", styleLive},
		{"verde", styleLive},
		{"ciano", styleFocus},
		{"cyan", styleFocus},
		{"red", styleError},
	}
	lower := strings.ToLower(plain)
	var b strings.Builder
	for len(lower) > 0 {
		bestAt, bestLen := -1, 0
		var paint func(string) string
		for _, t := range terms {
			idx := strings.Index(lower, t.needle)
			if idx < 0 {
				continue
			}
			if bestAt < 0 || idx < bestAt || (idx == bestAt && len(t.needle) > bestLen) {
				bestAt = idx
				bestLen = len(t.needle)
				paint = t.paint
			}
		}
		if bestAt < 0 {
			b.WriteString(plain)
			break
		}
		b.WriteString(plain[:bestAt])
		end := bestAt + bestLen
		b.WriteString(paint(plain[bestAt:end]))
		plain = plain[end:]
		lower = lower[end:]
	}
	return b.String()
}
