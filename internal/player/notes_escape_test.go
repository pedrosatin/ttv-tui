package player

import (
	"strings"
	"testing"
)

// hostileReason is a reason phrase as a server could send it: OSC 0 retitles
// the window, CSI 2J clears the screen.
const hostileReason = "\x1b]0;ZZ\x07\x1b[2J"

func assertNoTerminalControls(t *testing.T, notes []string) {
	t.Helper()
	for _, n := range notes {
		if strings.ContainsAny(n, "\x1b\x07\u009b") || strings.IndexByte(n, 0x9b) >= 0 {
			t.Fatalf("note carries a terminal control: %q", n)
		}
	}
}

// The notes reach stderr after the TUI exits, so the Session keeps them
// already escaped; the ad-hold detector still reads the raw line.
func TestSessionNotesAreEscaped(t *testing.T) {
	line := "[stream.hls][info] " + hostileReason + " stream output"
	_, notes := feedLines([]string{line})
	if len(notes) != 1 {
		t.Fatalf("notes = %q, want the unknown ad-hold line", notes)
	}
	assertNoTerminalControls(t, notes)
	if want := `player: unrecognized streamlink ad-hold line: [stream.hls][info] \x1b]0;ZZ\x07\x1b[2J stream output`; notes[0] != want {
		t.Fatalf("note = %q, want %q", notes[0], want)
	}

	fallback := "[plugins.kick][warning] ttv-tui plugin fallback: ad filter off" + hostileReason
	_, notes = feedLines([]string{fallback})
	assertNoTerminalControls(t, notes)
	if len(notes) != 1 || !strings.HasSuffix(notes[0], `ad filter off\x1b]0;ZZ\x07\x1b[2J`) {
		t.Fatalf("notes = %q, want the fallback line escaped", notes)
	}
}

// Only what becomes a note is escaped: the failure classification and the
// line kept for the failure message see what streamlink wrote.
func TestSessionClassifiesRawLine(t *testing.T) {
	line := "error: Unable to open URL: network is unreachable" + hostileReason
	s := &Session{
		cfg:   sessionConfig{Generation: 1, Target: testTarget("x"), Emit: func(Event) {}},
		state: StatePlaying,
	}
	s.handleLine(line)
	if s.failClass != FailureNetwork {
		t.Fatalf("failClass = %q, want %q", s.failClass, FailureNetwork)
	}
	if s.failLine != line || s.lastLine != line {
		t.Fatalf("failLine = %q, lastLine = %q, want the raw line", s.failLine, s.lastLine)
	}
}
