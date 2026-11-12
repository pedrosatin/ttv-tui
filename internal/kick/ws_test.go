package kick

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// wsServer accepts one WebSocket connection per request and hands the raw
// connection to serve after the opening handshake.
func wsServer(t *testing.T, serve func(conn net.Conn, rw *bufio.ReadWriter)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
			http.Error(w, "not a websocket handshake", http.StatusBadRequest)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + acceptKey(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n")
		_ = rw.Flush()
		serve(conn, rw)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// serverFrame builds an unmasked frame, as a server sends.
func serverFrame(fin bool, op byte, payload []byte) []byte {
	head := op
	if fin {
		head |= 0x80
	}
	out := []byte{head}
	switch n := len(payload); {
	case n < 126:
		out = append(out, byte(n))
	case n <= 0xFFFF:
		out = append(out, 126)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
	default:
		out = append(out, 127)
		out = binary.BigEndian.AppendUint64(out, uint64(n))
	}
	return append(out, payload...)
}

// readClientFrame reads one frame a client sent and checks it is masked.
func readClientFrame(r io.Reader) (op byte, payload []byte, err error) {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	if head[1]&0x80 == 0 {
		return 0, nil, errors.New("client frame is not masked")
	}
	size := int(head[1] & 0x7F)
	if size == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		size = int(binary.BigEndian.Uint16(ext[:]))
	}
	var mask [4]byte
	if _, err := io.ReadFull(r, mask[:]); err != nil {
		return 0, nil, err
	}
	payload = make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return head[0] & 0x0F, payload, nil
}

func dialTest(t *testing.T, url string) Socket {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	sock, err := dialWebSocket(ctx, url)
	if err != nil {
		t.Fatalf("dialWebSocket: %v", err)
	}
	t.Cleanup(func() { _ = sock.Close() })
	return sock
}

func TestWebSocket_TextBothWays(t *testing.T) {
	long := strings.Repeat("é", 40000) // needs the 16-bit length, then the 64-bit one
	got := make(chan string, 4)
	url := wsServer(t, func(conn net.Conn, rw *bufio.ReadWriter) {
		_, _ = conn.Write(serverFrame(true, wsOpText, []byte("olá")))
		_, _ = conn.Write(serverFrame(true, wsOpText, []byte(long)))
		for {
			op, payload, err := readClientFrame(rw)
			if err != nil {
				return
			}
			if op == wsOpText {
				got <- string(payload)
			}
		}
	})
	sock := dialTest(t, url+"/app/key?protocol=7")
	for _, want := range []string{"olá", long} {
		text, err := sock.ReadText()
		if err != nil || text != want {
			t.Fatalf("ReadText = (%d bytes, %v), want %d bytes", len(text), err, len(want))
		}
	}
	for _, msg := range []string{"curta", strings.Repeat("x", 300)} {
		if err := sock.WriteText(msg); err != nil {
			t.Fatalf("WriteText: %v", err)
		}
		select {
		case text := <-got:
			if text != msg {
				t.Fatalf("server read %d bytes, want %d", len(text), len(msg))
			}
		case <-time.After(testTimeout):
			t.Fatal("server read nothing")
		}
	}
}

func TestWebSocket_ControlFramesAndFragments(t *testing.T) {
	pong := make(chan []byte, 1)
	url := wsServer(t, func(conn net.Conn, rw *bufio.ReadWriter) {
		// A ping between the two halves of a fragmented message, then binary
		// (ignored), then a whole message.
		_, _ = conn.Write(serverFrame(false, wsOpText, []byte("meia ")))
		_, _ = conn.Write(serverFrame(true, wsOpPing, []byte("eco")))
		_, _ = conn.Write(serverFrame(true, wsOpContinuation, []byte("mensagem")))
		_, _ = conn.Write(serverFrame(true, wsOpBinary, []byte{1, 2, 3}))
		_, _ = conn.Write(serverFrame(true, wsOpText, []byte("fim")))
		for {
			op, payload, err := readClientFrame(rw)
			if err != nil {
				return
			}
			if op == wsOpPong {
				pong <- payload
			}
		}
	})
	sock := dialTest(t, url)
	for _, want := range []string{"meia mensagem", "fim"} {
		if text, err := sock.ReadText(); err != nil || text != want {
			t.Fatalf("ReadText = (%q, %v), want %q", text, err, want)
		}
	}
	select {
	case p := <-pong:
		if string(p) != "eco" {
			t.Errorf("pong payload = %q, want the ping's", p)
		}
	case <-time.After(testTimeout):
		t.Fatal("ping got no pong")
	}
}

func TestWebSocket_CloseFromServer(t *testing.T) {
	echoed := make(chan byte, 1)
	url := wsServer(t, func(conn net.Conn, rw *bufio.ReadWriter) {
		// 4001 with a reason, as Pusher closes on a bad key.
		_, _ = conn.Write(serverFrame(true, wsOpClose, append([]byte{0x0F, 0xA1}, "App key not in this cluster"...)))
		if op, _, err := readClientFrame(rw); err == nil {
			echoed <- op
		}
	})
	sock := dialTest(t, url)
	_, err := sock.ReadText()
	if !errors.Is(err, errSocketClosed) || !strings.Contains(err.Error(), "4001 App key") {
		t.Fatalf("ReadText err = %v, want the close code and reason", err)
	}
	select {
	case op := <-echoed:
		if op != wsOpClose {
			t.Errorf("client answered the close with opcode %#x", op)
		}
	case <-time.After(testTimeout):
		t.Fatal("client did not echo the close")
	}
}

func TestWebSocket_CloseUnblocksRead(t *testing.T) {
	url := wsServer(t, func(conn net.Conn, rw *bufio.ReadWriter) {
		_, _, _ = readClientFrame(rw) // the close frame, or the end of the connection
	})
	sock := dialTest(t, url)
	done := make(chan error, 1)
	go func() {
		_, err := sock.ReadText()
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	if err := sock.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = sock.Close() // idempotent
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ReadText returned no error after Close")
		}
	case <-time.After(testTimeout):
		t.Fatal("ReadText still blocked after Close")
	}
	if err := sock.WriteText("tarde"); err == nil {
		t.Error("WriteText after Close: want an error")
	}
}

func TestWebSocket_ProtocolViolations(t *testing.T) {
	tests := map[string][]byte{
		"reserved bit":               {0xC1, 0x00},
		"continuation out of place":  serverFrame(true, wsOpContinuation, []byte("x")),
		"fragmented control frame":   serverFrame(false, wsOpPing, nil),
		"oversized control frame":    serverFrame(true, wsOpPing, make([]byte, 126)),
		"unknown opcode":             serverFrame(true, 0x3, nil),
		"message over the limit":     {0x81, 127, 0, 0, 0, 0, 0, 0x20, 0, 0},
		"message inside a fragment":  append(serverFrame(false, wsOpText, []byte("a")), serverFrame(true, wsOpText, []byte("b"))...),
		"connection cut mid-message": {0x81, 0x05, 'a'},
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			url := wsServer(t, func(conn net.Conn, _ *bufio.ReadWriter) { _, _ = conn.Write(raw) })
			if text, err := dialTest(t, url).ReadText(); err == nil {
				t.Fatalf("ReadText = %q, want an error", text)
			}
		})
	}
}

func TestWebSocket_HandshakeFailures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	defer refused.Close()
	if _, err := dialWebSocket(ctx, "ws"+strings.TrimPrefix(refused.URL, "http")); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("refused handshake err = %v, want the status", err)
	}

	wrongKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, _ := w.(http.Hijacker).Hijack()
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: errado\r\n\r\n")
		_ = rw.Flush()
	}))
	defer wrongKey.Close()
	if _, err := dialWebSocket(ctx, "ws"+strings.TrimPrefix(wrongKey.URL, "http")); err == nil || !strings.Contains(err.Error(), "accept") {
		t.Errorf("wrong accept key err = %v", err)
	}

	for _, bad := range []string{"http://example.com/x", "://nope"} {
		if _, err := dialWebSocket(ctx, bad); err == nil {
			t.Errorf("dialWebSocket(%q): want an error", bad)
		}
	}

	// A server that accepts the connection and never answers: the context ends
	// the handshake.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		if conn, err := ln.Accept(); err == nil {
			defer func() { _ = conn.Close() }()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	short, cancelShort := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelShort()
	if _, err := dialWebSocket(short, "ws://"+ln.Addr().String()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("silent server err = %v, want the deadline", err)
	}
}
