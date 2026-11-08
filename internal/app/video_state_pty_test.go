//go:build unix

package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/doctor"
)

// The PTY never answers DA1, so the terminal side fails on its own; the fake mpv
// keeps the other side supported. The screen is read before any key is pressed:
// the reason has to be there from the opening, with its way out.
func TestVideoConfigStatesMissingSupportAtOpening_PTY(t *testing.T) {
	for _, tc := range []struct {
		name, locale string
		want         []string
	}{
		{"en", "en_US.UTF-8", []string{"this terminal has no sixel", "foot ttv-tui"}},
		{"pt-BR", "pt_BR.UTF-8", []string{"terminal sem sixel", "foot ttv-tui"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := startTUIOnPTY(t,
				"TTV_APP_CONFIG_BODY=video = \"sixel\"\n",
				"TTV_APP_MPV_VO=  sixel  terminal graphics using sixels\n",
				"LANG="+tc.locale, "LC_ALL=", "LC_MESSAGES=")
			p.waitScreen(0, tc.want...)
		})
	}
}

// The real doctor on a real PTY: it asks once, reads the answer, reports and
// leaves the screen alone.
func TestDoctorQueriesTheTerminal_PTY(t *testing.T) {
	for _, tc := range []struct {
		name, reply, want string
	}{
		{"sixel", da1Answer, "Sixel in terminal: YES"},
		{"no sixel", "\x1b[?62;22c", "Sixel in terminal: NO ("},
		{"silent", "", "did not answer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
			t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
			t.Setenv("PATH", dir)
			t.Setenv("TMUX", "")
			master, slave := openPTY(t)
			stopped := make(chan struct{})
			closePTYAfter(t, master, slave, stopped)
			var output []byte
			go func() {
				defer close(stopped)
				deadline := time.Now().Add(2500 * time.Millisecond)
				buf := make([]byte, 4096)
				answered := false
				for !bytes.Contains(output, []byte("not tested.")) {
					n, err := readPTY(master, buf, deadline)
					output = append(output, buf[:n]...)
					if !answered && bytes.Contains(output, []byte("\x1b[c")) {
						answered = true
						if tc.reply != "" {
							_, _ = master.Write([]byte(tc.reply))
						}
					}
					if err != nil {
						return
					}
				}
			}()
			start := time.Now()
			doctor.Run(slave, doctor.Options{Lang: "en", Terminal: TerminalProbe(slave, slave)})
			if took := time.Since(start); took > 2*time.Second {
				t.Fatalf("doctor took %s", took)
			}
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("report never reached the terminal")
			}
			text := string(output)
			if !strings.Contains(text, tc.want) {
				t.Fatalf("report lacks %q:\n%q", tc.want, text)
			}
			for _, forbidden := range []string{"\x1b[?1049h", "\x1b[?25l"} {
				if strings.Contains(text, forbidden) {
					t.Fatalf("doctor drove the screen with %q", forbidden)
				}
			}
			if strings.Count(text, "\x1b[c") != 1 {
				t.Fatalf("queries = %d, want 1: %q", strings.Count(text, "\x1b[c"), text)
			}
		})
	}
}
