//go:build unix

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

const (
	// stressPlayerFPS is what --vf=fps asks of mpv.
	stressPlayerFPS = 15
	// stressRebuild is how long the fake takes to bring video back. Real mpv took
	// 0.75 s to 2.4 s on a 2 s GOP, waiting for a keyframe; this keeps the wait
	// without paying for it in every test.
	stressRebuild = 300 * time.Millisecond
)

// The streamlink on PATH during these tests. It takes the real arguments of a real
// session and becomes the player below.
const stressStreamlink = `#!/bin/sh
printf '%s\n' "$*" >> "$TTV_FAKE_LOG"
TTV_APP_HELPER=sixel-player exec "$TTV_APP_BIN" "$@"
`

// The mpv on PATH only answers the detection.
const stressMPV = `#!/bin/sh
echo '  sixel            terminal graphics using sixels'
`

var (
	sixelWidthArg  = regexp.MustCompile(`--vo-sixel-width=([0-9]+)`)
	sixelHeightArg = regexp.MustCompile(`--vo-sixel-height=([0-9]+)`)
	sixelRowsArg   = regexp.MustCompile(`--vo-sixel-rows=([0-9]+)`)
)

// sixelPlayer stands in for streamlink and mpv in one process: it speaks the IPC
// protocol and writes frames to stdout as mpv does, at the size it was last given.
// Like the real one, video that comes back takes a while, and a paused player
// draws a single frame when it does. TTV_FAKE_ROGUE keeps frames coming through
// vid=no, so whatever stops them is the output's own gate.
type sixelPlayer struct {
	mu    sync.Mutex
	width int
	// box and rows are mpv's height and row options; see mpvFrameHeight.
	box, rows   int
	off, paused bool
	owed        bool
	epoch       int
	rogue       bool
	rebuild     time.Duration
	conn        net.Conn
	log         *os.File
}

func runSixelPlayer() {
	args := strings.Join(os.Args[1:], " ")
	match := ipcServerArg.FindStringSubmatch(args)
	if match == nil {
		os.Exit(2)
	}
	listener, err := net.Listen("unix", match[1])
	if err != nil {
		os.Exit(2)
	}
	log, err := os.OpenFile(os.Getenv("TTV_FAKE_IPC_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	conn, err := listener.Accept()
	if err != nil {
		os.Exit(2)
	}
	rebuild, _ := time.ParseDuration(os.Getenv("TTV_FAKE_REBUILD"))
	p := &sixelPlayer{rebuild: rebuild, rogue: os.Getenv("TTV_FAKE_ROGUE") == "1", conn: conn, log: log}
	if strings.Contains(args, "--vo=sixel") {
		p.width = argNumber(sixelWidthArg, args)
		p.box = argNumber(sixelHeightArg, args)
		p.rows = argNumber(sixelRowsArg, args)
		go p.draw()
	}
	p.send(map[string]any{"event": "playback-restart"})
	p.serve()
}

func argNumber(arg *regexp.Regexp, args string) int {
	match := arg.FindStringSubmatch(args)
	if match == nil {
		os.Exit(2)
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		os.Exit(2)
	}
	return n
}

func (p *sixelPlayer) send(message map[string]any) {
	line, err := json.Marshal(message)
	if err != nil {
		os.Exit(2)
	}
	_, _ = p.conn.Write(append(line, '\n'))
}

// draw keeps the frame clock whether or not anything is shown, as mpv's fps filter.
func (p *sixelPlayer) draw() {
	seq := 0
	for range time.Tick(time.Second / stressPlayerFPS) {
		p.mu.Lock()
		show := !p.off && (!p.paused || p.owed)
		p.owed = false
		width, height := p.width, mpvFrameHeight(p.box, p.rows)
		p.mu.Unlock()
		if !show {
			continue
		}
		seq++
		if _, err := os.Stdout.Write(syntheticFrame(seq, width, height)); err != nil {
			os.Exit(0)
		}
	}
}

// mpvFrameHeight is what mpv makes of the height it is given: the last of its rows
// stays free, and the rest is rounded down to a sixel band.
func mpvFrameHeight(box, rows int) int {
	height := box * (rows - 1) / rows
	return height - height%6
}

// serve answers commands until the session goes.
func (p *sixelPlayer) serve() {
	lines := bufio.NewScanner(p.conn)
	lines.Buffer(make([]byte, 64*1024), 1<<20)
	for lines.Scan() {
		_, _ = fmt.Fprintf(p.log, "%d %s\n", time.Now().UnixNano(), lines.Text())
		var request struct {
			Command   []json.RawMessage `json:"command"`
			RequestID *int64            `json:"request_id"`
		}
		if json.Unmarshal(lines.Bytes(), &request) != nil {
			continue
		}
		p.apply(request.Command)
		if request.RequestID != nil {
			p.send(map[string]any{"error": "success", "request_id": *request.RequestID})
		}
	}
	os.Exit(0)
}

func (p *sixelPlayer) apply(command []json.RawMessage) {
	if len(command) != 3 || string(command[0]) != `"set_property"` {
		return
	}
	value := string(command[2])
	switch string(command[1]) {
	case `"options/vo-sixel-width"`:
		p.setSize(value, &p.width)
	case `"options/vo-sixel-height"`:
		p.setSize(value, &p.box)
	case `"options/vo-sixel-rows"`:
		p.setSize(value, &p.rows)
	case `"pause"`:
		p.mu.Lock()
		p.paused = value == "true"
		p.mu.Unlock()
		p.send(map[string]any{"event": "property-change", "id": 2, "name": "pause", "data": value == "true"})
	case `"vid"`:
		p.setTrack(value != `"no"`)
	}
}

func (p *sixelPlayer) setSize(value string, into *int) {
	if n, err := strconv.Atoi(value); err == nil {
		p.mu.Lock()
		*into = n
		p.mu.Unlock()
	}
}

func (p *sixelPlayer) setTrack(on bool) {
	if p.rogue {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.epoch++
	if !on {
		p.off = true
		return
	}
	epoch := p.epoch
	time.AfterFunc(p.rebuild, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.epoch == epoch {
			p.off, p.owed = false, true
		}
	})
}

// floodIRC is a chat server that keeps talking at the rate it is told. Lines go
// through the real parser, with accents, an emoji and lengths that wrap.
type floodIRC struct {
	rate atomic.Int64
	sent atomic.Int64
	quit chan struct{}

	mu    sync.Mutex
	conns []net.Conn
	wg    sync.WaitGroup
}

func newFloodIRC() *floodIRC { return &floodIRC{quit: make(chan struct{})} }

func floodLine(n int64) string {
	text := fmt.Sprintf("ação 🎮 msg %05d", n)
	if n%4 == 0 {
		text += " " + strings.Repeat("kappa ", 25)
	}
	return fmt.Sprintf("@display-name=Viewer;id=m-%d :viewer!viewer@viewer.tmi.twitch.tv PRIVMSG #ptychan :%s\r\n", n, text)
}

func (f *floodIRC) dial(context.Context, string) (net.Conn, error) {
	client, server := net.Pipe()
	f.mu.Lock()
	f.conns = append(f.conns, server)
	f.mu.Unlock()
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		lines := bufio.NewScanner(server)
		flooding := false
		for lines.Scan() {
			if flooding || !strings.HasPrefix(lines.Text(), "JOIN ") {
				continue
			}
			flooding = true
			f.wg.Add(1)
			go f.flood(server)
		}
	}()
	return client, nil
}

func (f *floodIRC) flood(conn net.Conn) {
	defer f.wg.Done()
	if _, err := io.WriteString(conn, ":tmi.twitch.tv 001 tester :Welcome, GLHF!\r\n"); err != nil {
		return
	}
	for {
		wait := 5 * time.Millisecond
		rate := f.rate.Load()
		if rate > 0 {
			wait = time.Second / time.Duration(rate)
		}
		select {
		case <-f.quit:
			return
		case <-time.After(wait):
		}
		if rate == 0 {
			continue
		}
		if _, err := io.WriteString(conn, floodLine(f.sent.Add(1))); err != nil {
			return
		}
	}
}

func (f *floodIRC) close() {
	close(f.quit)
	f.mu.Lock()
	for _, conn := range f.conns {
		_ = conn.Close()
	}
	f.mu.Unlock()
	f.wg.Wait()
}

type stressOptions struct {
	// rogue keeps the player drawing through vid=no.
	rogue bool
	// viaRun goes through Run, detection included, instead of a program built here.
	// Such a run has no handle to send messages to.
	viaRun bool
}

// stressRig is the application on a PTY, with the terminal above on the far end:
// the real Bubble Tea program and renderer, the real model, player session, chat
// client and output, and only streamlink, mpv and the IRC server faked. Everything
// but a few messages reaches it the way it reaches a user: keys and SIGWINCH.
type stressRig struct {
	t      *testing.T
	master *os.File
	term   *wireTerminal
	irc    *floodIRC
	cat    *i18n.Catalog
	prog   *tea.Program
	starts string
	ipc    string
	cols   int
	rows   int
}

func startStressRig(t *testing.T, opts stressOptions) *stressRig {
	t.Helper()
	// The first subscription starts a goroutine of os/signal that never ends.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	signal.Stop(signals)
	goroutines := runtime.NumGoroutine()
	master, slave := openPTY(t)
	term := newWireTerminal(30, 100, master)
	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		term.pump(master)
	}()

	bin := t.TempDir()
	for name, script := range map[string]string{"streamlink": stressStreamlink, "mpv": stressMPV} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	starts, ipc := filepath.Join(bin, "starts.log"), filepath.Join(bin, "ipc.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TTV_APP_BIN", self)
	t.Setenv("TTV_FAKE_LOG", starts)
	t.Setenv("TTV_FAKE_IPC_LOG", ipc)
	t.Setenv("TTV_FAKE_REBUILD", stressRebuild.String())
	t.Setenv("TMUX", "")
	rogue := ""
	if opts.rogue {
		rogue = "1"
	}
	t.Setenv("TTV_FAKE_ROGUE", rogue)
	// A socket path has a short limit; a test's temp dir can exceed it.
	runtimeDir, err := os.MkdirTemp("", "ttv")
	if err != nil {
		t.Fatal(err)
	}

	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	r := &stressRig{
		t: t, master: master, term: term, irc: newFloodIRC(), cat: cat,
		starts: starts, ipc: ipc, cols: 100, rows: 30,
	}
	mgr := player.New(player.Options{RuntimeDir: runtimeDir, StopWait: 500 * time.Millisecond, SocketWait: 10 * time.Second})
	chatMgr := chat.New(chat.Options{Token: "stress-token", Username: "tester", Dial: r.irc.dial})

	finished := make(chan error, 1)
	stopProgram := func() {}
	if opts.viaRun {
		dir := t.TempDir()
		go func() {
			finished <- Run(Options{
				Video: "sixel", Lang: i18n.LangEN,
				ConfigPath: filepath.Join(dir, "config.toml"), StatePath: filepath.Join(dir, "state", "state.json"),
				Source: oneChannelSource{}, Player: mgr, Chat: chatMgr,
				Input: slave, Output: slave, Log: io.Discard,
			})
		}()
		stopProgram = func() { r.press("\x11") }
	} else {
		stopProgram = r.startProgram(slave, mgr, chatMgr, finished)
	}

	t.Cleanup(func() {
		stopProgram()
		select {
		case err := <-finished:
			if err != nil {
				t.Errorf("program ended with %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("program did not exit")
		}
		mgr.Close()
		chatMgr.Close()
		r.irc.close()
		_ = slave.Close()
		_ = master.Close()
		<-pumped
		_ = os.RemoveAll(runtimeDir)
		for _, problem := range term.problems() {
			t.Errorf("corruption: %s", problem)
		}
		if term.unfinished() {
			t.Error("the stream ended inside a sequence")
		}
		awaitGoroutines(t, goroutines)
	})
	return r
}

// startProgram wires what Run wires, keeping the handle Run hides, and returns
// what ends it.
func (r *stressRig) startProgram(slave *os.File, mgr *player.Manager, chatMgr *chat.Manager, finished chan<- error) func() {
	ctx, cancel := context.WithCancel(context.Background())
	cell := func() (int, int) { return wireCellWidth, wireCellHeight }
	output, surface, stopVideo := startVideo(slave, mgr, cell)
	model := ui.New(r.cat,
		ui.WithVideo(true, ""),
		surface,
		ui.WithSource(oneChannelSource{}),
		ui.WithPlayback(newPlaybackBridge(mgr)),
		ui.WithChat(newChatBridge(chatMgr)),
	)
	r.prog = tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithInput(slave),
		tea.WithOutput(output),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	)
	var wg sync.WaitGroup
	wg.Go(func() { forwardPlayerEvents(ctx, r.prog, mgr) })
	wg.Go(func() { forwardChatEvents(ctx, r.prog, chatMgr) })
	go func() {
		_, err := r.prog.Run()
		cancel()
		wg.Wait()
		stopVideo()
		finished <- err
	}()
	return r.prog.Quit
}

// awaitGoroutines waits for the count to fall back: the test's own goroutines end
// a moment after what they serve.
func awaitGoroutines(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			stacks := make([]byte, 1<<20)
			stacks = stacks[:runtime.Stack(stacks, true)]
			t.Errorf("%d goroutines, %d before the rig:\n%s", runtime.NumGoroutine(), before, stacks)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *stressRig) press(keys string) {
	r.t.Helper()
	if _, err := r.master.WriteString(keys); err != nil && !errors.Is(err, os.ErrClosed) {
		r.t.Fatalf("write %q: %v", keys, err)
	}
}

func (r *stressRig) await(what string, ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timeout waiting for %s; screen:\n%s", what, r.term.picture())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *stressRig) awaitText(want string) {
	r.t.Helper()
	r.await(fmt.Sprintf("%q on the screen", want), func() bool { return r.term.contains(want) })
}

// resize is a window drag: the grid and the kernel change, then the signal. A PTY
// without a controlling process has no one to signal, so the test signals itself.
func (r *stressRig) resize(cols, rows int) {
	r.t.Helper()
	r.cols, r.rows = cols, rows
	r.term.resize(rows, cols)
	setSize(r.t, r.master, uint16(rows), uint16(cols))
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		r.t.Fatal(err)
	}
}

func (r *stressRig) lines(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// ipcCount counts the commands the player received that hold want.
func (r *stressRig) ipcCount(want string) int {
	n := 0
	for _, line := range r.lines(r.ipc) {
		if strings.Contains(line, want) {
			n++
		}
	}
	return n
}

const (
	vidOff = `"vid","no"`
	vidOn  = `"vid","auto"`
)

// panelRows is the height of the panel in a terminal of this height: the rule of
// ui.videoRows, from outside, with the chat strip on at its default 8 rows.
func panelRows(height int) int { return max(height-1-8, 0) }

// play takes the path of a user: channel in the window, the mode, then r.
func (r *stressRig) play() {
	r.t.Helper()
	r.awaitText("PtyCategory")
	r.press("\r")
	r.awaitText("PtyChan")
	r.press("\r")
	r.awaitText("Chat — #ptychan")
	r.press(strings.Repeat(string(rune(keyCtrlW)), 4))
	r.awaitText(r.cat.T("video.restart_embedded"))
	r.press("r")
	r.await("the first frames", func() bool { return r.term.frameCount() >= 3 })
	if starts := r.lines(r.starts); len(starts) != 2 || !usesSixel(starts[1]) {
		r.t.Fatalf("starts: %q", starts)
	}
}

// awaitPicture waits for the panel to settle: text gone from its rows, the image
// inside and none over the chat, for long enough that no repaint is still coming.
func (r *stressRig) awaitPicture() {
	r.t.Helper()
	rows := panelRows(r.rows)
	clean := time.Now()
	r.await("the image in the panel", func() bool {
		inside, outside := r.term.imageCells(rows)
		if inside == 0 || outside > 0 || len(r.term.panelText(rows)) > 0 {
			clean = time.Now()
			return false
		}
		return time.Since(clean) >= 300*time.Millisecond
	})
}

// holdPicture fails the moment the panel stops showing a clean image.
func (r *stressRig) holdPicture(hold time.Duration) {
	r.t.Helper()
	rows := panelRows(r.rows)
	for end := time.Now().Add(hold); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if dirty := r.term.panelText(rows); len(dirty) > 0 {
			r.t.Fatalf("text in the panel: %q\n%s", dirty, r.term.picture())
		}
		if inside, outside := r.term.imageCells(rows); inside == 0 || outside > 0 {
			r.t.Fatalf("image: %d cells in the panel, %d over the chat\n%s", inside, outside, r.term.picture())
		}
	}
}

// assertNoImage fails if any cell still shows the image.
func (r *stressRig) assertNoImage() {
	r.t.Helper()
	if inside, outside := r.term.imageCells(r.rows); inside+outside != 0 {
		r.t.Fatalf("image still on screen: %d cells\n%s", inside+outside, r.term.picture())
	}
}

// fits reports whether a frame stays inside the panel of the current size.
func (r *stressRig) fits(f wireFrame) bool {
	return f.width <= r.cols*wireCellWidth && f.height <= panelRows(r.rows)*wireCellHeight
}
