package player

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// SetMute while ipcConn is nil must keep the wish so applyPlaybackBaseline
// mutes when the socket connects. MuteMainForPreview during Starting hits this
// path; swallowing the wish leaves the main session with sound.
func TestSetMuteWithoutIPCKeepsWishForBaseline(t *testing.T) {
	s := &Session{
		cfg:     sessionConfig{WriteWait: time.Second, Volume: 80},
		state:   StateStarting,
		pending: make(map[int64]pendingCommand),
		volume:  80,
	}
	err := s.setMute(true)
	if !isIPCNotConnected(err) {
		t.Fatalf("SetMute without IPC: err=%v, want errIPCNotConnected", err)
	}
	s.mu.Lock()
	has, want := s.hasWantMuted, s.wantMuted
	s.mu.Unlock()
	if !has || !want {
		t.Fatalf("wish cleared after ipc-not-connected: has=%v want=%v", has, want)
	}

	conn, peer := net.Pipe()
	t.Cleanup(func() {
		_ = peer.Close()
		_ = conn.Close()
	})
	s.cmdMu.Lock()
	s.ipcConn = conn
	s.cmdMu.Unlock()

	lines := make(chan string, 8)
	go func() {
		r := bufio.NewReader(peer)
		for {
			b, err := r.ReadBytes('\n')
			if len(b) > 0 {
				lines <- string(b)
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()

	s.applyPlaybackBaseline()

	deadline := time.After(2 * time.Second)
	sawMute := false
	for !sawMute {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("peer closed before mute command")
			}
			var msg struct {
				Command []any `json:"command"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &msg); err != nil {
				continue
			}
			if len(msg.Command) >= 3 && msg.Command[0] == "set_property" && msg.Command[1] == "mute" && msg.Command[2] == true {
				sawMute = true
			}
		case <-deadline:
			t.Fatal("applyPlaybackBaseline did not send mute true for the pending wish")
		}
	}
}
