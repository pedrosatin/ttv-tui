package app

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// isTerminal checks the supplied stream, not a fallback /dev/tty. A character
// device alone is insufficient: /dev/null is one too. x/term is already used
// by Bubble Tea and verifies the terminal on Unix and Windows. Inside the window
// the check holds too: a PTY on Unix, the ConPTY's console on Windows.
func isTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && term.IsTerminal(f.Fd())
}
