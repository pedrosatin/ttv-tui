package player

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// realMPVEnv turns on the test against a real mpv binary, which CI only has
// where a step installed it:
//
//	TTV_TEST_REAL_MPV=1 go test -count=1 -v -run TestRealMPV ./internal/player
const realMPVEnv = "TTV_TEST_REAL_MPV"

// mpvLines is the read side of one connection to mpv.
type mpvLines struct {
	conn net.Conn
	in   chan ipcMessage
	done chan error
	// seen keeps what one wait skipped: an event can arrive while waiting for
	// the reply to the command that caused it.
	seen []ipcMessage
}

func readMPV(conn net.Conn) *mpvLines {
	l := &mpvLines{conn: conn, in: make(chan ipcMessage, 64), done: make(chan error, 1)}
	go func() {
		l.done <- readIPCMessages(conn, func(m ipcMessage) { l.in <- m })
	}()
	return l
}

// waitFor returns the first message match accepts, seen or still to come.
func (l *mpvLines) waitFor(t *testing.T, what string, match func(ipcMessage) bool) ipcMessage {
	t.Helper()
	for i, msg := range l.seen {
		if match(msg) {
			l.seen = slices.Delete(l.seen, i, i+1)
			return msg
		}
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg := <-l.in:
			if match(msg) {
				return msg
			}
			l.seen = append(l.seen, msg)
		case err := <-l.done:
			t.Fatalf("connection ended waiting for %s: %v", what, err)
		case <-deadline:
			t.Fatalf("timeout waiting for %s", what)
		}
	}
}

// request sends one command and returns its successful reply.
func (l *mpvLines) request(t *testing.T, id int64, command ...any) ipcMessage {
	t.Helper()
	if err := writeIPC(l.conn, ipcWriteWait, map[string]any{"command": command, "request_id": id}); err != nil {
		t.Fatalf("write %v: %v", command, err)
	}
	reply := l.waitFor(t, "reply", func(m ipcMessage) bool { return m.isCommandReply() && m.RequestID == id })
	if reply.Error != "success" {
		t.Fatalf("%v: mpv answered %q", command, reply.Error)
	}
	return reply
}

// The whole contract with the real player, through the production dial: mpv opens
// the endpoint the OS's way (socket file or named pipe), takes the same commands
// the session sends, loads the session's input.conf, and ending it reads as the
// peer being gone.
func TestRealMPVHandshake(t *testing.T) {
	if os.Getenv(realMPVEnv) != "1" {
		t.Skipf("set %s=1 to run against a real mpv", realMPVEnv)
	}
	mpv, err := exec.LookPath("mpv")
	if err != nil {
		t.Fatalf("%s is set but mpv is not on PATH: %v", realMPVEnv, err)
	}

	conf := inputConfPath(privateDir(t))
	if err := writeInputConf(conf); err != nil {
		t.Fatalf("writeInputConf: %v", err)
	}
	endpoint := testEndpoint(t, "real-mpv")
	cmd := exec.Command(mpv,
		"--no-config", "--no-input-default-bindings", "--idle", "--vo=null", "--ao=null",
		"--no-osc", "--osd-level=1", "--osd-bar=no",
		"--volume-max=100", "--input-conf="+conf, "--input-ipc-server="+endpoint)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	// A launcher (Chocolatey's shim) can leave the output pipes with a child.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mpv: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	stop := sync.OnceFunc(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	t.Cleanup(stop)

	// No gone channel: the process started here may be that launcher, which can
	// exit while mpv goes on. A dead mpv costs the wait, and its output says why.
	conn, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(15*time.Second))
	if err != nil {
		stop()
		t.Fatalf("dialIPCUntil: %v\nmpv output:\n%s", err, out.String())
	}
	defer func() { _ = conn.Close() }()
	lines := readMPV(conn)

	// The session's handshake: observers, then the volume and pause baseline.
	lines.request(t, 1, "observe_property", 1, "idle-active")
	lines.request(t, 2, "observe_property", 2, "pause")
	lines.request(t, 3, "observe_property", 3, "volume")
	lines.request(t, 4, "set_property", "volume", 42)
	lines.request(t, 5, "set_property", "pause", true)

	lines.waitFor(t, "volume change", func(m ipcMessage) bool {
		vol, ok := parseVolumeData(m.Data)
		return m.Event == "property-change" && m.Name == "volume" && ok && vol == 42
	})
	lines.waitFor(t, "pause change", func(m ipcMessage) bool {
		paused, ok := parseBoolData(m.Data)
		return m.Event == "property-change" && m.Name == "pause" && ok && paused
	})

	// mpv read the file at this OS's path syntax and took every binding from it.
	reply := lines.request(t, 6, "get_property", "input-bindings")
	var bindings []struct {
		Key     string `json:"key"`
		Command string `json:"cmd"`
		Section string `json:"section"`
	}
	if err := json.Unmarshal(reply.Data, &bindings); err != nil {
		t.Fatalf("input-bindings: %v", err)
	}
	var cycle bool
	for _, b := range bindings {
		cycle = cycle || (b.Key == "SPACE" && b.Command == "no-osd cycle pause; "+pauseOSD)
	}
	if !cycle {
		t.Fatalf("input.conf at %s not loaded; bindings: %s", filepath.ToSlash(conf), reply.Data)
	}

	// Exercise the generated bindings, then capture exactly what show-text expands.
	for i, tc := range []struct{ key, text, want string }{
		{"SPACE", pauseText, "▶"},
		{"SPACE", pauseText, "‖"},
		{"+", "${volume}%", "47%"},
	} {
		id := int64(20 + i*2)
		lines.request(t, id, "keypress", tc.key)
		lines.waitFor(t, "binding property change", func(m ipcMessage) bool {
			if m.Event != "property-change" {
				return false
			}
			if tc.key == "SPACE" {
				paused, ok := parseBoolData(m.Data)
				return m.Name == "pause" && ok && paused == (tc.want == "‖")
			}
			vol, ok := parseVolumeData(m.Data)
			return m.Name == "volume" && ok && vol == 47
		})
		reply := lines.request(t, id+1, "expand-text", tc.text)
		var got string
		if err := json.Unmarshal(reply.Data, &got); err != nil || got != tc.want {
			t.Fatalf("OSD after %s = %s, want %q (decode: %v)", tc.key, reply.Data, tc.want, err)
		}
		t.Logf("keypress %s -> show-text %q -> %s", tc.key, tc.text, got)
	}

	if err := writeIPC(conn, ipcWriteWait, map[string]any{"command": []any{"quit"}}); err != nil {
		t.Fatalf("write quit: %v", err)
	}
	for {
		select {
		case <-lines.in:
		case err := <-lines.done:
			if !isIPCPeerGone(err) {
				t.Fatalf("read after quit = %v, not peer gone", err)
			}
			return
		case <-time.After(10 * time.Second):
			t.Fatal("mpv kept the connection open after quit")
		}
	}
}
