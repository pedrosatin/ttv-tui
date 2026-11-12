//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPasteStreamURL_PTY pastes a Kick URL into search (Ctrl+F) in the real
// program: playback starts with the expected URL and label, the Kick chat shows
// its messages next to it, read-only, and the bar names the channel and what
// the site says about it.
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

	// Paste the Kick URL and press Enter: the recorded chat session plays.
	off := p.mark()
	p.sendAndWait([]byte("https://kick.com/xqc\r"), "collectiblesGoldenW")

	deadline = time.Now().Add(5 * time.Second)
	for !strings.Contains(p.screenSince(off), "KickCat") {
		if time.Now().After(deadline) {
			t.Fatalf("the live state of the channel never reached the bar:\n%s", p.screenSince(off))
		}
		time.Sleep(20 * time.Millisecond)
	}
	screen := p.screenSince(off)
	for _, want := range []string{
		"Chat — xqc",                          // the room, without the "#" of IRC
		"Viewer01",                            // an author of the recorded session
		"Kick: read-only, no message sending", // the input line says sending does not exist
		"xqc (Kick)",                          // the bar names the channel, not kick:xqc
	} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen missing %q:\n%s", want, screen)
		}
	}
	for _, unwanted := range []string{"#xqc", "Chat for this platform is not supported yet", "Starting kick:xqc", "Playing kick:xqc"} {
		if strings.Contains(screen, unwanted) {
			t.Fatalf("screen shows %q:\n%s", unwanted, screen)
		}
	}
	t.Logf("PTY CAPTURE Kick URL pasted:\n%s", screen)

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
