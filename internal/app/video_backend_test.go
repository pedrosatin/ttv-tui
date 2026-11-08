package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

func TestVideoBackendFlagWinsOverConfig(t *testing.T) {
	on := config.Config{Video: config.VideoSixel}
	for _, tc := range []struct {
		name string
		opts Options
		cfg  config.Config
		want string
	}{
		{"neither", Options{}, config.Config{}, ""},
		{"config only", Options{}, on, "sixel"},
		{"flag only", Options{Video: "sixel"}, config.Config{}, "sixel"},
		{"flag given without VideoSet", Options{Video: "sixel"}, on, "sixel"},
		{"empty flag turns the config off", Options{VideoSet: true}, on, ""},
		{"empty flag with nothing to turn off", Options{VideoSet: true}, config.Config{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := videoBackend(tc.opts, tc.cfg); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTerminalProbeNeedsBothStreamsToBeTerminals(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	if TerminalProbe(null, file) != nil || TerminalProbe(file, file) != nil || TerminalProbe(null, null) != nil {
		t.Fatal("a redirected stream must not be queried")
	}
}
