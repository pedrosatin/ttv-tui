package app

import (
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func videoOption(ctx context.Context, opts Options, input io.Reader, output io.Writer) ui.Option {
	if opts.Video == "" {
		return ui.WithVideo(false, "")
	}
	if runtime.GOOS == "windows" {
		return ui.WithVideo(true, "video.unsupported_platform")
	}
	if os.Getenv("TMUX") != "" {
		return ui.WithVideo(true, "video.tmux")
	}
	probe := opts.SixelProbe
	if probe.Terminal == nil {
		probe.Terminal = func(ctx context.Context) ([]byte, error) { return queryDA1(ctx, input, output) }
	}
	return ui.WithVideo(true, player.DetectSixel(ctx, probe))
}

// queryDA1 owns input only before Bubble Tea starts. Cancel and join the reader
// before restoring raw mode so no background read can steal UI input.
func queryDA1(ctx context.Context, input io.Reader, output io.Writer) ([]byte, error) {
	f, ok := input.(*os.File)
	if !ok {
		return nil, os.ErrInvalid
	}
	state, err := term.MakeRaw(f.Fd())
	if err != nil {
		return nil, err
	}
	defer func() { _ = term.Restore(f.Fd(), state) }()
	reader, err := cancelreader.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	ctx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			reader.Cancel()
		case <-done:
		}
	}()
	defer func() { close(done); <-stopped }()
	if _, err := io.WriteString(output, "\x1b[c"); err != nil {
		return nil, err
	}
	var reply []byte
	buf := make([]byte, 128)
	for len(reply) < 1024 {
		n, err := reader.Read(buf)
		reply = append(reply, buf[:n]...)
		if strings.Contains(string(reply), "c") {
			return reply, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return reply, nil
}
