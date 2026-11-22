//go:build windowtest

package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatsDuringFontReplacement(t *testing.T) {
	a := &app{fs: &fontSet{cellW: 1, cellH: 2}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			a.paintMu.Lock()
			a.fs.close()
			a.fs = &fontSet{cellW: i + 1, cellH: i + 2, tofu: i}
			a.paintMu.Unlock()
		}
	}()
	for i := 0; i < 10000; i++ {
		out := a.stats()
		if out["cellH"].(int) != out["cellW"].(int)+1 {
			t.Error("stats misturou métricas de fontes diferentes")
		}
	}
	<-done
}

func TestStatsReportsMainAndPreviewRoles(t *testing.T) {
	a := &app{fs: &fontSet{cellW: 1, cellH: 2}}
	dir, err := os.MkdirTemp("/tmp", "ttvts-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	sock := filepath.Join(dir, "t.sock")
	go a.serveTest(sock)

	query := func() map[string]any {
		t.Helper()
		var c net.Conn
		var dialErr error
		for range 100 {
			c, dialErr = net.Dial("unix", sock)
			if dialErr == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		defer func() { _ = c.Close() }()
		if _, err := c.Write([]byte("stats\n")); err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err := json.NewDecoder(c).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	check := func(wantActive, wantMain, wantPreview bool, wantRole string) {
		t.Helper()
		got := query()
		if got["active"] != wantActive || got["mainActive"] != wantMain || got["previewActive"] != wantPreview || got["lastRole"] != wantRole {
			t.Fatalf("stats = (active=%v, main=%v, preview=%v, lastRole=%v), quero (%v, %v, %v, %q)",
				got["active"], got["mainActive"], got["previewActive"], got["lastRole"], wantActive, wantMain, wantPreview, wantRole)
		}
	}

	check(false, false, false, "")
	a.preview = &session{role: rolePreview}
	a.lastRole = rolePreview
	check(true, false, true, rolePreview)
	a.sess = &session{role: roleMain}
	a.lastRole = roleMain
	check(true, true, true, roleMain)
	a.preview = nil
	check(true, true, false, roleMain)
}
