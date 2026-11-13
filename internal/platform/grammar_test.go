package platform

import (
	"strings"
	"testing"
)

func TestValidTwitchLogin(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"gaules", true},
		{"xqc", true},
		{"a", true},
		{"user_123", true},
		{strings.Repeat("a", 25), true},
		{"", false},
		{strings.Repeat("a", 26), false},
		{"Gaules", false},
		{"a-b", false},
		{"a.b", false},
		{"a b", false},
		{"a,#b", false},
		{"a\r\nb", false},
		{"a\x00", false},
		{"a\x1b", false},
		{"..", false},
		{"ação", false},
		{"#canal", false},
	}
	for _, tc := range tests {
		if got := ValidTwitchLogin(tc.in); got != tc.want {
			t.Errorf("ValidTwitchLogin(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidKickSlug(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"xqc", true},
		{"ice-poseidon", true},
		{"user_1", true},
		{strings.Repeat("a", 64), true},
		{"", false},
		{strings.Repeat("a", 65), false},
		{".", false},
		{"..", false},
		{"a..", false},
		{"a.b", false},
		{"Xqc", false},
		{"a/b", false},
		{"a?b", false},
		{"a\x1b", false},
		{"a\nb", false},
		{"a b", false},
	}
	for _, tc := range tests {
		if got := ValidKickSlug(tc.in); got != tc.want {
			t.Errorf("ValidKickSlug(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidIdentifier(t *testing.T) {
	tests := []struct {
		plat, in string
		want     bool
	}{
		{"", "gaules", true},
		{Twitch, "a-b", false},
		{"Kick", "a-b", true},
		{Kick, "..", false},
		{YouTube, "dQw4w9WgXcQ", true},
		{YouTube, "a\x07", false},
		{"vimeo.com", "123/abc", true},
		{"vimeo.com", "", false},
		{"vimeo.com", "a\u0085b", false},
	}
	for _, tc := range tests {
		if got := ValidIdentifier(tc.plat, tc.in); got != tc.want {
			t.Errorf("ValidIdentifier(%q, %q) = %v, want %v", tc.plat, tc.in, got, tc.want)
		}
	}
}

func TestHasControl(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"gaules", false},
		{"ação ao vivo", false},
		{"a\x00", true},
		{"a\x07", true},
		{"a\x1b[2J", true},
		{"a\r\n", true},
		{"a\x7f", true},
		{"a\u0085", true},
		{"a\u009b", true},
	}
	for _, tc := range tests {
		if got := HasControl(tc.in); got != tc.want {
			t.Errorf("HasControl(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLowerASCII(t *testing.T) {
	tests := map[string]string{
		"":         "",
		"Gaules":   "gaules",
		"XQC_1-a":  "xqc_1-a",
		"\u212a":   "\u212a",
		"AÇÃO":     "aÇÃo",
		"\u0130Ab": "\u0130ab",
	}
	for in, want := range tests {
		if got := LowerASCII(in); got != want {
			t.Errorf("LowerASCII(%q) = %q, want %q", in, got, want)
		}
	}
}
