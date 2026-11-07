package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// detectVideo returns the reason the requested backend cannot run, as a catalog
// key. Empty means it can.
func detectVideo(ctx context.Context, opts Options, input io.Reader, output io.Writer) string {
	if runtime.GOOS == "windows" {
		return "video.unsupported_platform"
	}
	if os.Getenv("TMUX") != "" {
		return "video.tmux"
	}
	probe := opts.SixelProbe
	if probe.Terminal == nil {
		probe.Terminal = func(ctx context.Context) ([]byte, error) { return queryDA1(ctx, input, output) }
	}
	return player.DetectSixel(ctx, probe)
}

// terminalOutput is what Bubble Tea writes to while video is on. Bubble Tea only
// reads the window size from an output that is a terminal File, so a bare writer
// would cost the UI every resize.
type terminalOutput struct {
	*player.SixelOutput
	file *os.File
}

func (t terminalOutput) Read(p []byte) (int, error) { return t.file.Read(p) }
func (t terminalOutput) Fd() uintptr                { return t.file.Fd() }

// sixelSurface adapts player.SixelOutput to ui.VideoSurface.
type sixelSurface struct{ out *player.SixelOutput }

func (s sixelSurface) SetPanel(p ui.VideoPanel) { s.out.SetPanel(player.SixelPanel(p)) }

// terminalQueryTimeout bounds how long a query waits for the terminal's answer.
const terminalQueryTimeout = 350 * time.Millisecond

// startVideo takes the terminal over for the sixel backend. cell is its source for
// the cell size. It returns the writer Bubble Tea must use and the UI option for
// the panel; stop ends the output.
func startVideo(output io.Writer, mgr *player.Manager, cell func() (int, int)) (io.Writer, ui.Option, func()) {
	file, ok := output.(*os.File)
	if !ok {
		return output, nil, func() {}
	}
	sixel := player.NewSixelOutput(file, player.SixelOptions{
		Cell:   cell,
		Resize: func(width, height int) { _ = mgr.ResizeVideo(width, height) },
		Track:  func(on bool) { _ = mgr.SetVideoTrack(on) },
	})
	mgr.SetVideoOutput(sixel)
	return terminalOutput{SixelOutput: sixel, file: file}, ui.WithVideoSurface(sixelSurface{sixel}), func() { _ = sixel.Close() }
}

// probeCell returns the source of the cell size in pixels. The window size is
// asked again each time, because it follows font changes. The answer to CSI 16 t
// is the fallback, and it has to be collected here: once Bubble Tea owns the input
// nothing reads it. Zeros from the source mean the cell is unknown.
func probeCell(ctx context.Context, input io.Reader, output io.Writer) func() (int, int) {
	file, ok := output.(*os.File)
	if !ok {
		return nil
	}
	var width, height int
	if w, h := cellSize(file); w == 0 || h == 0 {
		reply, _ := queryCell(ctx, input, output)
		width, height, _ = player.ParseCellReply(reply)
	}
	return func() (int, int) {
		if w, h := cellSize(file); w > 0 && h > 0 {
			return w, h
		}
		return width, height
	}
}

// queryCell asks for the cell size. Every terminal here answers DA1, and answers in
// order, so its reply ends the wait: a terminal that ignores CSI 16 t costs one
// round trip instead of the whole timeout. Only a silent terminal waits it out, and
// what arrived by then still comes back with the error.
func queryCell(ctx context.Context, input io.Reader, output io.Writer) ([]byte, error) {
	return queryTerminal(ctx, input, output, "\x1b[16t\x1b[c", func(reply []byte) bool {
		return bytes.HasSuffix(reply, []byte("c"))
	})
}

func queryDA1(ctx context.Context, input io.Reader, output io.Writer) ([]byte, error) {
	reply, err := queryTerminal(ctx, input, output, "\x1b[c", func(reply []byte) bool {
		return bytes.Contains(reply, []byte("c"))
	})
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// queryTerminal owns input only before Bubble Tea starts. Cancel and join the
// reader before restoring raw mode so no background read can steal UI input. On an
// error, the reply holds what arrived before it.
func queryTerminal(ctx context.Context, input io.Reader, output io.Writer, query string, complete func([]byte) bool) ([]byte, error) {
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
	ctx, cancel := context.WithTimeout(ctx, terminalQueryTimeout)
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
	if _, err := io.WriteString(output, query); err != nil {
		return nil, err
	}
	var reply []byte
	buf := make([]byte, 128)
	for len(reply) < 1024 {
		n, err := reader.Read(buf)
		reply = append(reply, buf[:n]...)
		if complete(reply) {
			return reply, nil
		}
		if err != nil {
			return reply, err
		}
	}
	return reply, nil
}
