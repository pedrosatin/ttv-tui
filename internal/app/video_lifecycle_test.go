//go:build unix

package app

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// fakeStreamlink records its arguments and stays alive. Asked for sixel it keeps
// writing frames: it ignores vid=no on purpose, so the only thing between it and
// the terminal is the output's own gate.
const fakeStreamlink = `#!/bin/sh
printf '%s\n' "$*" >> "$TTV_FAKE_LOG"
case "$*" in
*--vo=sixel*)
	while :; do
		printf '\033Pq"1;1;64;36#1~~~~-\033\\'
		sleep 0.02
	done
	;;
esac
exec sleep 60
`

var ipcServerArg = regexp.MustCompile(`--input-ipc-server=(\S+)`)

type stubChat struct{}

func (stubChat) Join(string) tea.Cmd { return nil }
func (stubChat) Reconnect() tea.Cmd  { return nil }
func (stubChat) Stop() tea.Cmd       { return nil }

// fakeMPV is the far end of one session's IPC socket.
type fakeMPV struct {
	conn net.Conn

	mu       sync.Mutex
	commands []string
	gone     bool
}

func (f *fakeMPV) read() {
	sc := bufio.NewScanner(f.conn)
	for sc.Scan() {
		f.mu.Lock()
		f.commands = append(f.commands, sc.Text())
		f.mu.Unlock()
	}
	f.mu.Lock()
	f.gone = true
	f.mu.Unlock()
}

func (f *fakeMPV) received(command string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.Contains(c, command) {
			return true
		}
	}
	return false
}

func (f *fakeMPV) alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.gone
}

// videoRig is the whole path from a key to the process: the real model, bridge,
// manager and session against a fake streamlink.
type videoRig struct {
	t     *testing.T
	mgr   *player.Manager
	cat   *i18n.Catalog
	model ui.Model
	log   string

	mu     sync.Mutex
	screen []byte
}

func newVideoRig(t *testing.T, flag bool) *videoRig {
	t.Helper()
	bin := t.TempDir()
	for name, script := range map[string]string{"streamlink": fakeStreamlink, "mpv": "#!/bin/sh\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(bin, "args.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TTV_FAKE_LOG", log)
	// A socket path has a short limit; a test's temp dir can exceed it.
	runtimeDir, err := os.MkdirTemp("", "ttv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })

	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	mgr := player.New(player.Options{RuntimeDir: runtimeDir, StopWait: 500 * time.Millisecond, SocketWait: 10 * time.Second})
	r := &videoRig{t: t, mgr: mgr, cat: cat, log: log}

	var surface ui.Option
	if flag {
		screen, terminal, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			buf := make([]byte, 64*1024)
			for {
				n, err := screen.Read(buf)
				r.mu.Lock()
				r.screen = append(r.screen, buf[:n]...)
				r.mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		var stop func()
		_, surface, stop = startVideo(terminal, mgr, nil)
		t.Cleanup(func() {
			stop()
			_ = terminal.Close()
			<-drained
			_ = screen.Close()
		})
	}
	t.Cleanup(mgr.Close)

	r.model = ui.New(cat,
		ui.WithVideo(flag, ""),
		surface,
		ui.WithPlayback(newPlaybackBridge(mgr)),
		ui.WithChat(stubChat{}),
	)
	r.send(tea.WindowSizeMsg{Width: 120, Height: 32})
	return r
}

func (r *videoRig) send(msg tea.Msg) {
	r.t.Helper()
	next, cmd := r.model.Update(msg)
	r.model = next.(ui.Model)
	r.run(cmd)
}

func (r *videoRig) run(cmd tea.Cmd) {
	r.t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			r.run(c)
		}
	default:
		r.send(msg)
	}
}

func (r *videoRig) press(k tea.KeyMsg, times int) {
	r.t.Helper()
	for range times {
		r.send(k)
	}
}

func (r *videoRig) open(login string) {
	r.t.Helper()
	r.send(ui.ChannelSelectedMsg{Channel: twitch.Channel{Login: login}})
}

func (r *videoRig) starts() []string {
	raw, err := os.ReadFile(r.log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (r *videoRig) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Count(r.screen, []byte("\x1bP"))
}

func (r *videoRig) screenRow(row int) string {
	return strings.Split(ansi.Strip(r.model.View()), "\n")[row]
}

// await polls until ok, feeding player events to the model meanwhile.
func (r *videoRig) await(what string, ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timeout waiting for %s; starts: %q", what, r.starts())
		}
		select {
		case ev := <-r.mgr.Events():
			r.send(mapPlayerEvent(ev))
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// settle lets player events and debounced work land.
func (r *videoRig) settle() {
	r.t.Helper()
	end := time.Now().Add(400 * time.Millisecond)
	r.await("the settle window", func() bool { return time.Now().After(end) })
}

// serve waits for start number n and plays the mpv on its socket until Playing.
func (r *videoRig) serve(n int) *fakeMPV {
	r.t.Helper()
	r.await("the player to start", func() bool { return len(r.starts()) >= n })
	match := ipcServerArg.FindStringSubmatch(r.starts()[n-1])
	if match == nil {
		r.t.Fatalf("no IPC endpoint in %q", r.starts()[n-1])
	}
	listener, err := net.Listen("unix", match[1])
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := listener.Accept(); err == nil {
			accepted <- conn
		}
	}()
	var conn net.Conn
	r.await("the session to dial", func() bool {
		select {
		case conn = <-accepted:
			return true
		default:
			return false
		}
	})
	r.t.Cleanup(func() { _ = conn.Close() })
	mpv := &fakeMPV{conn: conn}
	go mpv.read()
	if _, err := conn.Write([]byte(`{"event":"playback-restart"}` + "\n")); err != nil {
		r.t.Fatal(err)
	}
	r.await("the playing state", func() bool { return r.model.PlayerState() == ui.PlayerStatePlaying })
	return mpv
}

func usesSixel(args string) bool {
	return strings.Contains(args, "--vo=sixel")
}

var (
	cycleKey = tea.KeyMsg{Type: tea.KeyCtrlW}
	restart  = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}
)

// embeddedRig reaches the state a user reaches: window first, then r in the mode.
func embeddedRig(t *testing.T) (*videoRig, *fakeMPV) {
	t.Helper()
	r := newVideoRig(t, true)
	r.open("gaules")
	r.serve(1)
	r.press(cycleKey, 4)
	r.press(restart, 1)
	mpv := r.serve(2)
	return r, mpv
}

func TestVideoSplitSessionUsesTheWindow(t *testing.T) {
	r := newVideoRig(t, true)
	r.open("gaules")
	r.serve(1)
	args := r.starts()[0]
	if usesSixel(args) || !strings.Contains(args, "--force-window=immediate") || strings.Contains(args, "--force-window=no") {
		t.Fatalf("split session did not get the window: %s", args)
	}
	r.settle()
	if n := r.frames(); n != 0 {
		t.Fatalf("%d frames outside the mode", n)
	}
}

func TestVideoChannelOpenedInTheModeIsEmbedded(t *testing.T) {
	r := newVideoRig(t, true)
	r.open("gaules")
	r.serve(1)
	r.press(cycleKey, 4)
	if r.model.LayoutMode() != ui.LayoutVideo {
		t.Fatalf("layout = %q", r.model.LayoutMode())
	}
	r.open("alanzoka")
	r.serve(2)
	if args := r.starts()[1]; !usesSixel(args) {
		t.Fatalf("session opened in the mode got no sixel: %s", args)
	}
	r.await("frames in the panel", func() bool { return r.frames() > 0 })
}

func TestVideoEnteringTheModeWarnsAndNeverRestarts(t *testing.T) {
	r := newVideoRig(t, true)
	r.open("gaules")
	mpv := r.serve(1)
	r.press(cycleKey, 4)
	// A smaller panel asks the player for a new frame size, which must stop short
	// of a session that draws in a window.
	r.send(tea.WindowSizeMsg{Width: 90, Height: 15})
	r.settle()
	if row := r.screenRow(0); !strings.Contains(row, r.cat.T("video.restart_embedded")) {
		t.Fatalf("panel row: %q", row)
	}
	if n := len(r.starts()); n != 1 {
		t.Fatalf("the player restarted on its own: %q", r.starts())
	}
	if !mpv.alive() {
		t.Fatal("the session was torn down")
	}
	if mpv.received(`"vid"`) || mpv.received("vo-sixel") {
		t.Fatalf("window session touched: %q", mpv.commands)
	}
}

func TestVideoLeavingKeepsAudioAndStopsFrames(t *testing.T) {
	r, mpv := embeddedRig(t)
	r.await("frames in the panel", func() bool { return r.frames() > 0 })

	r.press(cycleKey, 1)
	r.await("vid=no", func() bool { return mpv.received(`"vid","no"`) })
	r.settle()
	if !mpv.alive() || len(r.starts()) != 2 {
		t.Fatalf("leaving the mode touched the session: alive=%v starts=%q", mpv.alive(), r.starts())
	}
	if bar := ansi.Strip(r.model.StatusBar()); !strings.Contains(bar, r.cat.T("video.audio_only")) {
		t.Fatalf("status bar: %q", bar)
	}
	drawn := r.frames()
	r.settle()
	if n := r.frames(); n != drawn {
		t.Fatalf("%d frames emitted outside the mode", n-drawn)
	}

	// Back in the mode the same session draws again, with no restart.
	r.press(cycleKey, 4)
	r.await("vid=auto", func() bool { return mpv.received(`"vid","auto"`) })
	r.await("frames back in the panel", func() bool { return r.frames() > drawn })
	if n := len(r.starts()); n != 2 {
		t.Fatalf("returning restarted the player: %q", r.starts())
	}
}

func TestVideoRestartKeyUsesTheOutputOfTheMode(t *testing.T) {
	r, _ := embeddedRig(t)
	if args := r.starts()[1]; !usesSixel(args) {
		t.Fatalf("r in the mode: %s", args)
	}
	r.press(cycleKey, 1)
	r.press(restart, 1)
	r.serve(3)
	if args := r.starts()[2]; usesSixel(args) {
		t.Fatalf("r outside the mode kept sixel: %s", args)
	}
	if bar := ansi.Strip(r.model.StatusBar()); strings.Contains(bar, r.cat.T("video.audio_only")) {
		t.Fatalf("stale notice: %q", bar)
	}
}

// setVolume is a volume change as mpv reports it, and waits for the model to see it.
func (r *videoRig) setVolume(mpv *fakeMPV, volume int) {
	r.t.Helper()
	line := fmt.Sprintf(`{"event":"property-change","id":3,"name":"volume","data":%d}`+"\n", volume)
	if _, err := mpv.conn.Write([]byte(line)); err != nil {
		r.t.Fatal(err)
	}
	r.await("the volume", func() bool { return r.model.PlayerVolume() == volume })
}

func TestVideoRestartKeyKeepsTheVolume(t *testing.T) {
	r := newVideoRig(t, true)
	r.open("gaules")
	window := r.serve(1)
	r.setVolume(window, 40)

	r.press(cycleKey, 4)
	r.press(restart, 1)
	if got := r.model.PlayerVolume(); got != 40 {
		t.Fatalf("the bar went to %d on the restart", got)
	}
	embedded := r.serve(2)
	r.await("the volume to reach the new session", func() bool { return embedded.received(`"volume",40`) })
	if embedded.received(`"volume",100`) {
		t.Fatalf("the restart went through the baseline: %q", embedded.commands)
	}

	// Only another channel goes back to the baseline.
	r.open("alanzoka")
	other := r.serve(3)
	r.await("the baseline", func() bool { return other.received(`"volume",100`) })
	if got := r.model.PlayerVolume(); got != 100 {
		t.Fatalf("another channel kept volume %d", got)
	}
}

var (
	escKey  = tea.KeyMsg{Type: tea.KeyEsc}
	listKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
)

// The whole way through the terminal: with the list open no frame gets out, and
// the session goes on with its audio.
func TestVideoListOverThePanelStopsTheFrames(t *testing.T) {
	for name, open := range map[string]tea.KeyMsg{"esc": escKey, "l": listKey} {
		t.Run(name, func(t *testing.T) {
			r, mpv := embeddedRig(t)
			r.await("frames in the panel", func() bool { return r.frames() > 0 })

			r.press(open, 1)
			r.await("vid=no", func() bool { return mpv.received(`"vid","no"`) })
			r.settle()
			drawn := r.frames()
			r.settle()
			if n := r.frames(); n != drawn {
				t.Fatalf("%d frames under the list", n-drawn)
			}
			if !mpv.alive() || len(r.starts()) != 2 {
				t.Fatalf("the list touched the session: alive=%v starts=%q", mpv.alive(), r.starts())
			}
			if r.model.LayoutMode() != ui.LayoutVideo {
				t.Fatalf("layout = %q", r.model.LayoutMode())
			}

			r.press(escKey, 1)
			r.await("frames after the list", func() bool { return r.frames() > drawn })
			r.await("vid=auto", func() bool { return mpv.received(`"vid","auto"`) })
			if n := len(r.starts()); n != 2 {
				t.Fatalf("closing restarted the player: %q", r.starts())
			}
		})
	}
}

func TestVideoWithoutTheFlagNothingChanges(t *testing.T) {
	r := newVideoRig(t, false)
	r.open("gaules")
	mpv := r.serve(1)
	for range 5 {
		r.press(cycleKey, 1)
		if r.model.LayoutMode() == ui.LayoutVideo {
			t.Fatal("the mode exists without the flag")
		}
		r.press(restart, 1)
	}
	r.settle()
	if n := len(r.starts()); n != 1 || usesSixel(r.starts()[0]) {
		t.Fatalf("starts: %q", r.starts())
	}
	if mpv.received(`"vid"`) {
		t.Fatalf("session touched: %q", mpv.commands)
	}
}
