//go:build unix

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// orphanLoginPayload would retitle the window and clear the screen if the
// orphan prompt printed it raw.
const orphanLoginPayload = "\x1b]0;X\x07\x1b[2J"

// setOrphanLogin rewrites the login of the only live-player record in dir, as
// a record written for a hostile label would hold it.
func setOrphanLogin(t *testing.T, dir, login string) {
	t.Helper()
	recs := records(t, dir)
	if len(recs) != 1 {
		t.Fatalf("records = %v, want one", recs)
	}
	data, err := os.ReadFile(recs[0])
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	rec["login"] = login
	data, err = json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recs[0], data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertNoControlBytes fails when out carries a byte a terminal acts on, other
// than the newlines the messages end with.
func assertNoControlBytes(t *testing.T, what, out string) {
	t.Helper()
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c < 0x20 && c != '\n' || c == 0x7f || c == 0x9b {
			t.Fatalf("%s carries control byte %#x at %d: %q", what, c, i, out)
		}
	}
}

func TestHandleOrphansNeutralizesLoginInPrompt(t *testing.T) {
	dir := t.TempDir()
	playerPid, _ := spawnOrphan(t, dir)
	setOrphanLogin(t, dir, orphanLoginPayload)
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	regDir := playerRegistryDir(filepath.Join(dir, "state", "state.json"))

	var out bytes.Buffer
	handleOrphans(regDir, strings.NewReader("n\n"), &out, &bytes.Buffer{}, true, cat)
	assertNoControlBytes(t, "interactive prompt", out.String())
	if !strings.Contains(out.String(), `channel \x1b]0;X\x07\x1b[2J, pid`) {
		t.Fatalf("prompt does not show the login escaped: %q", out.String())
	}
	if !alive(playerPid) {
		t.Fatal("declined orphan was stopped")
	}

	var log bytes.Buffer
	handleOrphans(regDir, strings.NewReader(""), &bytes.Buffer{}, &log, false, cat)
	assertNoControlBytes(t, "non-interactive log", log.String())
	if !strings.Contains(log.String(), `channel \x1b]0;X\x07\x1b[2J, pid`) {
		t.Fatalf("log does not show the login escaped: %q", log.String())
	}
	if alive(playerPid) {
		t.Fatal("non-interactive run did not stop the orphan")
	}
}
