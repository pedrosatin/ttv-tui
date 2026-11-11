package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
)

func TestScaledFontSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	path := filepath.Join(dir, "mono.ttf")
	if err := os.WriteFile(path, gomono.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fc-match"), []byte("#!/bin/sh\nprintf '%s' \"$TTV_TEST_FONT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TTV_TEST_FONT", path)
	base, err := scaledFontSet(9, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer base.close()
	base.get('M', false)
	for _, scale := range []float64{2, 1.5, 1, 0, -1} {
		fs, err := scaledFontSet(9, scale)
		if err != nil {
			t.Fatal(err)
		}
		if len(fs.cache) != 0 {
			t.Fatal("cache de glifos reaproveitado")
		}
		if scale > 1 && (fs.cellW <= base.cellW || fs.cellH <= base.cellH) {
			t.Errorf("escala %g não ampliou células: %dx%d", scale, fs.cellW, fs.cellH)
		}
		if scale <= 1 && (fs.cellW != base.cellW || fs.cellH != base.cellH || fs.base != base.base) {
			t.Errorf("escala %g não restaurou métricas", scale)
		}
		if fs.get('M', false) == nil {
			t.Error("fonte sem glifo M")
		}
		fs.close()
	}
}
