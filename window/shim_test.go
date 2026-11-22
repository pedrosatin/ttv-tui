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

func TestParseShimConfigRole(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		roleEnv  string
		wantIPC  string
		wantRole string
	}{
		{
			name:     "principal padrão sem sinal",
			args:     []string{"--no-config", "--input-ipc-server=/run/ttv/i"},
			wantIPC:  "/run/ttv/i",
			wantRole: roleMain,
		},
		{
			name:     "principal com autofit e focus-on continua principal",
			args:     []string{"--input-ipc-server=/run/ttv/i", "--autofit=480x270", "--focus-on=never"},
			wantIPC:  "/run/ttv/i",
			wantRole: roleMain,
		},
		{
			name:     "principal com sinal desconhecido",
			args:     []string{"--input-ipc-server=/run/ttv/i"},
			roleEnv:  "other",
			wantIPC:  "/run/ttv/i",
			wantRole: roleMain,
		},
		{
			name:     "prévia pelo sinal explícito",
			args:     []string{"--input-ipc-server=/run/ttv/preview/i", "--mute=yes"},
			roleEnv:  rolePreview,
			wantIPC:  "/run/ttv/preview/i",
			wantRole: rolePreview,
		},
		{
			name:     "sem ipc como versão",
			args:     []string{"--version"},
			roleEnv:  rolePreview,
			wantIPC:  "",
			wantRole: rolePreview,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotIPC, gotRole := parseShimConfig(tc.args, func(k string) string {
				if k == windowRoleEnv {
					return tc.roleEnv
				}
				return ""
			})
			if gotIPC != tc.wantIPC || gotRole != tc.wantRole {
				t.Fatalf("parseShimConfig(%v, %q) = (%q, %q), quero (%q, %q)", tc.args, tc.roleEnv, gotIPC, gotRole, tc.wantIPC, tc.wantRole)
			}
		})
	}
}
