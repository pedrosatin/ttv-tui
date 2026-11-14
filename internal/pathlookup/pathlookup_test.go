package pathlookup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func toolName() string {
	if runtime.GOOS == "windows" {
		return "tool.exe"
	}
	return "tool"
}

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// Um executável plantado no diretório de onde o app foi aberto não entra: o
// PATH vazio, "." ou relativo aponta para lá e fica de fora.
func TestAbsInSkipsRelativeEntries(t *testing.T) {
	launch, real := t.TempDir(), t.TempDir()
	writeExecutable(t, launch, toolName())
	if err := os.Mkdir(filepath.Join(launch, "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(launch, "rel"), toolName())
	want := writeExecutable(t, real, toolName())
	t.Chdir(launch)

	sep := string(os.PathListSeparator)
	got, err := AbsIn("tool", sep+"."+sep+"rel"+sep+real)
	if err != nil {
		t.Fatalf("AbsIn: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("AbsIn = %q, want %q", got, want)
	}

	_, err = AbsIn("tool", sep+"."+sep+"rel")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("only relative entries: err = %v, want ErrNotFound", err)
	}
}

// Abs lê o PATH do processo e, ao contrário do exec.LookPath, não para em
// exec.ErrDot: a entrada relativa não conta e a absoluta seguinte vale.
func TestAbsReadsPathAndNeverReturnsErrDot(t *testing.T) {
	launch, real := t.TempDir(), t.TempDir()
	writeExecutable(t, launch, toolName())
	want := writeExecutable(t, real, toolName())
	t.Chdir(launch)
	t.Setenv("PATH", "."+string(os.PathListSeparator)+real)

	got, err := Abs("tool")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("Abs = %q, want %q", got, want)
	}
}
