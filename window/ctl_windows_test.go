//go:build windows

package main

import "testing"

func TestStreamPipePrefix(t *testing.T) {
	for _, name := range []string{`C:\file`, `\\.\pipe\other`, `\\.\pipe\ttv-win-stream-../file`} {
		if validStreamPipe(name) {
			t.Fatal(name)
		}
	}
	if !validStreamPipe(`\\.\pipe\ttv-win-stream-123-456`) {
		t.Fatal("valid pipe rejected")
	}
}
