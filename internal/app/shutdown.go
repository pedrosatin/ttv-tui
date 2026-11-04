package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

// SignalError reports that the TUI stopped because the process received a
// termination signal (SIGHUP when the terminal window closes, SIGINT, SIGTERM).
// Run returns it only after the player and chat were shut down.
type SignalError struct {
	Signal os.Signal
}

func (e *SignalError) Error() string {
	return fmt.Sprintf("terminated by signal: %v", e.Signal)
}

// ExitCode follows the shell convention 128+signal number.
func (e *SignalError) ExitCode() int {
	if s, ok := e.Signal.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 1
}

// ExitCode maps an error returned by Run to a process exit status.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var se *SignalError
	if errors.As(err, &se) {
		return se.ExitCode()
	}
	return 1
}

// orphanStopWait is how long an orphaned player gets after SIGTERM before SIGKILL.
const orphanStopWait = 3 * time.Second

// playerRegistryDir keeps live-player records next to the app state file.
func playerRegistryDir(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "players")
}

// isInteractive reports whether r is a terminal we can ask a question on.
func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// handleOrphans looks for players left running by a previous run (the TUI died
// without its cleanup, e.g. SIGKILL). Interactive: ask before stopping each one,
// default yes. Non-interactive: stop it and leave a warning on log.
func handleOrphans(dir string, in io.Reader, out, log io.Writer, interactive bool, cat *i18n.Catalog) {
	orphans, err := player.FindOrphans(dir)
	if err != nil || len(orphans) == 0 {
		return
	}
	for _, o := range orphans {
		if interactive {
			_, _ = fmt.Fprintf(out, cat.T("app.orphan_prompt"), o.Login, o.PGID)
			if !answerIsYes(readLine(in)) {
				_, _ = fmt.Fprintln(out, cat.T("app.orphan_kept"))
				continue
			}
		}
		if err := o.Terminate(orphanStopWait); err != nil {
			_, _ = fmt.Fprintf(log, cat.T("app.orphan_stop_failed")+"\n", o.PGID, err)
			continue
		}
		if interactive {
			_, _ = fmt.Fprintln(out, cat.T("app.orphan_stopped"))
		} else {
			_, _ = fmt.Fprintf(log, cat.T("app.orphan_cleaned_log")+"\n", o.Login, o.PGID)
		}
	}
}

// readLine reads one line byte by byte, so nothing past the newline is consumed
// from the input the TUI will read next.
func readLine(r io.Reader) string {
	var sb strings.Builder
	buf := make([]byte, 1)
	for sb.Len() < 256 {
		n, err := r.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(sb.String())
}

// answerIsYes defaults to yes: only an answer starting with n/N declines.
func answerIsYes(answer string) bool {
	return !strings.HasPrefix(strings.ToLower(answer), "n")
}
