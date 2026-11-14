//go:build !windowtest

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProductionIgnoresTestSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.sock")
	t.Setenv("TTV_WINDOW_TEST", path)
	if testSocketPath() != "" {
		t.Fatal("test socket enabled")
	}
	new(app).startTest(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket exists: %v", err)
	}
}
