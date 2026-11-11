package main

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ttv-tui-cmd-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"} {
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	code := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
