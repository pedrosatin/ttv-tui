package main

import (
	"bytes"
	"strings"
	"testing"
)

// The flag package echoes the rejected option in its own diagnostic. These
// tests pin that a hostile option or value reaches stderr as visible text, and
// that the legitimate usage keeps the shape it had before.

var flagCommands = []struct {
	name string
	args []string
}{
	{"root", nil},
	{"login", []string{"login"}},
	{"doctor", []string{"doctor"}},
}

func runFlags(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestFlagDiagnosticsNeutralizeTerminalControls(t *testing.T) {
	const payload = "\x1b]0;X\x07"
	for _, cmd := range flagCommands {
		t.Run(cmd.name, func(t *testing.T) {
			for _, tail := range [][]string{
				{"--" + payload},
				{"--lang=" + payload + "\x9b2J", "--" + payload},
			} {
				args := append(append([]string{"--lang", "en"}, cmd.args...), tail...)
				code, stdout, stderr := runFlags(args...)
				if code == 0 {
					t.Fatalf("exit code = 0 for %q", args)
				}
				for _, got := range []string{stdout, stderr} {
					if strings.ContainsAny(got, "\x1b\x07") || strings.IndexByte(got, 0x9b) >= 0 {
						t.Fatalf("saída com controle cru para %q: %q", args, got)
					}
				}
				if !strings.Contains(stderr, `flag provided but not defined: -\x1b]0;X\x07`) {
					t.Errorf("stderr não mostra a opção como escape visível: %q", stderr)
				}
			}
		})
	}
}

func TestFlagValueErrorsNeutralizeTerminalControls(t *testing.T) {
	code, _, stderr := runFlags("--version=\x1b]0;X\x07")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if strings.ContainsAny(stderr, "\x1b\x07") {
		t.Fatalf("stderr com controle cru: %q", stderr)
	}
	if !strings.Contains(stderr, "invalid boolean value") {
		t.Errorf("stderr sem o diagnóstico do valor: %q", stderr)
	}
}

func TestFlagUsageKeepsItsShape(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		help  []string // prints the usage on stdout (root, login) ...
		helpE bool     // ... or on stderr (doctor)
	}{
		{"root", []string{"--lang", "en", "--bogus"}, []string{"--lang", "en", "--help"}, false},
		{"login", []string{"--lang", "en", "login", "--bogus"}, []string{"--lang", "en", "login", "--help"}, false},
		{"doctor", []string{"--lang", "en", "doctor", "--bogus"}, []string{"--lang", "en", "doctor", "-h"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, helpOut, helpErr := runFlags(tc.help...)
			usage := helpOut
			if tc.helpE {
				usage = helpErr
			}
			if !strings.Contains(usage, "Usage:") || !strings.Contains(usage, "\n") {
				t.Fatalf("ajuda sem o formato esperado: %q", usage)
			}
			code, _, stderr := runFlags(tc.args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			want := "flag provided but not defined: -bogus\n" + usage
			if stderr != want {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
		})
	}
}

func TestDoctorHelpAndExtraArgumentKeepExitCodes(t *testing.T) {
	if code, out, errOut := runFlags("--lang", "en", "doctor", "-h"); code != 0 || out != "" || !strings.Contains(errOut, "Usage:") {
		t.Errorf("doctor -h: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if code, _, errOut := runFlags("--lang", "en", "doctor", "extra"); code != 2 || !strings.Contains(errOut, "Usage:") {
		t.Errorf("doctor extra: code=%d stderr=%q", code, errOut)
	}
}
