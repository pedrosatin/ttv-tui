package app

import (
	"os"
	"runtime"

	"github.com/charmbracelet/x/term"
)

// isTerminal checks the supplied stream, not a fallback /dev/tty. A character
// device alone is insufficient: /dev/null is one too. x/term is already used
// by Bubble Tea and verifies the terminal on Unix and Windows.
func isTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok {
		return false
	}
	if hostedByConPTY() {
		return true
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && term.IsTerminal(f.Fd())
}

// hostedByConPTY reports a TUI inside the window on Windows, where the stdin
// the ConPTY hands over fails the check: Stat reports a named pipe. On Unix the
// window gives a real PTY, which passes it.
func hostedByConPTY() bool {
	return runtime.GOOS == "windows" && os.Getenv(videoHostEnv) != ""
}
