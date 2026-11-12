//go:build darwin

package main

// systemFonts looks in the macOS system folders: there is no fontconfig.
func systemFonts() fontFiles {
	return fontFiles{
		regular: []string{
			"/System/Library/Fonts/SFNSMono.ttf",
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/Monaco.ttf",
		},
		bold: []string{"/Library/Fonts/SF-Mono-Bold.otf"},
		fallback: []string{
			"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
			"/System/Library/Fonts/AppleSDGothicNeo.ttc",
			"/System/Library/Fonts/Apple Symbols.ttf",
			"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
		},
	}
}
