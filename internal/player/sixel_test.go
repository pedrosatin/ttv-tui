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
	if got := DetectSixel(context.Background(), SixelProbe{Terminal: func(context.Context) ([]byte, error) { return nil, context.DeadlineExceeded }}); got != "video.no_terminal" {
		t.Fatal(got)
	}
}
