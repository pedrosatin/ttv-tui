//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestKickCatalog_PTY switches the list to Kick in the real program: p shows
// the live categories of Kick with their viewers, Enter lists the channels of
// one, Enter on a channel plays it with the Kick chat next to it, and p goes
// back to Twitch while that keeps playing.
func TestKickCatalog_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RICH=1", "TTV_APP_KICK_CATALOG=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")
	p.waitScreen(0, "Twitch · Live Categories", "PtyCategory")

	off := p.mark()
	p.send([]byte("p"))
	screen := p.waitScreen(off, "Kick · Live Categories", "KickSlots", "777", "list: Kick")
	if strings.Contains(screen, "PtyCategory") {
		t.Fatalf("the Twitch category is still listed under Kick:\n%s", screen)
	}

	off = p.mark()
	p.send([]byte("\r"))
	p.waitScreen(off, "Kick · Live Channels — KickSlots", "pty kick title", "1.2k")

	off = p.mark()
	p.send([]byte("\r"))
	screen = p.waitScreen(off, "Chat — xqc", "collectiblesGoldenW", "xqc (Kick)")
	t.Logf("PTY CAPTURE Kick channel opened from the list:\n%s", screen)

	targetLog := filepath.Join(p.dir, "player_targets.log")
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(targetLog)
		if strings.Contains(string(data), "https://kick.com/xqc\tkick:xqc") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the player was not started with the Kick channel; log:\n%s", data)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Back to Twitch: the list changes, the Kick chat stays where it was.
	off = p.mark()
	p.send([]byte("p"))
	p.waitScreen(off, "Twitch · Live Categories", "PtyCategory")
}
