package doctor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const (
	withSixelReply = "\x1b[?62;4;22c"
	noSixelReply   = "\x1b[?62;22c"
	voWithSixel    = "Available video outputs:\n  gpu   GPU\n  sixel terminal graphics using sixels\n"
	voNoSixel      = "Available video outputs:\n  gpu   GPU\n  not   sixel is only a word here\n"
)

func clearTerminalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("TMUX", "")
	for _, marker := range emulatorMarkers {
		t.Setenv(marker.env, "")
	}
}

// videoFixture puts a fake mpv on PATH and clears what the host would leak in.
func videoFixture(t *testing.T, vo string) {
	t.Helper()
	base := isolate(t)
	fakeTool(t, base, "streamlink")
	fakeTool(t, base, "mpv")
	t.Setenv("PATH", base)
	t.Setenv("TTV_DOCTOR_FAKE_VO", vo)
	clearTerminalEnv(t)
}

func answering(reply string) func(context.Context) ([]byte, error) {
	return func(context.Context) ([]byte, error) { return []byte(reply), nil }
}

func catalog(t *testing.T, lang string) *i18n.Catalog {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// reportLine returns the value of the report line labeled key, "" when absent.
func reportLine(report string, cat *i18n.Catalog, key string) string {
	prefix := cat.T(key) + ": "
	for line := range strings.SplitSeq(report, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

func TestVideoCombinations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows verdict is covered by TestVideoSpecialEnvironments")
	}
	for _, tc := range []struct {
		name          string
		reply, vo     string
		wantTerminal  string
		wantMpv       string
		verdict       string
		steps         []string
		configEnabled bool
	}{
		{"both, on in config", withSixelReply, voWithSixel, "doctor.yes", "doctor.yes", "doctor.video_on", nil, true},
		{"both, off in config", withSixelReply, voWithSixel, "doctor.yes", "doctor.yes", "doctor.video_off", []string{"doctor.next_enable"}, false},
		{"terminal without sixel", noSixelReply, voWithSixel, "doctor.no", "doctor.yes", "doctor.video_unavailable", []string{"doctor.next_terminal"}, true},
		{"mpv without sixel", withSixelReply, voNoSixel, "doctor.yes", "doctor.no", "doctor.video_unavailable", []string{"doctor.next_mpv"}, true},
		{"neither", noSixelReply, voNoSixel, "doctor.no", "doctor.no", "doctor.video_unavailable", []string{"doctor.next_terminal", "doctor.next_mpv"}, false},
	} {
		for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
			t.Run(tc.name+"/"+lang, func(t *testing.T) {
				videoFixture(t, tc.vo)
				if tc.configEnabled {
					if err := config.Save(config.Config{Video: config.VideoSixel}); err != nil {
						t.Fatal(err)
					}
				}
				var out bytes.Buffer
				code := Run(&out, Options{Version: "test", Lang: lang, Terminal: answering(tc.reply)})
				if code != 0 {
					t.Fatalf("exit = %d: an optional mode must not fail the diagnostic", code)
				}
				cat := catalog(t, lang)
				report := out.String()
				for key, want := range map[string]string{
					"doctor.sixel_terminal": cat.T(tc.wantTerminal),
					"doctor.sixel_mpv":      cat.T(tc.wantMpv),
					"doctor.video_mode":     cat.T(tc.verdict),
				} {
					if !strings.HasPrefix(reportLine(report, cat, key), want) {
						t.Fatalf("%s = %q, want prefix %q\n%s", key, reportLine(report, cat, key), want, report)
					}
				}
				next := reportLine(report, cat, "doctor.next")
				if len(tc.steps) == 0 && next != "" {
					t.Fatalf("no next step expected, got %q", next)
				}
				for _, step := range tc.steps {
					if !strings.Contains(next, cat.T(step)) {
						t.Fatalf("next step %q lacks %q", next, cat.T(step))
					}
				}
			})
		}
	}
}

// expectedPlatform selects the Windows fallback when the terminal cannot be queried.
func expectedPlatform(other string) string {
	if runtime.GOOS == "windows" {
		return "doctor.next_windows"
	}
	return other
}

func TestNextStepWording(t *testing.T) {
	for _, tc := range []struct{ lang, reply, vo, step, reason string }{
		{i18n.LangEN, noSixelReply, voWithSixel, "doctor.next_terminal", "doctor.reason_no_sixel"},
		{i18n.LangPTBR, noSixelReply, voWithSixel, "doctor.next_terminal", "doctor.reason_no_sixel"},
		{i18n.LangEN, withSixelReply, voNoSixel, "doctor.next_mpv", "doctor.reason_mpv_no_vo"},
		{i18n.LangPTBR, withSixelReply, voNoSixel, "doctor.next_mpv", "doctor.reason_mpv_no_vo"},
		{i18n.LangEN, withSixelReply, voWithSixel, "doctor.next_enable", ""},
		{i18n.LangPTBR, withSixelReply, voWithSixel, "doctor.next_enable", ""},
	} {
		videoFixture(t, tc.vo)
		var out bytes.Buffer
		Run(&out, Options{Lang: tc.lang, Terminal: answering(tc.reply)})
		cat := catalog(t, tc.lang)
		next := reportLine(out.String(), cat, "doctor.next")
		if want := cat.T(expectedPlatform(tc.step)); !strings.Contains(next, want) {
			t.Fatalf("%s: next step %q lacks %q", tc.lang, next, want)
		}
		reason := tc.reason
		if runtime.GOOS == "windows" {
			if !strings.Contains(reportLine(out.String(), cat, "doctor.sixel_terminal"), cat.T("doctor.reason_windows")) {
				t.Fatalf("%s: report lacks Windows reason: %s", tc.lang, out.String())
			}
			if reason == "doctor.reason_no_sixel" {
				reason = ""
			}
		}
		if reason != "" && !strings.Contains(out.String(), cat.T(reason)) {
			t.Fatalf("%s: report lacks %s: %s", tc.lang, reason, out.String())
		}
	}
}

func TestVideoReasons(t *testing.T) {
	videoFixture(t, voNoSixel)
	base := filepath.Dir(firstOnPath(t))
	for _, tc := range []struct {
		name     string
		terminal func(context.Context) ([]byte, error)
		reason   string
	}{
		{"announces no sixel", answering(noSixelReply), "doctor.reason_no_sixel"},
		{"silent terminal", answering(""), "doctor.reason_no_reply"},
		{"query error", func(context.Context) ([]byte, error) { return nil, errors.New("boom") }, "doctor.reason_no_reply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkTerminal(context.Background(), videoEnv{goos: "linux", terminal: tc.terminal})
			if got.state != sixelNo || got.reason != tc.reason {
				t.Fatalf("got %+v, want no/%s", got, tc.reason)
			}
		})
	}
	if got := checkMpv(context.Background()); got != (sixelSide{sixelNo, "doctor.reason_mpv_no_vo"}) {
		t.Fatalf("no vo: %+v", got)
	}
	t.Setenv("TTV_DOCTOR_FAKE_VO_FAIL", "1")
	if got := checkMpv(context.Background()); got != (sixelSide{sixelNo, "doctor.reason_mpv_failed"}) {
		t.Fatalf("failing mpv: %+v", got)
	}
	t.Setenv("PATH", filepath.Join(base, "nowhere"))
	if got := checkMpv(context.Background()); got != (sixelSide{sixelNo, "doctor.reason_mpv_missing"}) {
		t.Fatalf("missing mpv: %+v", got)
	}
}

func firstOnPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("PATH"), "mpv")
}

func TestVideoSpecialEnvironments(t *testing.T) {
	videoFixture(t, voWithSixel)
	asked := func(context.Context) ([]byte, error) {
		t.Error("the terminal must not be queried here")
		return nil, nil
	}
	for _, tc := range []struct {
		name    string
		env     videoEnv
		reason  string
		verdict string
		step    string
	}{
		{"no tty", videoEnv{goos: "linux"}, "doctor.reason_no_tty", "doctor.video_unconfirmed", "doctor.next_tty"},
		{"tmux", videoEnv{goos: "linux", tmux: true, terminal: asked}, "doctor.reason_tmux", "doctor.video_unavailable", "doctor.next_tmux"},
		{"windows", videoEnv{goos: "windows", terminal: asked}, "doctor.reason_windows", "doctor.video_unavailable", "doctor.next_windows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkVideo(context.Background(), tc.env)
			if got.terminal.state != sixelUntested || got.terminal.reason != tc.reason || got.verdict != tc.verdict {
				t.Fatalf("got %+v", got)
			}
			if len(got.steps) != 1 || got.steps[0] != tc.step {
				t.Fatalf("steps = %v, want %s", got.steps, tc.step)
			}
		})
	}
}

// A terminal that never answers ends with the deadline, not with a stuck report.
func TestSilentTerminalDoesNotHang(t *testing.T) {
	videoFixture(t, voWithSixel)
	silent := func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan videoReport, 1)
	go func() {
		done <- checkVideo(context.Background(), videoEnv{goos: "linux", terminal: silent})
	}()
	select {
	case report := <-done:
		if report.terminal.reason != "doctor.reason_no_reply" || len(report.steps) == 0 || report.steps[0] != "doctor.next_terminal" {
			t.Fatalf("silent terminal report = %+v", report)
		}
	case <-time.After(3*time.Second + terminalQueryLimit):
		t.Fatal("doctor hung on a silent terminal")
	}
	var out bytes.Buffer
	Run(&out, Options{Lang: i18n.LangEN, Terminal: silent})
	cat := catalog(t, i18n.LangEN)
	reason := "doctor.reason_no_reply"
	if runtime.GOOS == "windows" {
		reason = "doctor.reason_windows"
	}
	if !strings.Contains(reportLine(out.String(), cat, "doctor.sixel_terminal"), cat.T(reason)) ||
		!strings.Contains(reportLine(out.String(), cat, "doctor.next"), cat.T(expectedPlatform("doctor.next_terminal"))) {
		t.Fatalf("report lacks reason or next step: %s", out.String())
	}
}

func TestNoTerminalReportsNotTestedWithoutEscapes(t *testing.T) {
	videoFixture(t, voWithSixel)
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		var out bytes.Buffer
		if code := Run(&out, Options{Lang: lang}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		cat := catalog(t, lang)
		if got := reportLine(out.String(), cat, "doctor.sixel_terminal"); !strings.HasPrefix(got, cat.T("doctor.untested")) {
			t.Fatalf("terminal side = %q", got)
		}
		if strings.Contains(out.String(), "\x1b") {
			t.Fatalf("escape sequence in the report: %q", out.String())
		}
	}
}

func TestDescribeTerminal(t *testing.T) {
	cat := catalog(t, i18n.LangEN)
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"term only", map[string]string{"TERM": "foot"}, "foot"},
		{"term unset", map[string]string{"TERM": ""}, "TERM not set"},
		{"term program", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "iTerm.app"}, "xterm-256color (iTerm.app)"},
		{"marker", map[string]string{"TERM": "xterm-kitty", "KITTY_WINDOW_ID": "1"}, "xterm-kitty (kitty)"},
		{"same name once", map[string]string{"TERM": "ghostty", "TERM_PROGRAM": "ghostty"}, "ghostty"},
		{"no control bytes", map[string]string{"TERM": "foo\x1b[31mbar\n"}, "foo[31mbar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearTerminalEnv(t)
			t.Setenv("TERM", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := describeTerminal(cat); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Every key the report can print has to resolve in both catalogs: T returns the
// key itself when one is missing.
func TestVideoReportKeysExistInBothLanguages(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := catalog(t, lang)
		for _, key := range []string{
			"doctor.terminal", "doctor.term_unset", "doctor.sixel_terminal", "doctor.sixel_mpv", "doctor.untested",
			"doctor.reason_no_tty", "doctor.reason_tmux", "doctor.reason_windows", "doctor.reason_no_reply",
			"doctor.reason_no_sixel", "doctor.reason_mpv_missing", "doctor.reason_mpv_failed", "doctor.reason_mpv_no_vo",
			"doctor.video_mode", "doctor.video_on", "doctor.video_off", "doctor.video_unavailable", "doctor.video_unconfirmed",
			"doctor.next", "doctor.next_enable", "doctor.next_terminal", "doctor.next_mpv", "doctor.next_tty",
			"doctor.next_tmux", "doctor.next_windows",
		} {
			if cat.T(key) == key {
				t.Errorf("%s: missing %s", lang, key)
			}
		}
	}
}

func TestMpvProbeHonorsDeadline(t *testing.T) {
	videoFixture(t, voWithSixel)
	t.Setenv("TTV_DOCTOR_FAKE_VO_WAIT", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := checkMpv(ctx)
	if got != (sixelSide{sixelNo, "doctor.reason_mpv_failed"}) {
		t.Fatalf("timed-out mpv = %+v", got)
	}
	if time.Since(start) > time.Second {
		t.Fatal("mpv deadline was not respected")
	}
}
