package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFontPoints(t *testing.T) {
	for _, raw := range []string{"NaN", "Inf", "-1", "1e9", "", "5", "73"} {
		if got := fontPoints(raw); got != 9 {
			t.Errorf("%s: %v", raw, got)
		}
	}
	if fontPoints("12") != 12 {
		t.Fatal("valid size rejected")
	}
}

func TestTUIEnvironment(t *testing.T) {
	got := tuiEnvironment([]string{"HOME=/isolated", "TTV_WINDOW_TEST=/test", "PATH=/bin"})
	if !reflect.DeepEqual(got, []string{"HOME=/isolated", "PATH=/bin"}) {
		t.Fatal(got)
	}
}

func TestThemeInvalidPalette(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(os.Getenv("HOME"), ".local/state/omarchy/current/theme")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foot.ini"), []byte("regularX=ffffff\nbrightZ=ffffff\nregular8=ffffff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := palette
	loadTheme()
	if palette != before {
		t.Fatal("invalid indices changed palette")
	}
}

func TestFindMpvRelativePATH(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(mpvNames[0], []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findMpv(".", ""); got != "" {
		t.Fatal(got)
	}
}
