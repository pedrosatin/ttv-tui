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

func TestIPCName(t *testing.T) {
	for _, name := range []string{"", "other-123", `ttv-tui-../file`, `ttv-tui-..\file`, `\\.\pipe\ttv-tui-123`} {
		if validIPCName(name) {
			t.Fatalf("invalid IPC accepted: %q", name)
		}
	}
	if !validIPCName("ttv-tui-123-456") {
		t.Fatal("valid IPC rejected")
	}
}
