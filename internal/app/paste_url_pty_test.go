//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPasteStreamURL_PTY tests pasting a non-Twitch stream URL into search (Ctrl+F),
// verifying that it starts playback with the expected URL and label, and displays
// the unsupported chat notice on the chat panel.
func TestPasteStreamURL_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RICH=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "PtyCategory") {
		if time.Now().After(deadline) {
			t.Fatalf("initial categories missing:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Press Ctrl+F (0x06) to open search
	p.sendAndWait([]byte{0x06}, "Search:")

	// Paste non-Twitch stream URL and press Enter
	off := p.mark()
	p.sendAndWait([]byte("https://kick.com/xqc\r"), "Chat for this platform is not supported yet")

	screen := p.screenSince(off)
	if !strings.Contains(screen, "Chat for this platform is not supported yet") {
		t.Fatalf("expected unsupported chat notice, got:\n%s", screen)
	}
	if !strings.Contains(screen, "Unavailable") {
		t.Fatalf("expected chat header to show Unavailable, got:\n%s", screen)
	}
	t.Logf("PTY CAPTURE non-Twitch URL pasted:\n%s", screen)

	// Verify that the player was called with the expected URL and label
	targetLog := filepath.Join(p.dir, "player_targets.log")
	deadline = time.Now().Add(5 * time.Second)
	foundTarget := false
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(targetLog)
		if err == nil && strings.Contains(string(data), "https://kick.com/xqc\tkick:xqc") {
			foundTarget = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !foundTarget {
		data, _ := os.ReadFile(targetLog)
		t.Fatalf("expected player to be invoked with URL https://kick.com/xqc and label kick:xqc, got log:\n%s", string(data))
	}
}
