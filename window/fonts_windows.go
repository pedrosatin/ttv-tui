//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// systemFonts looks in %WINDIR%\Fonts: Windows has no fontconfig.
func systemFonts() fontFiles {
	dir := os.Getenv("WINDIR")
	if dir == "" {
		dir = `C:\Windows`
	}
	in := func(names ...string) []string {
		paths := make([]string, len(names))
		for i, n := range names {
			paths[i] = filepath.Join(dir, "Fonts", n)
		}
		return paths
	}
	return fontFiles{
		regular:  in("CascadiaMono.ttf", "CascadiaCode.ttf", "consola.ttf", "lucon.ttf", "cour.ttf"),
		bold:     in("CascadiaMono-Bold.ttf", "CascadiaCode-Bold.ttf", "consolab.ttf", "courbd.ttf"),
		fallback: in("msgothic.ttc", "meiryo.ttc", "seguiemj.ttf", "segoeui.ttf", "seguisym.ttf", "msyh.ttc", "malgun.ttf"),
	}
}
