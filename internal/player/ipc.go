package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"
)

// ipcMessage is one JSON line from the mpv IPC socket: either an event or a
// command reply. Replies carry request_id when the command included one.
type ipcMessage struct {
	Event     string          `json:"event"`
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
	Error     string          `json:"error"`
	RequestID int64           `json:"request_id"`
}

func (e ipcMessage) isCommandReply() bool {
	return e.Event == "" && e.Error != ""
}

func (e ipcMessage) indicatesPlaying() bool {
	if e.Event == "playback-restart" || e.Event == "file-loaded" || e.Event == "start-file" {
		return true
	}
	if e.Event == "property-change" && e.Name == "idle-active" {
		var idle bool
		if err := json.Unmarshal(e.Data, &idle); err == nil && !idle {
			return true
		}
	}
	return false
}

const (
	// ipcDialAttemptTimeout bounds one dial; ipcDialRetry is the pause between
	// attempts while the endpoint is not accepting connections yet.
	ipcDialAttemptTimeout = 500 * time.Millisecond
	ipcDialRetry          = 50 * time.Millisecond
	// ipcRefusedGrace is how long a connection refused by an endpoint that
	// already exists is tolerated. It only has to cover the gap between mpv
	// creating the socket file and starting to listen on it (two consecutive
	// calls, microseconds apart); past it, nobody is going to listen.
	ipcRefusedGrace = 2 * time.Second
)

var (
	// errIPCWaitAborted means the wait was cut short (session cancelled or its
	// process gone), not that the endpoint failed to appear.
	errIPCWaitAborted = errors.New("player: ipc wait aborted")
	// errIPCWaitTimeout means the endpoint never accepted a connection before
	// the deadline.
	errIPCWaitTimeout = errors.New("player: ipc endpoint did not accept connections")
	// errIPCNoListener means the endpoint exists but kept refusing connections
	// for ipcRefusedGrace: mpv never opened its control channel.
	errIPCNoListener = errors.New("player: ipc endpoint exists but nothing listens on it")
)

// dialIPCUntil dials endpoint until it connects. Readiness is the successful
// dial itself, never the existence of a file: that is the only test that works
// the same for a socket file and a named pipe. It gives up with
// errIPCWaitTimeout once deadline passes, or with errIPCWaitAborted when ctx is
// done or gone is closed. An endpoint that is absent (or busy) is waited for
// until deadline; one that is present but keeps refusing connections fails
// with errIPCNoListener after ipcRefusedGrace instead. dialIPCOnce is per OS.
func dialIPCUntil(ctx context.Context, gone <-chan struct{}, endpoint string, deadline time.Time) (net.Conn, error) {
	var last error
	var refusedSince time.Time // zero while the endpoint is not present-and-refusing
	for {
		select {
		case <-ctx.Done():
			return nil, errIPCWaitAborted
		case <-gone:
			return nil, errIPCWaitAborted
		default:
		}

		timeout := ipcDialAttemptTimeout
		if left := time.Until(deadline); left > 0 && left < timeout {
			timeout = left
		}
		conn, err := dialIPCOnce(endpoint, timeout)
		if err == nil {
			return conn, nil
		}
		last = err

		if ipcEndpointRefused(endpoint, err) {
			if refusedSince.IsZero() {
				refusedSince = time.Now()
			} else if time.Since(refusedSince) >= ipcRefusedGrace {
				return nil, fmt.Errorf("%w: %s: %v", errIPCNoListener, endpoint, last)
			}
		} else {
			refusedSince = time.Time{}
		}

		left := time.Until(deadline)
		if left <= 0 {
			return nil, fmt.Errorf("%w: %s: %v", errIPCWaitTimeout, endpoint, last)
		}
		timer := time.NewTimer(min(ipcDialRetry, left))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errIPCWaitAborted
		case <-gone:
			timer.Stop()
			return nil, errIPCWaitAborted
		case <-timer.C:
		}
	}
}

func writeIPC(conn net.Conn, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}

func readIPCMessages(conn net.Conn, handle func(ipcMessage)) error {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg ipcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		handle(msg)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return nil // EOF
}

// isIPCPeerGone reports whether err means the mpv IPC peer closed or reset the
// connection. A clean EOF and ECONNRESET (common when the peer Close()s while our
// observe_property writes are still unread) both end playback. Errors that exist
// only on one OS (Windows reports a closed pipe or socket with its own codes, not
// these) are judged by platformIPCPeerGone.
func isIPCPeerGone(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	return platformIPCPeerGone(err)
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func parseBoolData(data json.RawMessage) (bool, bool) {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		return b, true
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case "true", "yes", "on":
			return true, true
		case "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

func parseVolumeData(data json.RawMessage) (int, bool) {
	var f float64
	if err := json.Unmarshal(data, &f); err == nil {
		return clampVolume(int(f + 0.5)), true
	}
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		return clampVolume(n), true
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		var f2 float64
		if _, err := fmt.Sscanf(s, "%f", &f2); err == nil {
			return clampVolume(int(f2 + 0.5)), true
		}
	}
	return 0, false
}
