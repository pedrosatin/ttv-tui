package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/app"
)

func TestCLINonTTYErrorGoesToStderr(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{"en", "stdin and stdout must be terminals"},
		{"pt-BR", "stdin e stdout precisam ser terminais"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			dir := t.TempDir()
			input, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = input.Close() }()
			var stdout, stderr bytes.Buffer
			code := runWithIO([]string{"--lang", tc.lang}, input, &stdout, &stderr, app.LoginOptions{ConfigPath: filepath.Join(dir, "config.toml")})
			if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.want) || !strings.Contains(stderr.String(), "ssh -t") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if strings.ContainsAny(stderr.String(), "\x1b") {
				t.Fatalf("terminal escapes in error: %q", stderr.String())
			}
		})
	}
}

func TestDoctorWithRedirectedStdinAndStdout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	t.Setenv("PATH", dir)
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.Create(filepath.Join(dir, "doctor.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	var stderr bytes.Buffer
	code := runWithIO([]string{"doctor", "--lang", "en"}, input, output, &stderr, app.LoginOptions{ConfigPath: filepath.Join(dir, "config.toml")})
	report, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	// Missing tools yield diagnostic status 1, not a TUI initialization error.
	if code != 1 || stderr.Len() != 0 || !bytes.Contains(report, []byte("Token configured: NO")) || bytes.Contains(report, []byte("ssh -t")) {
		t.Fatalf("code=%d stderr=%q report=%q", code, stderr.String(), report)
	}
	// No TUI and no query: the report is plain text, whatever the streams are.
	if bytes.Contains(report, []byte("\x1b")) {
		t.Fatalf("terminal escapes in a redirected report: %q", report)
	}
}
