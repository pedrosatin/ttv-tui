//go:build unix

package player

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteInputConfRefusesSharedDir(t *testing.T) {
	dir := privateDir(t)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	path := filepath.Join(dir, inputConfName)
	if err := writeInputConf(path); err == nil {
		t.Fatal("writeInputConf accepted a directory other users can read")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file left behind: %v", err)
	}
}

func TestWriteInputConfDoesNotWriteThroughSymlink(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(dir, inputConfName)
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := writeInputConf(path); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("symlink target overwritten: %q", got)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("path is still a symlink")
	}
}
