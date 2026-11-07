//go:build unix

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"golang.org/x/sys/unix"
)

// readPTYFull handles the nonblocking, non-pollable master returned on Darwin.
// Use raw reads on Linux too, so readiness handling is exercised rather than
// hidden by os.File's poller. The fake terminal must wait for the query before answering;
// otherwise it exits on EAGAIN and the real probe can only time out.
func readPTYFull(master *os.File, buf []byte) (int, error) {
	deadline := time.Now().Add(2 * time.Second)
	n := 0
	for n < len(buf) {
		read, err := readPTY(master, buf[n:], deadline)
		n += read
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// readPTY also serves the renderer's continuous reader. Darwin's non-pollable
// os.File returns EAGAIN before output arrives; that is readiness, not EOF.
func readPTY(master *os.File, buf []byte, deadline time.Time) (int, error) {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, os.ErrDeadlineExceeded
		}
		fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, int(remaining.Milliseconds())+1); err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return 0, err
		}
		if fds[0].Revents == 0 {
			continue
		}
		n, err := unix.Read(int(master.Fd()), buf)
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EINTR) {
			continue
		}
		if err == nil && n == 0 {
			err = io.ErrUnexpectedEOF
		}
		return max(n, 0), err
	}
}

func TestQueryDA1(t *testing.T) {
	for _, reply := range []string{"\x1b[?62;4;22c", "\x1b[?62;22c", ""} {
		t.Run(reply, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			done := make(chan error, 1)
			go func() {
				query := make([]byte, 3)
				_, err := readPTYFull(master, query)
				if err == nil && string(query) != "\x1b[c" {
					err = io.ErrUnexpectedEOF
				}
				if err == nil && reply != "" {
					// Fragment the DA1 response, as a real terminal may do.
					for _, b := range []byte(reply) {
						if _, err = master.Write([]byte{b}); err != nil {
							break
						}
					}
				}
				done <- err
			}()
			start := time.Now()
			got, err := queryDA1(context.Background(), slave, slave)
			if reply == "" {
				if err == nil || time.Since(start) > 2*time.Second {
					t.Fatalf("timeout: %v", err)
				}
			} else if err != nil || string(got) != reply {
				t.Fatalf("got %q err=%v", got, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			// The cancelled reader is gone and the stream is still usable.
			if _, err := slave.Write([]byte("ok")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 2)
			if _, err := readPTYFull(master, buf); err != nil || string(buf) != "ok" {
				t.Fatalf("terminal not restored: %q %v", buf, err)
			}
		})
	}
}

// fakeTerminal reads the query off the far end of a PTY and answers it in pieces,
// as a real terminal may. It reports the query it saw.
func fakeTerminal(master *os.File, reply string) <-chan string {
	seen := make(chan string, 1)
	go func() {
		query := make([]byte, len("\x1b[16t\x1b[c"))
		n, _ := readPTYFull(master, query)
		seen <- string(query[:n])
		for _, b := range []byte(reply) {
			if _, err := master.Write([]byte{b}); err != nil {
				return
			}
		}
	}()
	return seen
}

const da1Answer = "\x1b[?62;4;22c"

func TestProbeCellFollowsTheTerminalAnswer(t *testing.T) {
	for _, tc := range []struct {
		name          string
		reply         string
		width, height int
	}{
		{"answers", "\x1b[6;12;6t" + da1Answer, 6, 12},
		{"answers a large cell", "\x1b[6;24;12t" + da1Answer, 12, 24},
		{"ignores the query", da1Answer, 0, 0},
		{"answers zeros", "\x1b[6;0;0t" + da1Answer, 0, 0},
		{"answers nothing", "", 0, 0},
		{"answers without DA1", "\x1b[6;12;6t", 6, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			seen := fakeTerminal(master, tc.reply)
			begin := time.Now()
			cell := probeCell(context.Background(), slave, slave)
			if time.Since(begin) > 2*time.Second {
				t.Fatal("the query outlived its timeout")
			}
			if got := <-seen; got != "\x1b[16t\x1b[c" {
				t.Fatalf("query = %q", got)
			}
			if width, height := cell(); width != tc.width || height != tc.height {
				t.Fatalf("cell = %dx%d, want %dx%d", width, height, tc.width, tc.height)
			}
			// Nothing of the answer may stay behind for Bubble Tea to read as keys.
			if _, err := slave.Write([]byte("ok")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 2)
			if _, err := readPTYFull(master, buf); err != nil || string(buf) != "ok" {
				t.Fatalf("terminal not restored: %q %v", buf, err)
			}
		})
	}
}

// DA1 proves the terminal is done, so one that ignores the query costs no timeout.
func TestQueryCellEndsOnTheDA1Reply(t *testing.T) {
	master, slave := openPTY(t)
	defer func() { _ = master.Close(); _ = slave.Close() }()
	fakeTerminal(master, da1Answer)
	if reply, err := queryCell(context.Background(), slave, slave); err != nil || string(reply) != da1Answer {
		t.Fatalf("reply %q err=%v", reply, err)
	}
}

// A window size with the pixel fields filled in needs no question at all.
func TestProbeCellAsksNothingWhenTheWindowSizeHasPixels(t *testing.T) {
	master, slave := openPTY(t)
	defer func() { _ = master.Close(); _ = slave.Close() }()
	ws := unix.Winsize{Row: 30, Col: 100, Xpixel: 700, Ypixel: 420}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &ws); err != nil {
		t.Fatal(err)
	}
	cell := probeCell(context.Background(), slave, slave)
	if width, height := cell(); width != 7 || height != 14 {
		t.Fatalf("cell = %dx%d", width, height)
	}
	if _, err := slave.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := readPTYFull(master, buf); err != nil || string(buf) != "ok" {
		t.Fatalf("something was written first: %q %v", buf, err)
	}
}

// From the terminal's answer to the frame on screen, with a real 6x12 cell and the
// 18 rows of the panel. Whether or not the terminal answers, the chat keeps its rows.
func TestRealCellOf6x12NeverReachesTheChat(t *testing.T) {
	const panelRows, realCellHeight = 18, 12
	for name, reply := range map[string]string{
		"terminal answers":      "\x1b[6;12;6t" + da1Answer,
		"terminal ignores":      da1Answer,
		"terminal stays silent": "",
	} {
		t.Run(name, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			fakeTerminal(master, reply)
			cell := probeCell(context.Background(), slave, slave)
			sixel := player.NewSixelOutput(io.Discard, player.SixelOptions{Cell: cell})
			defer func() { _ = sixel.Close() }()
			sixel.SetPanel(player.SixelPanel{Row: 1, Col: 1, Rows: panelRows, Cols: 120, Visible: true})
			if _, height := sixel.FrameSize(); height > panelRows*realCellHeight {
				t.Fatalf("frame of %dpx takes %.1f rows of a %d-row panel",
					height, float64(height)/realCellHeight, panelRows)
			}
		})
	}
}

// rendererModel is a screen with blank panel rows and a line that keeps changing.
type rendererModel struct {
	sizes chan tea.WindowSizeMsg
	rows  int
	ticks int
}

type (
	tickMsg  struct{}
	clearMsg struct{}
)

func (m rendererModel) Init() tea.Cmd { return nil }

func (m rendererModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.rows = msg.Height
		m.sizes <- msg
	case tickMsg:
		m.ticks++
	case clearMsg:
		return m, tea.ClearScreen
	}
	return m, nil
}

func (m rendererModel) View() string {
	lines := make([]string, max(m.rows, 1))
	lines[len(lines)-1] = "chat line " + strconv.Itoa(m.ticks)
	return strings.Join(lines, "\n")
}

// The real Bubble Tea renderer on a real PTY, against frames arriving at the same
// time: the size still reaches the program through the wrapper, every frame
// crosses whole, and clear and resize get the image back.
func TestSixelOutputWithBubbleTeaRenderer(t *testing.T) {
	t.Run("native PTY", func(t *testing.T) { testSixelRenderer(t, false) })
	t.Run("non-pollable PTY", func(t *testing.T) { testSixelRenderer(t, true) })
}

func testSixelRenderer(t *testing.T, nonPollable bool) {
	master, slave := openPTY(t)
	if nonPollable {
		// Match Darwin on Linux too: NewFile sees a blocking descriptor, then
		// reads return EAGAIN without os.File's runtime poller to retry them.
		fd, err := unix.Dup(int(master.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		_ = master.Close()
		if err := unix.SetNonblock(fd, false); err != nil {
			_ = unix.Close(fd)
			t.Fatal(err)
		}
		master = os.NewFile(uintptr(fd), "non-pollable PTY")
		if err := unix.SetNonblock(fd, true); err != nil {
			_ = master.Close()
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var stream []byte
	readerDone := make(chan struct{})
	stopReader := make(chan struct{})
	var readerErr error
	go func() {
		defer close(readerDone)
		buf := make([]byte, 64*1024)
		for {
			select {
			case <-stopReader:
				return
			default:
			}
			n, err := readPTY(master, buf, time.Now().Add(100*time.Millisecond))
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			mu.Lock()
			stream = append(stream, buf[:n]...)
			mu.Unlock()
			if err != nil {
				readerErr = err
				return
			}
		}
	}()
	snapshot := func() []byte {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Clone(stream)
	}

	sixel := player.NewSixelOutput(slave, player.SixelOptions{Cell: func() (int, int) { return 10, 20 }})
	model := rendererModel{sizes: make(chan tea.WindowSizeMsg, 4)}
	prog := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInput(nil),
		tea.WithOutput(terminalOutput{SixelOutput: sixel, file: slave}),
		tea.WithoutSignalHandler(),
	)
	finished := make(chan error, 1)
	go func() {
		_, err := prog.Run()
		finished <- err
	}()
	programStopped := false
	var stopOnce sync.Once
	stopOutput := func() {
		stopOnce.Do(func() {
			_ = sixel.Close()
			close(stopReader)
			<-readerDone
			_ = slave.Close()
			_ = master.Close()
		})
	}
	t.Cleanup(func() {
		if !programStopped {
			prog.Kill()
			select {
			case err := <-finished:
				if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
					t.Errorf("Run during cleanup: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("program did not stop during cleanup")
			}
		}
		stopOutput()
	})
	waitSize := func(width, height int) {
		t.Helper()
		select {
		case got := <-model.sizes:
			if got.Width != width || got.Height != height {
				t.Fatalf("size = %+v, want %dx%d", got, width, height)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the program never got the window size: the wrapper is not a terminal File")
		}
	}
	waitSize(100, 30)
	sixel.SetPanel(player.SixelPanel{Row: 1, Col: 1, Rows: 18, Cols: 100, Visible: true, Playing: true})

	frame := []byte("\x1bPq\"1;1;480;270#1" + strings.Repeat("!200~-", 500) + "\x1b\\")
	wrapped := append(append([]byte("\x1b7\x1b[?80l\x1b[1;1H"), frame...), "\x1b8"...)
	waitFor := func(what string, ok func([]byte) bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok(snapshot()) {
			select {
			case <-readerDone:
				t.Fatalf("PTY reader stopped waiting for %s: %v", what, readerErr)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("timeout waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	imageAfter := func(mark int) func([]byte) bool {
		return func(s []byte) bool { return bytes.Contains(s[mark:], wrapped) }
	}

	// Chat churn and a single frame: after this only damage can draw again.
	sixel.Submit(bytes.Clone(frame))
	waitFor("the first image", imageAfter(0))
	for range 5 {
		prog.Send(tickMsg{})
		time.Sleep(20 * time.Millisecond)
	}

	prog.Send(clearMsg{})
	waitFor("the clear", func(s []byte) bool { return bytes.Contains(s, []byte("\x1b[2J")) })
	clearAt := bytes.LastIndex(snapshot(), []byte("\x1b[2J"))
	waitFor("the image after the clear", imageAfter(clearAt))

	mark := len(snapshot())
	setSize(t, master, 26, 90)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	waitSize(90, 26)
	sixel.SetPanel(player.SixelPanel{Row: 1, Col: 1, Rows: 18, Cols: 90, Visible: true, Playing: true})
	waitFor("the image after the resize", imageAfter(mark))

	prog.Quit()
	select {
	case err := <-finished:
		programStopped = true
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
	sixel.Submit(bytes.Clone(frame))
	time.Sleep(200 * time.Millisecond)
	stopOutput()

	got := snapshot()
	exit := bytes.LastIndex(got, []byte("\x1b[?1049l"))
	if exit < 0 {
		t.Fatal("alternate screen never left")
	}
	if bytes.Contains(got[exit:], []byte("\x1bP")) {
		t.Fatal("a frame landed on the shell after the program left the alternate screen")
	}
	// Whatever the renderer wrote, every DCS on the wire is the whole wrapped frame.
	if starts, whole := bytes.Count(got, []byte("\x1bP")), bytes.Count(got, wrapped); starts != whole || whole < 3 {
		t.Fatalf("%d DCS on the wire, %d whole", starts, whole)
	}
}
