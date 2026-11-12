//go:build !windows && !darwin

package main

import (
	"os/exec"
	"strings"
)

// fcMatch resolves a fontconfig pattern to a file; empty when it cannot.
func fcMatch(pattern string) string {
	out, err := exec.Command("fc-match", "-f", "%{file}", pattern).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func systemFonts() fontFiles {
	return fontFiles{
		regular: []string{fcMatch("monospace")},
		bold:    []string{fcMatch("monospace:bold")},
		fallback: []string{
			fcMatch("monospace:lang=ja"),
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansSymbols2-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansSymbols-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansThai-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansArabic-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansHebrew-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansDevanagari-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansMath-Regular.ttf",
		},
	}
}
