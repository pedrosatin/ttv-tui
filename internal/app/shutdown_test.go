//go:build unix

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"golang.org/x/sys/unix"
)

// These tests re-execute the test binary in helper roles selected by
// TTV_APP_HELPER: the TUI itself (attached to a PTY), a fake streamlink that
// spawns a descendant in its process group, and a run that gets SIGKILLed to
// leave an orphaned player. Nothing touches the network or a real player.

func TestMain(m *testing.M) {
	switch os.Getenv("TTV_APP_HELPER") {
	case "tui":
		os.Exit(runTUIHelper())
	case "player":
		runFakePlayer()
		os.Exit(0)
	case "player-child":
		runFakePlayerChild()
		os.Exit(0)
	case "owner":
		runOrphanOwner()
		os.Exit(0)
	case "sixel-player":
		runSixelPlayer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type emptySource struct{}

func (emptySource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{}, nil
}

func (emptySource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{}, nil
}

func (emptySource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{}, nil
}

func (emptySource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (emptySource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

func helperPlayerCommand(dir string) player.CommandFunc {
	return func(login, socketPath string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "--", login, socketPath)
		cmd.Env = append(os.Environ(), "TTV_APP_HELPER=player", "TTV_APP_DIR="+dir, "TTV_APP_SOCKET="+socketPath)
		return cmd
	}
}

func helperPlayerOptions(dir string) player.Options {
	return player.Options{
		Command:     helperPlayerCommand(dir),
		RuntimeDir:  filepath.Join(dir, "run"),
		RegistryDir: filepath.Join(dir, "state", "players"),
		StopWait:    300 * time.Millisecond,
	}
}

func writePidFile(path string) {
	tmp := path + ".tmp"
	_ = os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())), 0o600)
	_ = os.Rename(tmp, path)
}

// runTUIHelper is the TUI under test: playback already started, then app.Run
// on the PTY it inherited, exiting with the same status mapping as main.
func runTUIHelper() int {
	dir := os.Getenv("TTV_APP_DIR")
	mgr := player.New(helperPlayerOptions(dir))
	defer mgr.Close()
	if _, err := mgr.Start("hup"); err != nil {
		return 3
	}
	opts := Options{
		ConfigPath: filepath.Join(dir, "config.toml"),
		StatePath:  filepath.Join(dir, "state", "state.json"),
		Source:     emptySource{},
		Player:     mgr,
	}
	if os.Getenv("TTV_APP_RICH") == "1" || os.Getenv("TTV_APP_FILTER_CLEAR") == "1" || os.Getenv("TTV_APP_RECENT_ORDER") == "1" || os.Getenv("TTV_APP_SEARCH_ESC") == "1" || os.Getenv("TTV_APP_CHANNEL_FILTER") == "1" || os.Getenv("TTV_APP_FILTER_SCROLL") == "1" {
		// Logged-in chat whose dial never reaches the network, so every UI
		// mode can be entered from the PTY.
		opts.Source = oneChannelSource{}
		if os.Getenv("TTV_APP_FILTER_CLEAR") == "1" {
			opts.Source = filterClearSource{}
			if err := seedFilterClearFavorite(opts.StatePath); err != nil {
				return 3
			}
		}
		if os.Getenv("TTV_APP_RECENT_ORDER") == "1" {
			opts.Source = recentOrderSource{}
			if err := seedRecentOrderState(opts.StatePath); err != nil {
				return 3
			}
		}
		if os.Getenv("TTV_APP_SEARCH_ESC") == "1" {
			opts.Source = searchEscSource{}
		}
		if os.Getenv("TTV_APP_CHANNEL_FILTER") == "1" {
			opts.Source = channelFilterSource{}
		}
		if os.Getenv("TTV_APP_FILTER_SCROLL") == "1" {
			opts.Source = filterScrollSource{}
		}
		dial := func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("offline test")
		}
		if os.Getenv("TTV_APP_USERNOTICE") == "1" {
			dial = dialUserNoticeIRC
		}
		if os.Getenv("TTV_APP_RECONNECT") == "1" {
			dial = newReconnectIRC()
		}
		chatMgr := chat.New(chat.Options{
			Token:    "pty-test-token",
			Username: "tester",
			Dial:     dial,
		})
		defer chatMgr.Close()
		opts.Chat = chatMgr
	}
	return ExitCode(Run(opts))
}

// runFakePlayer mimics streamlink: a descendant (mpv) in the same process
// group, an IPC socket, default signal dispositions.
func runFakePlayer() {
	dir := os.Getenv("TTV_APP_DIR")
	socket := os.Getenv("TTV_APP_SOCKET")
	writePidFile(filepath.Join(dir, "player.pid"))
	child := exec.Command(os.Args[0], "--", "mpv", socket)
	child.Env = append(os.Environ(), "TTV_APP_HELPER=player-child")
	if err := child.Start(); err != nil {
		os.Exit(1)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		os.Exit(1)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 512)
				for {
					if _, err := c.Read(buf); err != nil {
						_ = c.Close()
						return
					}
				}
			}(conn)
		}
	}()
	for {
		time.Sleep(time.Hour)
	}
}

func runFakePlayerChild() {
	writePidFile(filepath.Join(os.Getenv("TTV_APP_DIR"), "child.pid"))
	for {
		time.Sleep(time.Hour)
	}
}

// runOrphanOwner starts a player and waits to be SIGKILLed, leaving the player
// running with a registry record, as a TUI killed abruptly would.
func runOrphanOwner() {
	mgr := player.New(helperPlayerOptions(os.Getenv("TTV_APP_DIR")))
	if _, err := mgr.Start("orphaned"); err != nil {
		os.Exit(3)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitPid(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s not written within %v", path, timeout)
	return 0
}

func groupHasMembers(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || err == syscall.EPERM
}

func records(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "state", "players", "player-*.json"))
	return m
}

// --- PTY plumbing (openPTY and alive live in pty_<goos>_test.go) ---

func setSize(t *testing.T, f *os.File, rows, cols uint16) {
	t.Helper()
	ws := unix.Winsize{Row: rows, Col: cols}
	sc, err := f.SyscallConn()
	if err != nil {
		t.Fatalf("SyscallConn: %v", err)
	}
	var ioErr error
	if err := sc.Control(func(fd uintptr) {
		ioErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &ws)
	}); err != nil || ioErr != nil {
		t.Fatalf("TIOCSWINSZ: %v %v", err, ioErr)
	}
}

type ptyTUI struct {
	t      *testing.T
	dir    string
	master *os.File
	cmd    *exec.Cmd
	done   chan struct{}

	mu  sync.Mutex
	out bytes.Buffer

	playerPid, childPid int
}

// startTUIOnPTY launches the TUI as session leader with the PTY as its
// controlling terminal (like a shell in a terminal window) and waits until it
// has rendered and the player group (parent + descendant) is up.
func startTUIOnPTY(t *testing.T, extraEnv ...string) *ptyTUI {
	t.Helper()
	dir := t.TempDir()
	master, slave := openPTY(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Env = append(cmd.Env, "TTV_APP_HELPER=tui", "TTV_APP_DIR="+dir, "TERM=xterm-256color", "XDG_STATE_HOME="+filepath.Join(dir, "xdg"))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start tui: %v", err)
	}
	_ = slave.Close()

	p := &ptyTUI{t: t, dir: dir, master: master, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.mu.Lock()
				p.out.Write(buf[:n])
				p.mu.Unlock()
				answerTerminalQueries(master, buf[:n])
			}
			if errors.Is(err, syscall.EAGAIN) {
				// Only a master kept off the poller gets here (pty_darwin_test.go).
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-p.done
		}
		_ = master.Close()
		for _, pid := range []int{p.playerPid, p.childPid} {
			if pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		if runtime.GOOS == "linux" {
			deadline := time.Now().Add(time.Second)
			for {
				pids := playerProcesses(p.dir)
				if len(pids) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Errorf("player processes survived cleanup: %v", pids)
					for _, pid := range pids {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	})

	p.playerPid = waitPid(t, filepath.Join(dir, "player.pid"), 10*time.Second)
	p.childPid = waitPid(t, filepath.Join(dir, "child.pid"), 10*time.Second)
	// Alternate-screen enter is written by tea.Program.Run, after Run has
	// installed its signal handling.
	deadline := time.Now().Add(15 * time.Second)
	for !p.rendered() {
		if time.Now().After(deadline) {
			t.Fatalf("TUI did not render; output so far: %q", p.output())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !alive(p.playerPid) || !alive(p.childPid) {
		t.Fatal("player group not running before the test action")
	}
	if len(records(t, dir)) != 1 {
		t.Fatalf("expected one live-player record, got %v", records(t, dir))
	}
	return p
}

// answerTerminalQueries replies like a terminal emulator to the colour and
// cursor queries termenv sends at startup, so it does not wait for its timeout.
func answerTerminalQueries(master *os.File, chunk []byte) {
	if bytes.Contains(chunk, []byte("\x1b]11;?")) {
		_, _ = master.Write([]byte("\x1b]11;rgb:0000/0000/0000\x1b\\"))
	}
	if bytes.Contains(chunk, []byte("\x1b]10;?")) {
		_, _ = master.Write([]byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\"))
	}
	if bytes.Contains(chunk, []byte("\x1b[6n")) {
		_, _ = master.Write([]byte("\x1b[1;1R"))
	}
}

func (p *ptyTUI) output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

func (p *ptyTUI) rendered() bool {
	return strings.Contains(p.output(), "\x1b[?1049h")
}

func (p *ptyTUI) waitExit(timeout time.Duration) int {
	p.t.Helper()
	select {
	case <-p.done:
	case <-time.After(timeout):
		p.t.Fatalf("TUI did not exit within %v", timeout)
	}
	return p.cmd.ProcessState.ExitCode()
}

func (p *ptyTUI) assertPlayerGone() {
	p.t.Helper()
	if alive(p.playerPid) {
		p.t.Errorf("player parent %d survived", p.playerPid)
	}
	if alive(p.childPid) {
		p.t.Errorf("player descendant %d survived", p.childPid)
	}
	if groupHasMembers(p.playerPid) {
		p.t.Errorf("player process group %d still has members", p.playerPid)
	}
	if r := records(p.t, p.dir); len(r) != 0 {
		p.t.Errorf("live-player records left behind: %v", r)
	}
	if left, _ := filepath.Glob(filepath.Join(p.dir, "run", "*", "i")); len(left) != 0 {
		p.t.Errorf("IPC sockets left behind: %v", left)
	}
}

func TestSIGHUPToTUIStopsPlayerGroup(t *testing.T) {
	p := startTUIOnPTY(t)
	if err := syscall.Kill(p.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	code := p.waitExit(10 * time.Second)
	if code != 128+int(syscall.SIGHUP) {
		t.Errorf("exit code = %d, want %d", code, 128+int(syscall.SIGHUP))
	}
	p.assertPlayerGone()
	if !t.Failed() {
		t.Logf("SIGHUP → exit %d; player %d and descendant %d gone", code, p.playerPid, p.childPid)
	}
}

func TestSIGINTAndSIGTERMStopPlayerGroup(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			p := startTUIOnPTY(t)
			if err := syscall.Kill(p.cmd.Process.Pid, sig); err != nil {
				t.Fatalf("send %v: %v", sig, err)
			}
			if code := p.waitExit(10 * time.Second); code != 128+int(sig) {
				t.Errorf("exit code = %d, want %d", code, 128+int(sig))
			}
			p.assertPlayerGone()
		})
	}
}

// Closing the terminal window: the emulator closes the PTY master and the kernel
// hangs up the session (SIGHUP to the controlling process).
func TestClosingTerminalStopsPlayerGroup(t *testing.T) {
	p := startTUIOnPTY(t)
	if err := p.master.Close(); err != nil {
		t.Fatalf("close master: %v", err)
	}
	code := p.waitExit(10 * time.Second)
	if code == 0 {
		t.Errorf("exit code = 0 after hangup, want non-zero")
	}
	p.assertPlayerGone()
	if !t.Failed() {
		t.Logf("terminal closed → exit %d; player group gone", code)
	}
}

// A detached tmux client leaves the pane's PTY open in the tmux server: no
// hangup, no input, at most size changes when another client attaches. That
// must not be treated as the TUI dying. Ctrl+Q afterwards still exits cleanly.
func TestDetachedTerminalKeepsPlayingThenCtrlQStops(t *testing.T) {
	p := startTUIOnPTY(t)
	setSize(t, p.master, 24, 80)
	setSize(t, p.master, 40, 160)
	time.Sleep(800 * time.Millisecond)

	select {
	case <-p.done:
		t.Fatalf("TUI exited while the terminal was only detached/resized (code %d)", p.cmd.ProcessState.ExitCode())
	default:
	}
	if !alive(p.playerPid) || !alive(p.childPid) {
		t.Fatal("player stopped while the terminal was only detached/resized")
	}
	if len(records(t, p.dir)) != 1 {
		t.Fatal("live-player record dropped while still playing")
	}

	if _, err := p.master.Write([]byte{0x11}); err != nil { // Ctrl+Q
		t.Fatalf("write Ctrl+Q: %v", err)
	}
	if code := p.waitExit(10 * time.Second); code != 0 {
		t.Errorf("Ctrl+Q exit code = %d, want 0", code)
	}
	p.assertPlayerGone()
}

// spawnOrphan runs a ttv-tui-like owner that starts a player and is then
// SIGKILLed, so no cleanup runs and the player keeps playing.
func spawnOrphan(t *testing.T, dir string) (playerPid, childPid int) {
	t.Helper()
	owner := exec.Command(os.Args[0], "-test.run=^$")
	owner.Env = append(os.Environ(), "TTV_APP_HELPER=owner", "TTV_APP_DIR="+dir)
	if err := owner.Start(); err != nil {
		t.Fatalf("start owner: %v", err)
	}
	playerPid = waitPid(t, filepath.Join(dir, "player.pid"), 10*time.Second)
	childPid = waitPid(t, filepath.Join(dir, "child.pid"), 10*time.Second)
	t.Cleanup(func() {
		_ = syscall.Kill(-playerPid, syscall.SIGKILL)
	})
	deadline := time.Now().Add(5 * time.Second)
	for len(records(t, dir)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("owner did not record its player")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = owner.Process.Kill()
	_ = owner.Wait()
	time.Sleep(100 * time.Millisecond)
	if !alive(playerPid) || !alive(childPid) {
		t.Fatal("player did not outlive its SIGKILLed owner; nothing to detect")
	}
	return playerPid, childPid
}

func TestHandleOrphansStopsPlayerNonInteractive(t *testing.T) {
	dir := t.TempDir()
	playerPid, childPid := spawnOrphan(t, dir)

	var logBuf bytes.Buffer
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	handleOrphans(playerRegistryDir(filepath.Join(dir, "state", "state.json")),
		strings.NewReader(""), &bytes.Buffer{}, &logBuf, false, cat)
	if alive(playerPid) || alive(childPid) || groupHasMembers(playerPid) {
		t.Fatalf("orphaned player group %d survived startup", playerPid)
	}
	if r := records(t, dir); len(r) != 0 {
		t.Fatalf("orphan record left behind: %v", r)
	}
	if want := fmt.Sprintf("pid %d", playerPid); !strings.Contains(logBuf.String(), want) {
		t.Fatalf("log = %q, want a warning mentioning %q", logBuf.String(), want)
	}
	t.Logf("log: %s", strings.TrimSpace(logBuf.String()))
}

func TestHandleOrphansInteractiveAsksFirst(t *testing.T) {
	dir := t.TempDir()
	playerPid, childPid := spawnOrphan(t, dir)
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	regDir := filepath.Join(dir, "state", "players")

	// Declined: the player keeps playing and is offered again next time.
	var out bytes.Buffer
	handleOrphans(regDir, strings.NewReader("n\nleftover"), &out, &bytes.Buffer{}, true, cat)
	if !strings.Contains(out.String(), "Encerrar antes de continuar?") || !strings.Contains(out.String(), "orphaned") {
		t.Fatalf("prompt = %q", out.String())
	}
	if !alive(playerPid) || len(records(t, dir)) != 1 {
		t.Fatal("declined orphan was stopped or forgotten")
	}

	// Accepted with the default (empty answer).
	out.Reset()
	in := strings.NewReader("\nrest")
	handleOrphans(regDir, in, &out, &bytes.Buffer{}, true, cat)
	if alive(playerPid) || alive(childPid) || groupHasMembers(playerPid) {
		t.Fatalf("accepted orphan group %d survived", playerPid)
	}
	if !strings.Contains(out.String(), "Player da sessão anterior encerrado.") {
		t.Fatalf("output = %q", out.String())
	}
	if rest := make([]byte, 8); true {
		n, _ := in.Read(rest)
		if string(rest[:n]) != "rest" {
			t.Fatalf("prompt consumed input past the answer line: left %q", rest[:n])
		}
	}
}

// playerProcesses finds helper players by the unique socket directory in their command line.
func playerProcesses(dir string) []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), dir+"/run/") {
			pids = append(pids, pid)
		}
	}
	return pids
}
