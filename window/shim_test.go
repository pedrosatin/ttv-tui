package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMpvSkipsTheShim(t *testing.T) {
	shim, real := t.TempDir(), t.TempDir()
	for _, dir := range []string{shim, real} {
		if err := os.WriteFile(filepath.Join(dir, "mpv"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := shim + string(os.PathListSeparator) + real
	if got, want := findMpv(path, shim), filepath.Join(real, "mpv"); got != want {
		t.Fatalf("achou %q, quero %q", got, want)
	}
	if got := findMpv(shim, shim); got != "" {
		t.Fatalf("só o shim no PATH: achou %q", got)
	}
}
