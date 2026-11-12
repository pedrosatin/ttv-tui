package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMpvSkipsTheShim(t *testing.T) {
	shim, real := t.TempDir(), t.TempDir()
	for _, dir := range []string{shim, real} {
		if err := os.WriteFile(filepath.Join(dir, mpvNames[0]), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := shim + string(os.PathListSeparator) + real
	if got, want := findMpv(path, shim), filepath.Join(real, mpvNames[0]); got != want {
		t.Fatalf("achou %q, quero %q", got, want)
	}
	if got := findMpv(shim, shim); got != "" {
		t.Fatalf("só o shim no PATH: achou %q", got)
	}
}

// O streamlink acha o shim pelo PATHEXT no Windows e o chama de mpv.EXE ou mpv.COM.
func TestCalledAsMpv(t *testing.T) {
	for arg0, want := range map[string]bool{
		"mpv": true,
		filepath.Join("ttv-shell-1", "bin", "mpv"): true,
		"mpv.EXE":                       true,
		filepath.Join("bin", "mpv.COM"): true,
		"ttv-tui-window":                false,
		"ttv-tui-window.exe":            false,
		"mpvx":                          false,
	} {
		if got := calledAsMpv(arg0); got != want {
			t.Errorf("calledAsMpv(%q) = %v, quero %v", arg0, got, want)
		}
	}
}
