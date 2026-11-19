//go:build unix

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Startup cleans both the main registry and the nested preview registry.
// FindOrphans skips directories, so a leftover under players/preview would
// otherwise survive SIGKILL of a previous TUI.
func TestHandleOrphansCleansPreviewRegistry(t *testing.T) {
	dir := t.TempDir()
	regDir := playerRegistryDir(filepath.Join(dir, "state", "state.json"))
	previewDir := filepath.Join(regDir, "preview")
	if err := os.MkdirAll(previewDir, 0o700); err != nil {
		t.Fatal(err)
	}

	dead := shortLivedPID(t)
	rec := map[string]any{
		"owner_pid":   dead,
		"owner_start": 1,
		"pgid":        dead,
		"socket":      "/nonexistent/preview-orphan/i",
		"generation":  1,
		"login":       "prevorphan",
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(previewDir, "player-1-1.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	// Same sequence as app.Run: main registry, then preview subdirectory.
	handleOrphans(regDir, strings.NewReader(""), &bytes.Buffer{}, &log, false, cat)
	handleOrphans(previewDir, strings.NewReader(""), &bytes.Buffer{}, &log, false, cat)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preview record still present: %v (log=%q)", err, log.String())
	}
}

func shortLivedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if cmd.Process == nil {
		t.Fatal("no process")
	}
	return cmd.Process.Pid
}
