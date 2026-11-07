//go:build unix

package app

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

func TestQueryDA1(t *testing.T) {
	for _, reply := range []string{"\x1b[?62;4;22c", "\x1b[?62;22c", ""} {
		t.Run(reply, func(t *testing.T) {
			master, slave := openPTY(t)
			defer func() { _ = master.Close(); _ = slave.Close() }()
			done := make(chan error, 1)
			go func() {
				query := make([]byte, 3)
				_, err := io.ReadFull(master, query)
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
			if _, err := io.ReadFull(master, buf); err != nil || string(buf) != "ok" {
				t.Fatalf("terminal not restored: %q %v", buf, err)
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
	master, slave := openPTY(t)
	var mu sync.Mutex
	var stream []byte
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 64*1024)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			stream = append(stream, buf[:n]...)
			mu.Unlock()
			if err != nil {
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
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
	sixel.Submit(bytes.Clone(frame))
	time.Sleep(200 * time.Millisecond)
	_ = sixel.Close()
	_ = slave.Close()
	_ = master.Close()
	<-readerDone

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
