package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// logPollInterval is how often a followed log is checked for new lines. Nothing it
// carries is urgent to the millisecond: an ad hold or a failure shows up this late.
const logPollInterval = 100 * time.Millisecond

// playerLogPath is where streamlink logs in the session directory. It goes with the
// directory, so a session leaves no log behind. At the info level it grows by a few
// lines per ad break.
func playerLogPath(dir string) string {
	return filepath.Join(dir, "streamlink.log")
}

// openPlayerLog creates the log and returns its read end. Creating it here makes
// following it independent of when streamlink gets to it; streamlink appends. The
// truncation is for the fallback directory, which an earlier run may have left.
func openPlayerLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("player: streamlink log: %w", err)
	}
	return f, nil
}

// logFollower reads a file another process appends to as if it were a pipe: Read
// waits for more instead of stopping at the end of the file. A file has no writer
// to lose, so the end comes from outside: once gone is closed, one last pass picks
// up what the process wrote before exiting.
type logFollower struct {
	ctx  context.Context
	file *os.File
	gone <-chan struct{}
	last bool
}

func (l *logFollower) Read(p []byte) (int, error) {
	for {
		n, err := l.file.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if l.last {
			return 0, io.EOF
		}
		select {
		case <-l.ctx.Done():
			return 0, io.EOF
		case <-l.gone:
			l.last = true
		case <-time.After(logPollInterval):
		}
	}
}
