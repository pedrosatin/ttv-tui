//go:build windows

package main

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

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

func TestParseRole(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"", roleMain, false},
		{roleMain, roleMain, false},
		{rolePreview, rolePreview, false},
		{"other", "", true},
	} {
		got, err := parseRole(tc.raw)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Fatalf("parseRole(%q) = (%q, %v), quero (%q, err=%v)", tc.raw, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestRoleCrossValidationWindows(t *testing.T) {
	mainPipe := `\\.\pipe\ttv-win-stream-123-456`
	previewPipe := `\\.\pipe\ttv-win-stream-preview-123-456`
	mainIPC := "ttv-tui-123-456"
	previewIPC := "ttv-tui-preview-123-456"

	if !validStreamPipeForRole(mainPipe, roleMain) || !validStreamPipeForRole(previewPipe, rolePreview) {
		t.Fatal("valid role pipe rejected")
	}
	if validStreamPipeForRole(previewPipe, roleMain) || validStreamPipeForRole(mainPipe, rolePreview) {
		t.Fatal("cross-role stream pipe accepted")
	}
	if !validIPCNameForRole(mainIPC, roleMain) || !validIPCNameForRole(previewIPC, rolePreview) {
		t.Fatal("valid role IPC rejected")
	}
	if validIPCNameForRole(previewIPC, roleMain) || validIPCNameForRole(mainIPC, rolePreview) {
		t.Fatal("cross-role IPC name accepted")
	}
	if validStreamPipeForRole(`\\.\pipe\ttv-win-stream-preview-`, rolePreview) || validIPCNameForRole("ttv-tui-preview-", rolePreview) {
		t.Fatal("empty preview suffix accepted")
	}
}

func TestReadStartRoleAndRejectionWindows(t *testing.T) {
	mainPipe := `\\.\pipe\ttv-win-stream-1-2`
	previewPipe := `\\.\pipe\ttv-win-stream-preview-1-2`
	mainIPC := "ttv-tui-1-2"
	previewIPC := "ttv-tui-preview-1-2"

	runReq := func(ipc, pipe, role string) (ctlMsg, error) {
		ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "c"), Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ln.Close() }()
		c, err := net.DialUnix("unix", nil, ln.Addr().(*net.UnixAddr))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		srv, err := ln.AcceptUnix()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = srv.Close() }()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		_ = srv.SetDeadline(time.Now().Add(5 * time.Second))

		type res struct {
			msg ctlMsg
			err error
		}
		ch := make(chan res, 1)
		go func() {
			m, err := readStart(srv)
			ch <- res{m, err}
		}()
		var pidMsg struct {
			PID int `json:"pid"`
		}
		if err := json.NewDecoder(c).Decode(&pidMsg); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"ipc": ipc, "pipe": pipe, "role": role})
		if _, err := c.Write(append(payload, '\n')); err != nil {
			t.Fatal(err)
		}
		r := <-ch
		return r.msg, r.err
	}

	if msg, err := runReq(mainIPC, mainPipe, ""); err != nil || msg.role != roleMain {
		t.Fatalf("empty role: msg=%+v err=%v", msg, err)
	}
	if msg, err := runReq(previewIPC, previewPipe, rolePreview); err != nil || msg.role != rolePreview {
		t.Fatalf("preview role: msg=%+v err=%v", msg, err)
	}
	for _, tc := range []struct {
		name, ipc, pipe, role string
	}{
		{"invalid role", mainIPC, mainPipe, "other"},
		{"preview ipc with main role", previewIPC, mainPipe, roleMain},
		{"main ipc with preview role", mainIPC, previewPipe, rolePreview},
		{"preview pipe with main role", mainIPC, previewPipe, roleMain},
		{"main pipe with preview role", previewIPC, mainPipe, rolePreview},
	} {
		if _, err := runReq(tc.ipc, tc.pipe, tc.role); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}
