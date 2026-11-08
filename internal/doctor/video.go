package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

// terminalQueryLimit bounds the DA1 query, so a terminal that stays silent costs
// the report under a second.
const terminalQueryLimit = time.Second

type sixelState int

const (
	sixelUntested sixelState = iota
	sixelYes
	sixelNo
)

// sixelSide is what one side reports. reason is a catalog key, empty on success.
type sixelSide struct {
	state  sixelState
	reason string
}

func (s sixelSide) text(cat *i18n.Catalog) string {
	var value string
	switch s.state {
	case sixelYes:
		value = cat.T("doctor.yes")
	case sixelNo:
		value = cat.T("doctor.no")
	default:
		value = cat.T("doctor.untested")
	}
	if s.reason == "" {
		return value
	}
	return fmt.Sprintf("%s (%s)", value, cat.T(s.reason))
}

// videoEnv is everything the verdict depends on, so each combination can be tested.
type videoEnv struct {
	goos       string
	tmux       bool
	terminal   func(context.Context) ([]byte, error)
	configured bool
}

type videoReport struct {
	terminal, mpv sixelSide
	verdict       string
	steps         []string
}

// nextStep joins the steps, the terminal's first: every one of them is needed.
func (r videoReport) nextStep(cat *i18n.Catalog) string {
	parts := make([]string, len(r.steps))
	for i, step := range r.steps {
		parts[i] = cat.T(step)
	}
	return strings.Join(parts, "; ")
}

// checkVideo tests both sides and decides whether the mode can run. It mirrors
// the checks the app makes when it starts, without touching the screen.
func checkVideo(ctx context.Context, env videoEnv) videoReport {
	report := videoReport{terminal: checkTerminal(ctx, env), mpv: checkMpv(ctx)}
	if env.goos == "windows" {
		report.verdict = "doctor.video_unavailable"
		report.steps = []string{"doctor.next_windows"}
		return report
	}
	switch {
	case report.terminal.state == sixelNo:
		report.steps = append(report.steps, "doctor.next_terminal")
	case env.tmux:
		report.steps = append(report.steps, "doctor.next_tmux")
	case report.terminal.state == sixelUntested:
		report.steps = append(report.steps, "doctor.next_tty")
	}
	if report.mpv.state == sixelNo {
		report.steps = append(report.steps, "doctor.next_mpv")
	}
	switch {
	case report.terminal.state == sixelYes && report.mpv.state == sixelYes && env.configured:
		report.verdict = "doctor.video_on"
	case report.terminal.state == sixelYes && report.mpv.state == sixelYes:
		report.verdict = "doctor.video_off"
		report.steps = []string{"doctor.next_enable"}
	case report.terminal.state == sixelUntested && !env.tmux && report.mpv.state == sixelYes:
		report.verdict = "doctor.video_unconfirmed"
	default:
		report.verdict = "doctor.video_unavailable"
	}
	return report
}

func checkTerminal(ctx context.Context, env videoEnv) sixelSide {
	switch {
	case env.goos == "windows":
		return sixelSide{sixelUntested, "doctor.reason_windows"}
	case env.tmux:
		return sixelSide{sixelUntested, "doctor.reason_tmux"}
	case env.terminal == nil:
		return sixelSide{sixelUntested, "doctor.reason_no_tty"}
	}
	ctx, cancel := context.WithTimeout(ctx, terminalQueryLimit)
	defer cancel()
	reply, err := env.terminal(ctx)
	if err != nil || len(reply) == 0 {
		// Silence is not evidence of support, and it is not a reason to retry.
		return sixelSide{sixelNo, "doctor.reason_no_reply"}
	}
	if !player.TerminalHasSixel(reply) {
		return sixelSide{sixelNo, "doctor.reason_no_sixel"}
	}
	return sixelSide{sixelYes, ""}
}

func checkMpv(ctx context.Context) sixelSide {
	path, err := exec.LookPath("mpv")
	if err != nil {
		return sixelSide{sixelNo, "doctor.reason_mpv_missing"}
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--no-config", "--vo=help")
	cmd.WaitDelay = 100 * time.Millisecond
	var output cappedOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return sixelSide{sixelNo, "doctor.reason_mpv_failed"}
	}
	if !player.HasSixelVO(output.Bytes()) {
		return sixelSide{sixelNo, "doctor.reason_mpv_no_vo"}
	}
	return sixelSide{sixelYes, ""}
}

// emulatorMarkers name the emulator from what it exports. TERM alone often says
// only xterm-256color. An empty name means the variable holds it.
var emulatorMarkers = []struct{ env, name string }{
	{"TERM_PROGRAM", ""},
	{"KITTY_WINDOW_ID", "kitty"},
	{"ALACRITTY_WINDOW_ID", "Alacritty"},
	{"GHOSTTY_RESOURCES_DIR", "Ghostty"},
	{"WEZTERM_PANE", "WezTerm"},
	{"KONSOLE_VERSION", "Konsole"},
	{"WT_SESSION", "Windows Terminal"},
	{"VTE_VERSION", "VTE"},
}

// describeTerminal returns TERM plus the emulator when one can be named.
func describeTerminal(cat *i18n.Catalog) string {
	term := printableEnv(os.Getenv("TERM"))
	if term == "" {
		term = cat.T("doctor.term_unset")
	}
	for _, marker := range emulatorMarkers {
		value := printableEnv(os.Getenv(marker.env))
		if value == "" {
			continue
		}
		if marker.name != "" {
			value = marker.name
		}
		if value == term {
			return term
		}
		return fmt.Sprintf("%s (%s)", term, value)
	}
	return term
}

// printableEnv keeps an environment value safe to print: the report goes to a
// terminal, and these values are not ours.
func printableEnv(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 40 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}
