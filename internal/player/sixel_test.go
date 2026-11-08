package player

import (
	"context"
	"errors"
	"testing"
)

func TestDetectSixel(t *testing.T) {
	for _, tc := range []struct {
		name, reply, output, want string
		fail                      bool
	}{
		{"sixel", "\x1b[?62;4;22c", "Available video outputs:\n sixel Sixel terminal output\n", "", false},
		{"no sixel", "\x1b[?62;22c", "sixel output", "video.no_terminal", false},
		{"no reply", "", "sixel output", "video.no_terminal", false},
		{"secondary attributes", "\x1b[>62;4c", "sixel output", "video.no_terminal", false},
		{"class is not capability", "\x1b[?4;22c", "sixel output", "video.no_terminal", false},
		{"no mpv vo", "\x1b[?62;4c", "gpu GPU output\n", "video.no_mpv", false},
		{"not substring", "\x1b[?62;4c", "gpu not sixel\n", "video.no_mpv", false},
		{"mpv failed", "\x1b[?62;4c", "sixel output", "video.no_mpv", true},
		{"neither side", "\x1b[?62;22c", "gpu GPU output\n", "video.no_both", false},
		{"silent terminal, mpv failed", "", "sixel output", "video.no_both", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := SixelProbe{
				Terminal: func(context.Context) ([]byte, error) { return []byte(tc.reply), nil },
				Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
					if name != "mpv" || len(args) != 2 || args[1] != "--vo=help" {
						t.Fatalf("unexpected command %s %v", name, args)
					}
					if tc.fail {
						return nil, errors.New("missing executable")
					}
					return []byte(tc.output), nil
				},
			}
			if got := DetectSixel(context.Background(), probe); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	if got := DetectSixel(context.Background(), SixelProbe{
		Terminal: func(context.Context) ([]byte, error) { return nil, context.DeadlineExceeded },
		Command:  func(context.Context, string, ...string) ([]byte, error) { return []byte("sixel output"), nil },
	}); got != "video.no_terminal" {
		t.Fatal(got)
	}
}

func TestParseCellReply(t *testing.T) {
	for _, tc := range []struct {
		name          string
		reply         string
		width, height int
		ok            bool
	}{
		{"alone", "\x1b[6;12;6t", 6, 12, true},
		{"before the DA1 reply", "\x1b[6;20;10t\x1b[?62;4c", 10, 20, true},
		{"after stray input", "x\x1b[6;16;8t", 8, 16, true},
		{"8-bit CSI", "\x9b6;16;8t", 8, 16, true},
		{"only DA1", "\x1b[?62;4c", 0, 0, false},
		{"empty", "", 0, 0, false},
		{"zeros", "\x1b[6;0;0t", 0, 0, false},
		{"zero height", "\x1b[6;0;8t", 0, 0, false},
		{"text area report", "\x1b[4;540;960t", 0, 0, false},
		{"cut short", "\x1b[6;12;", 0, 0, false},
		{"absurd", "\x1b[6;99999;6t", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			width, height, ok := ParseCellReply([]byte(tc.reply))
			if width != tc.width || height != tc.height || ok != tc.ok {
				t.Fatalf("got %dx%d ok=%v", width, height, ok)
			}
		})
	}
}
