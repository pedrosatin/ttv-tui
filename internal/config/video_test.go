package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadBody(t *testing.T, body string) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("a video value must never break the config: %v", err)
	}
	return cfg
}

func TestVideoKey(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"absent", "min_viewers = 5\n", ""},
		{"sixel", "video = \"sixel\"\n", VideoSixel},
		{"case and space", "video = \" SIXEL \"\n", VideoSixel},
		{"ui section", "[ui]\nvideo = \"sixel\"\n", VideoSixel},
		{"video section", "[video]\nbackend = \"sixel\"\n", VideoSixel},
		{"dotted key", "video.backend = \"sixel\"\n", VideoSixel},
		{"empty is off", "video = \"\"\n", ""},
		{"unknown backend", "video = \"kitty\"\n", ""},
		{"not a string", "video = true\n", ""},
		{"unquoted", "video = sixel\n", ""},
		{"no value", "video =\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := loadBody(t, tc.body).Video; got != tc.want {
				t.Fatalf("video = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBadVideoValueKeepsTheOtherKeys(t *testing.T) {
	cfg := loadBody(t, "video = 7\nmin_viewers = 12\nvideo_chat_rows = 5\n")
	if cfg.Video != "" || cfg.MinViewers != 12 || cfg.VideoChatRows != 5 {
		t.Fatalf("config damaged by a bad video value: %+v", cfg)
	}
}

// The app rewrites the whole file on login, --lang and filter changes: a key
// missing from the writer would turn the mode off without the user noticing.
func TestVideoKeySurvivesSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := SaveTo(path, Config{Video: VideoSixel, MinViewers: 3}); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil || cfg.Video != VideoSixel || cfg.MinViewers != 3 {
		t.Fatalf("round trip = %+v, %v", cfg, err)
	}
	if err := SaveTo(path, Config{Video: "kitty"}); err != nil {
		t.Fatal(err)
	}
	if cfg, err = LoadFrom(path); err != nil || cfg.Video != "" {
		t.Fatalf("unknown backend saved: %+v, %v", cfg, err)
	}
}
