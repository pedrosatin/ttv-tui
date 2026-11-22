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

// Um mpv no diretório atual não é o real: entradas vazias, "." ou relativas do
// PATH apontam para lá e ficam de fora.
func TestFindMpvSkipsRelativeEntries(t *testing.T) {
	launch, real := t.TempDir(), t.TempDir()
	for _, dir := range []string{launch, real} {
		if err := os.WriteFile(filepath.Join(dir, mpvNames[0]), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(launch)
	sep := string(os.PathListSeparator)
	if got, want := findMpv(sep+"."+sep+real, ""), filepath.Join(real, mpvNames[0]); got != want {
		t.Fatalf("achou %q, quero %q", got, want)
	}
	if got := findMpv("."+sep, ""); got != "" {
		t.Fatalf("só entradas relativas no PATH: achou %q", got)
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

func TestParseShimArgsRole(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantIPC  string
		wantRole string
	}{
		{
			name:     "principal padrão",
			args:     []string{"--no-config", "--input-ipc-server=/run/ttv/i"},
			wantIPC:  "/run/ttv/i",
			wantRole: roleMain,
		},
		{
			name:     "prévia com focus-on e autofit",
			args:     []string{"--input-ipc-server=/run/ttv/preview/i", "--mute=yes", "--autofit=480x270", "--focus-on=never"},
			wantIPC:  "/run/ttv/preview/i",
			wantRole: rolePreview,
		},
		{
			name:     "prévia só com autofit",
			args:     []string{"--autofit=480x270", "--input-ipc-server=ttv-tui-1-abc"},
			wantIPC:  "ttv-tui-1-abc",
			wantRole: rolePreview,
		},
		{
			name:     "sem ipc como versão",
			args:     []string{"--version"},
			wantIPC:  "",
			wantRole: roleMain,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotIPC, gotRole := parseShimArgs(tc.args)
			if gotIPC != tc.wantIPC || gotRole != tc.wantRole {
				t.Fatalf("parseShimArgs(%v) = (%q, %q), quero (%q, %q)", tc.args, gotIPC, gotRole, tc.wantIPC, tc.wantRole)
			}
		})
	}
}
