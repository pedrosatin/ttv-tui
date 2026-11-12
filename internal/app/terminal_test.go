package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunRejectsNullInputWithoutTerminalOutput(t *testing.T) {
	dir := t.TempDir()
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	var output, log bytes.Buffer
	err = Run(Options{Lang: "en", ConfigPath: filepath.Join(dir, "config.toml"), StatePath: filepath.Join(dir, "state.json"), Input: input, Output: &output, Log: &log})
	if err == nil || !strings.Contains(err.Error(), "ssh -t") {
		t.Errorf("Run error = %v; want terminal guidance including ssh -t", err)
	}
	if output.Len() != 0 || log.Len() != 0 {
		t.Errorf("terminal touched: output=%q log=%q", output.String(), log.String())
	}
}

// Só o ConPTY do Windows dispensa a checagem; no Unix a janela dá um PTY real.
func TestVideoHostSkipsTheCheckOnlyOnWindows(t *testing.T) {
	t.Setenv(videoHostEnv, filepath.Join(t.TempDir(), "layout.sock"))
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	if got, want := isTerminal(null), runtime.GOOS == "windows"; got != want {
		t.Fatalf("isTerminal(%s) com %s = %v; quero %v", os.DevNull, videoHostEnv, got, want)
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("um buffer passou por terminal")
	}
}
