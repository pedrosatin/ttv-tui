//go:build unix

package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// stalledIPC returns a session connected to a peer that never reads, with the
// socket's buffer already full: the next write to it blocks until something
// gives up. Closing the peer at cleanup wakes any write still blocked.
func stalledIPC(t *testing.T, writeWait time.Duration) (*Session, net.Conn) {
	t.Helper()
	endpoint := testEndpoint(t, "stalled")
	ln, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	conn, err := dialIPCOnce(endpoint, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	peer, err := ln.Accept()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("accept: %v", err)
	}
	t.Cleanup(func() {
		_ = peer.Close()
		_ = conn.Close()
	})
	fillSendBuffer(t, conn)

	_, cancel := context.WithCancel(context.Background())
	waitDone := make(chan struct{})
	close(waitDone)
	return &Session{
		cfg: sessionConfig{
			Generation: 1,
			Login:      "stalled",
			WriteWait:  writeWait,
			Emit:       func(Event) {},
		},
		cancel:   cancel,
		state:    StatePlaying,
		volume:   50,
		waitDone: waitDone,
		ipcConn:  conn,
		pending:  make(map[int64]pendingCommand),
	}, peer
}

// fillSendBuffer writes until the kernel takes no more bytes from conn.
func fillSendBuffer(t *testing.T, conn net.Conn) {
	t.Helper()
	chunk := make([]byte, 64<<10)
	for range 1024 {
		if err := conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Fatalf("SetWriteDeadline: %v", err)
		}
		if _, err := conn.Write(chunk); err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("fill: %v", err)
			}
			if err := conn.SetWriteDeadline(time.Time{}); err != nil {
				t.Fatalf("SetWriteDeadline: %v", err)
			}
			return
		}
	}
	t.Fatal("the socket kept accepting writes from a peer that never reads")
}

// A command to an mpv that stopped reading fails once the write deadline
// passes, and the next command is not stuck behind it.
func TestIPCWriteToStalledPeerExpires(t *testing.T) {
	s, _ := stalledIPC(t, 100*time.Millisecond)
	for i := range 2 {
		done := make(chan error, 1)
		go func() { done <- s.setVolume(40 + i) }()
		select {
		case err := <-done:
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("command %d: err = %v, want a write deadline error", i+1, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("command %d still blocked on a peer that does not read", i+1)
		}
	}
	s.cmdMu.Lock()
	left := len(s.pending)
	s.cmdMu.Unlock()
	if left != 0 {
		t.Fatalf("%d failed commands still wait for a reply", left)
	}
}

// Ctrl+Q while autorepeat commands queue behind a write to an mpv that stopped
// reading. The write deadline is far off: Stop must not wait for it, nor for
// cmdMu. Taking mpv down closes its end, and the stuck writes fail at once.
func TestStopDoesNotWaitForBlockedIPCWrites(t *testing.T) {
	s, peer := stalledIPC(t, time.Minute)
	giveIPCPeerToGroup(t, s, peer)

	const queued = 3
	sent := make(chan error, queued)
	for i := range queued {
		go func() { sent <- s.setVolume(40 + i) }()
	}
	// A command holds cmdMu from before its write until the write ends.
	deadline := time.Now().Add(10 * time.Second)
	for s.cmdMu.TryLock() {
		s.cmdMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("no command took the IPC lock")
		}
		runtime.Gosched()
	}

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop blocked behind IPC writes to a peer that does not read")
	}
	for range queued {
		select {
		case err := <-sent:
			if err == nil {
				t.Fatal("a command reported success to a peer that never read it")
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a command never returned")
		}
	}
}

// giveIPCPeerToGroup moves the far end of the session's IPC into a process of
// its own group that never reads it, as mpv is in streamlink's group.
func giveIPCPeerToGroup(t *testing.T, s *Session, peer net.Conn) {
	t.Helper()
	f, err := peer.(*net.UnixConn).File()
	if err != nil {
		t.Fatalf("peer file: %v", err)
	}
	cmd := exec.Command(os.Args[0], "--", "stalled-mpv")
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child")
	cmd.ExtraFiles = []*os.File{f}
	configureProcessGroup(cmd)
	err = cmd.Start()
	_ = f.Close()
	_ = peer.Close()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	group, err := startedGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("group: %v", err)
	}
	waitDone := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waitDone)
	}()
	t.Cleanup(func() {
		_ = group.kill()
		<-waitDone
	})
	s.cmd = cmd
	s.group = group
	s.waitDone = waitDone
	s.cfg.StopWait = 5 * time.Second
}

// The goroutine reading the IPC must not write: with mpv not reading, a write
// from it stops the replies and events behind it.
func TestCommandReplyDoesNotWaitForOSDWrite(t *testing.T) {
	s, _ := stalledIPC(t, time.Minute)
	s.pending[7] = pendingCommand{kind: pendingVolume, volume: 40, osd: true}
	done := make(chan struct{})
	go func() {
		s.handleIPCMessage(ipcMessage{Error: "success", RequestID: 7})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the IPC reader is blocked writing the OSD")
	}
	if got := s.Volume(); got != 40 {
		t.Fatalf("volume = %d after the confirmed reply, want 40", got)
	}
}

// A pause whose write expired never reached mpv, so mpv is still playing. The
// next Space must ask for the pause again, and the reply still confirms it.
func TestPauseAfterExpiredWriteStillPauses(t *testing.T) {
	s, _ := stalledIPC(t, 100*time.Millisecond)
	if err := s.togglePause(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("first Space: err = %v, want a write deadline error", err)
	}

	conn, peer := net.Pipe()
	t.Cleanup(func() {
		_ = peer.Close()
		_ = conn.Close()
	})
	s.cmdMu.Lock()
	s.ipcConn = conn
	s.cmdMu.Unlock()
	line := make(chan []byte, 1)
	go func() {
		b, _ := bufio.NewReader(peer).ReadBytes('\n')
		line <- b
	}()
	if err := s.togglePause(); err != nil {
		t.Fatalf("second Space: %v", err)
	}
	var sent struct {
		Command   []any `json:"command"`
		RequestID int64 `json:"request_id"`
	}
	if err := json.Unmarshal(<-line, &sent); err != nil {
		t.Fatalf("decode the second command: %v", err)
	}
	if want := []any{"set_property", "pause", true}; !reflect.DeepEqual(sent.Command, want) {
		t.Fatalf("second Space sent %v to an mpv still playing, want %v", sent.Command, want)
	}

	s.handleIPCMessage(ipcMessage{Error: "success", RequestID: sent.RequestID})
	s.mu.Lock()
	paused, st := s.paused, s.state
	s.mu.Unlock()
	if !paused || st != StatePaused {
		t.Fatalf("after the confirmed reply: paused = %v, state = %s, want paused", paused, st)
	}
}
