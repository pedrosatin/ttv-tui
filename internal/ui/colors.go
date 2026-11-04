package ui

import (
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
	ansiMagenta      = lipgloss.ANSIColor(5)
	ansiCyan         = lipgloss.ANSIColor(6)
	ansiBrightBlack  = lipgloss.ANSIColor(8) // dim/gray on most themes
	ansiBrightYellow = lipgloss.ANSIColor(11)
	ansiBrightCyan   = lipgloss.ANSIColor(14)
)

var (
	colorMu     sync.RWMutex
	colorsOn    bool
	colorInited bool
)

// configureOrientationColors decides whether semantic foreground colors are
// painted. NO_COLOR, CLICOLOR=0 and TERM=dumb turn colors off. Bold/reverse
// stay available: when the env disables color, the lipgloss profile is forced
// to ANSI so those attributes are not stripped by the Ascii profile.
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
	zero := string('0')
	if os.Getenv("CLICOLOR") == zero {
		force := os.Getenv("CLICOLOR_FORCE")
		if force == "" || force == zero {
			return true
		}
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

func withForeground(style lipgloss.Style, color lipgloss.TerminalColor) lipgloss.Style {
	if !colorsEnabled() {
		return style
	}
	return style.Foreground(color)
}

func styleFocus(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiBrightCyan).Render(s)
}

func styleHighlight(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiBrightCyan).Render(s)
}

func styleFavorite(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiYellow).Render(s)
}

func styleLive(s string) string {
	return withForeground(lipgloss.NewStyle(), ansiGreen).Render(s)
}

func stylePlayingChannel(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiGreen).Render(s)
}

func styleOffline(s string) string {
	return withForeground(lipgloss.NewStyle().Faint(true), ansiRed).Render(s)
}

func styleUnverified(s string) string {
	return withForeground(lipgloss.NewStyle().Faint(true), ansiBrightBlack).Render(s)
}

func styleWarning(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiYellow).Render(s)
}

func styleError(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiRed).Render(s)
}

func styleSystem(s string) string {
	return withForeground(lipgloss.NewStyle().Faint(true).Italic(true), ansiMagenta).Render(s)
}

func styleNotice(s string) string {
	return withForeground(lipgloss.NewStyle().Faint(true), ansiMagenta).Render(s)
}

func styleReject(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true), ansiRed).Render(s)
}

func styleMention(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true).Reverse(true), ansiBrightYellow).Render(s)
}

func styleChatHighlight(s string) string {
	return withForeground(lipgloss.NewStyle().Bold(true).Reverse(true), ansiBrightYellow).Render(s)
}

// stylePlayerState colors a player status string by PlayerState.
func stylePlayerState(state PlayerState, s string) string {
	if s == "" {
		return s
	}
	switch state {
	case PlayerStateStarting:
		return withForeground(lipgloss.NewStyle().Faint(true), ansiBrightBlack).Render(s)
	case PlayerStatePlaying:
		return withForeground(lipgloss.NewStyle(), ansiGreen).Render(s)
	case PlayerStatePaused:
		return withForeground(lipgloss.NewStyle(), ansiYellow).Render(s)
	case PlayerStateBreak:
		return withForeground(lipgloss.NewStyle(), ansiCyan).Render(s)
	case PlayerStateError:
		return styleError(s)
	default:
		return s
	}
}

// styleStatusPrimary colors the status-bar lead message for the active state.
// The branch order mirrors statusParts so the painted segment matches the lead.
func (m Model) styleStatusPrimary(primary string) string {
	switch {
	case m.ctrlCNotice != "":
		return primary
	case m.err != nil:
		return styleError(primary)
	case m.searching, m.filtering:
		return primary
	case m.authNotice != "", m.corruptNotice != "":
		return styleWarning(primary)
	case m.copyNotice != "", m.statusNotice != "":
		return primary
	case m.playerWindowNoticeShown():
		return stylePlayerState(PlayerStatePlaying, primary)
	case m.playerState == PlayerStateError:
		return styleError(primary)
	case m.playerState != PlayerStateNone && m.playerState != PlayerStateEnded:
		return stylePlayerState(m.playerState, primary)
	case m.playerState == PlayerStateEnded:
		return withForeground(lipgloss.NewStyle().Faint(true), ansiBrightBlack).Render(primary)
	default:
		return primary
	}
}

// mentionsSelf reports whether body addresses the logged-in chat username.
func (m Model) mentionsSelf(body string) bool {
	user := strings.TrimSpace(m.chatPanel.username)
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
