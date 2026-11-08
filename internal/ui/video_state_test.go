package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var videoFailureReasons = []string{"video.no_terminal", "video.no_mpv", "video.no_both", "video.tmux", "video.unsupported_platform"}

func resized(t *testing.T, m Model, width, height int) Model {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

// stateText joins the wrapped rows of the state line. The wrap may split a word
// and adds an indent, so it compares with its spaces gone (see noSpaces).
func stateText(view string, rows int) string {
	lines := strings.Split(ansi.Strip(view), "\n")
	parts := make([]string, 0, rows)
	for _, line := range lines[:rows] {
		parts = append(parts, strings.TrimPrefix(strings.TrimSpace(line), "!"))
	}
	return noSpaces(strings.Join(parts, ""))
}

func noSpaces(s string) string { return strings.ReplaceAll(s, " ", "") }

func failingVideoModel(t *testing.T, lang, reason string) Model {
	t.Helper()
	m, _, _ := setupTwoPanelModel(t)
	m.catalog, _ = i18n.New(lang)
	WithVideo(true, reason)(&m)
	return m
}

func TestVideoStateLineOnOpening(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, reason := range videoFailureReasons {
			t.Run(lang+"/"+reason, func(t *testing.T) {
				m := resized(t, failingVideoModel(t, lang, reason), 80, 24)
				want := m.t(reason)
				if m.videoStateRows == 0 {
					t.Fatal("no row reserved")
				}
				view := m.View()
				if got := stateText(view, m.videoStateRows); got != noSpaces(want) {
					t.Fatalf("state line = %q, want %q", got, want)
				}
				if rows := strings.Split(view, "\n"); len(rows) != 24 {
					t.Fatalf("frame has %d rows, want 24", len(rows))
				}
			})
		}
	}
}

// Bubble Tea draws once before the first size arrives: the line is there too.
func TestVideoStateLineBeforeFirstResize(t *testing.T) {
	m := failingVideoModel(t, i18n.LangEN, "video.no_mpv")
	if !strings.Contains(ansi.Strip(m.View()), m.t("video.no_mpv")) {
		t.Fatalf("missing before the first resize:\n%s", ansi.Strip(m.View()))
	}
}

func TestVideoStateLineNamesTheSideAndTheStep(t *testing.T) {
	for _, tc := range []struct{ lang, reason, side, step string }{
		{i18n.LangEN, "video.no_terminal", "terminal", "foot ttv-tui"},
		{i18n.LangEN, "video.no_mpv", "mpv", "--vo=sixel"},
		{i18n.LangEN, "video.no_both", "terminal", "--vo=sixel"},
		{i18n.LangPTBR, "video.no_terminal", "terminal", "foot ttv-tui"},
		{i18n.LangPTBR, "video.no_mpv", "mpv", "--vo=sixel"},
		{i18n.LangPTBR, "video.no_both", "terminal", "--vo=sixel"},
	} {
		m := failingVideoModel(t, tc.lang, tc.reason)
		text := m.t(tc.reason)
		if !strings.Contains(text, tc.side) || !strings.Contains(text, tc.step) {
			t.Fatalf("%s %s: %q lacks the side or the step", tc.lang, tc.reason, text)
		}
	}
}

// It is a state: nothing that replaces a bar notice may take it away.
func TestVideoStateLinePersists(t *testing.T) {
	m := resized(t, failingVideoModel(t, i18n.LangEN, "video.no_terminal"), 100, 24)
	want := m.t("video.no_terminal")
	has := func(step string) {
		t.Helper()
		if got := stateText(m.View(), m.videoStateRows); got != noSpaces(want) {
			t.Fatalf("%s: state line = %q", step, got)
		}
	}
	has("opening")
	m.warnStatus("transient notice")
	has("notice")
	m.clearStatusNotice()
	has("notice gone")
	for range 5 {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
		has("layout cycle")
	}
	m = resized(t, m, 60, 20)
	want = m.t("video.no_terminal")
	has("resize")
	m.playerState = PlayerStateError
	has("player error")
}

func TestVideoStateLineKeepsTheFrameInsideTheTerminal(t *testing.T) {
	for _, width := range []int{20, 40, 80, 120} {
		for _, height := range []int{1, 2, 3, 4, 7, 24} {
			m := resized(t, failingVideoModel(t, i18n.LangPTBR, "video.no_both"), width, height)
			rows := strings.Split(m.View(), "\n")
			if len(rows) != height {
				t.Fatalf("%dx%d: %d rows", width, height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("%dx%d: row too wide: %q", width, height, row)
				}
			}
			if height > 1 && m.videoStateRows == 0 {
				t.Fatalf("%dx%d: no state line", width, height)
			}
			if !strings.Contains(ansi.Strip(rows[height-1]), "Ctrl+Q") && width >= 80 {
				t.Fatalf("%dx%d: status bar is not the last row: %q", width, height, rows[height-1])
			}
		}
	}
}

func TestNoVideoStateLineWhenOffOrSupported(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested bool
		reason    string
	}{
		{"off", false, ""},
		{"supported", true, ""},
		{"reason without request", false, "video.no_terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			WithVideo(tc.requested, tc.reason)(&m)
			m = resized(t, m, 100, 24)
			if m.videoStateRows != 0 || m.Height() != 24 {
				t.Fatalf("rows=%d height=%d", m.videoStateRows, m.Height())
			}
			if strings.Contains(ansi.Strip(m.View()), m.t("video.no_terminal")) {
				t.Fatal("state line without a failed request")
			}
		})
	}
}

// The way to turn the mode on no longer depends on knowing the flag.
func TestVideoEnableHint(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog, _ = i18n.New(lang)
			m = resized(t, m, 200, 40)
			m.helpVisible = true
			if help := strings.Join(m.renderHelpLines(200), "\n"); !strings.Contains(help, `video = "sixel"`) || !strings.Contains(help, "--video=sixel") || !strings.Contains(help, m.t("help.video_enable")) {
				t.Fatalf("help has no way to turn it on:\n%s", help)
			}
			m.helpVisible = false
			for range 4 {
				m = sendLayoutKey(t, m, tea.KeyCtrlW)
			}
			if m.LayoutMode() != LayoutSplit {
				t.Fatalf("layout = %s", m.LayoutMode())
			}
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("video.enable_hint")) {
				t.Fatalf("bar has no way to turn it on: %q", bar)
			}
			m = sendLayoutKey(t, m, tea.KeyCtrlW)
			if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, m.t("video.enable_hint")) {
				t.Fatalf("hint outside the step where the mode would come: %q", bar)
			}
		})
	}
}

func TestVideoEnableHintGoneOnceRequested(t *testing.T) {
	for _, reason := range []string{"", "video.no_terminal"} {
		m, _, _ := setupTwoPanelModel(t)
		WithVideo(true, reason)(&m)
		m = resized(t, m, 200, 40)
		m.helpVisible = true
		if strings.Contains(strings.Join(m.renderHelpLines(200), "\n"), m.t("help.video_enable")) {
			t.Fatalf("reason %q: help still explains how to turn it on", reason)
		}
		m.helpVisible = false
		for range 4 {
			m = sendLayoutKey(t, m, tea.KeyCtrlW)
		}
		if strings.Contains(ansi.Strip(m.StatusBar()), m.t("video.enable_hint")) {
			t.Fatalf("reason %q: bar still explains how to turn it on", reason)
		}
	}
}
