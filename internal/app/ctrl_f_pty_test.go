//go:build unix

package app

import (
	"strings"
	"testing"
	"time"
)

// TestCtrlFOpensSearchPrompt_PTY drives the real TUI over a PTY and confirms
// Ctrl+F (0x06) opens the global search prompt. Ticket 10: Ctrl+S is XOFF on
// terminals with ixon, so Ctrl+F is the primary binding.
func TestCtrlFOpensSearchPrompt_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_SEARCH_ESC=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "EscCategory") {
		if time.Now().After(deadline) {
			t.Fatalf("initial categories missing:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	off := p.mark()
	p.sendAndWait([]byte{0x06}, "Search:") // Ctrl+F
	screen := p.screenSince(off)
	if !strings.Contains(screen, "Search:") {
		t.Fatalf("Ctrl+F did not open search prompt:\n%s", screen)
	}
	t.Logf("PTY CAPTURE Ctrl+F opens search:\n%s", screen)
}
