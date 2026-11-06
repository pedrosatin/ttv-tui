//go:build unix

package app

import (
	"strings"
	"testing"
)

const (
	keyCtrlL = 0x0c
	keyCtrlW = 0x17
)

// TestNarrowChat_PTY walks the two reports in a real terminal: Ctrl+W before
// any channel keeps the list and explains, and a pane under 80 columns reaches
// the chat through Ctrl+L.
func TestNarrowChat_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RICH=1", "TTV_APP_USERNOTICE=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")
	setSize(t, p.master, 24, 80)
	p.waitScreen(0, "PtyCat")

	off := p.mark()
	p.send([]byte{keyCtrlW})
	home := p.waitScreen(off, "No chat yet")
	if strings.Contains(home, "layout:") {
		t.Fatalf("Ctrl+W on the home screen announced a layout:\n%s", home)
	}
	t.Logf("PTY capture Ctrl+W on the home screen:\n%s", home)

	p.openChannel()
	p.waitScreen(0, "Bob: oi gente")

	off = p.mark()
	setSize(t, p.master, 24, 60)
	split := p.waitScreen(off, "Chat hidden (<80 cols)", "Ctrl+L")
	t.Logf("PTY capture split at 60 columns:\n%s", split)

	off = p.mark()
	p.send([]byte{keyCtrlL})
	chatOnly := p.waitScreen(off, "Chat — #ptychan", "Bob: oi gente")
	t.Logf("PTY capture chat-only at 60 columns:\n%s", chatOnly)
}
