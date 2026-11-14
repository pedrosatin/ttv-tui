package kick

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" // RFC 6455 defines the handshake with SHA-1; nothing here relies on it for security.
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Socket is the text side of a WebSocket connection, all the chat needs. Tests
// replace it with a scripted one.
type Socket interface {
	// ReadText blocks for the next text message. It fails once the connection
	// is closed, by either side.
	ReadText() (string, error)
	WriteText(text string) error
	Close() error
}

// DialFunc opens a Socket to a ws:// or wss:// URL.
type DialFunc func(ctx context.Context, rawURL string) (Socket, error)

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	// wsMaxMessage limita o tamanho máximo de uma mensagem recebida a 64 KiB.
	// Eventos de chat da Kick têm poucas centenas de bytes; o teto estrito
	// reduz o pior caso de consumo de memória com a fila de leitura cheia (H29).
	wsMaxMessage = 64 * 1024
	// wsMaxControl is the payload limit of a control frame (RFC 6455, 5.5).
	wsMaxControl     = 125
	wsWriteTimeout   = 10 * time.Second
	wsCloseTimeout   = time.Second
	wsDialTimeout    = 10 * time.Second
	wsOpContinuation = 0x0
	wsOpText         = 0x1
	wsOpBinary       = 0x2
	wsOpClose        = 0x8
	wsOpPing         = 0x9
	wsOpPong         = 0xA

	wsCloseNormalClosure = 1000
	wsCloseProtocolError = 1002
	wsCloseMessageTooBig = 1009
)

// errSocketClosed is what ReadText returns after the peer sent a close frame.
var errSocketClosed = errors.New("websocket: closed by peer")

// wsConn is a minimal RFC 6455 client: text messages, ping/pong and close, no
// extensions. The chat is the only user and reads far more than it writes.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	wmu       sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

// dialWebSocket opens rawURL and completes the opening handshake within the
// deadline of ctx.
func dialWebSocket(ctx context.Context, rawURL string) (Socket, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("websocket: parse url: %w", err)
	}
	var secure bool
	switch strings.ToLower(u.Scheme) {
	case "wss":
		secure = true
	case "ws":
	default:
		return nil, fmt.Errorf("websocket: unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "80"
		if secure {
			port = "443"
		}
	}
	addr := net.JoinHostPort(host, port)

	dialer := &net.Dialer{Timeout: wsDialTimeout}
	var conn net.Conn
	if secure {
		// HTTP/1.1 only: the upgrade handshake does not exist in HTTP/2.
		td := tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}}
		conn, err = td.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("websocket: dial %s: %w", addr, err)
	}

	ws, err := handshake(ctx, conn, u)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ws, nil
}

func handshake(ctx context.Context, conn net.Conn, u *url.URL) (*wsConn, error) {
	// Closing the connection is what interrupts a handshake when ctx ends.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("websocket: handshake key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(nonce[:])

	req := &http.Request{
		Method:     http.MethodGet,
		URL:        &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery},
		Host:       u.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Upgrade":               {"websocket"},
			"Connection":            {"Upgrade"},
			"Sec-WebSocket-Key":     {key},
			"Sec-WebSocket-Version": {"13"},
			"User-Agent":            {userAgent},
		},
	}
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("websocket: write handshake: %w", ctxErr(ctx, err))
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, req)
	if err != nil {
		return nil, fmt.Errorf("websocket: read handshake: %w", ctxErr(ctx, err))
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("websocket: handshake refused (HTTP %d)", res.StatusCode)
	}
	if !strings.EqualFold(res.Header.Get("Upgrade"), "websocket") || !headerHasToken(res.Header, "Connection", "upgrade") {
		return nil, errors.New("websocket: handshake without upgrade")
	}
	if res.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		return nil, errors.New("websocket: handshake with wrong accept key")
	}
	return &wsConn{conn: conn, br: br}, nil
}

// headerHasToken reports whether the comma-separated header name lists token,
// case-insensitively (RFC 6455, section 4.1: "Connection" must include
// "Upgrade").
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// ctxErr prefers the reason ctx ended over the I/O error its end caused.
func ctxErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (w *wsConn) ReadText() (string, error) {
	var (
		message   []byte
		assembled bool // a fragmented message is being put together
		text      bool
	)
	for {
		fin, op, payload, err := w.readFrame(len(message))
		if err != nil {
			return "", err
		}
		switch op {
		case wsOpPing:
			if err := w.writeFrame(wsOpPong, payload); err != nil {
				return "", err
			}
		case wsOpPong:
		case wsOpClose:
			// Echo the close; the peer is gone whether or not it arrives.
			_ = w.writeFrame(wsOpClose, payload)
			return "", closeError(payload)
		case wsOpText, wsOpBinary:
			if assembled {
				_ = w.closeWithCode(wsCloseProtocolError)
				return "", errors.New("websocket: new message inside a fragmented one")
			}
			message, assembled, text = payload, true, op == wsOpText
		case wsOpContinuation:
			if !assembled {
				_ = w.closeWithCode(wsCloseProtocolError)
				return "", errors.New("websocket: continuation without a message")
			}
			message = append(message, payload...)
		default:
			_ = w.closeWithCode(wsCloseProtocolError)
			return "", fmt.Errorf("websocket: unknown opcode %#x", op)
		}
		if assembled && fin && op <= wsOpBinary {
			if text {
				return string(message), nil
			}
			// Binary messages are not part of the chat protocol.
			message, assembled = nil, false
		}
	}
}

// closeError turns the payload of a close frame into an error that keeps the
// code and the reason the peer gave.
func closeError(payload []byte) error {
	if len(payload) < 2 {
		return errSocketClosed
	}
	return fmt.Errorf("%w (%d %s)", errSocketClosed, binary.BigEndian.Uint16(payload), strings.TrimSpace(string(payload[2:])))
}

// readFrame reads one frame. have is the size of the message assembled so far.
func (w *wsConn) readFrame(have int) (fin bool, op byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(w.br, head[:]); err != nil {
		return false, 0, nil, fmt.Errorf("websocket: read: %w", err)
	}
	fin = head[0]&0x80 != 0
	if head[0]&0x70 != 0 {
		_ = w.closeWithCode(wsCloseProtocolError)
		return false, 0, nil, errors.New("websocket: reserved bits set without an extension")
	}
	op = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	if masked {
		// RFC 6455 §5.1: servidor não pode mascarar frames enviados ao cliente.
		// O cliente deve fechar a conexão com erro de protocolo (código 1002).
		_ = w.closeWithCode(wsCloseProtocolError)
		return false, 0, nil, errors.New("websocket: server frame was masked (protocol error 1002)")
	}
	size := uint64(head[1] & 0x7F)
	switch size {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(w.br, ext[:]); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read: %w", err)
		}
		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(w.br, ext[:]); err != nil {
			return false, 0, nil, fmt.Errorf("websocket: read: %w", err)
		}
		size = binary.BigEndian.Uint64(ext[:])
		if size>>63 != 0 {
			// RFC 6455 §5.2: o bit mais significativo do comprimento de 64 bits é zero.
			_ = w.closeWithCode(wsCloseProtocolError)
			return false, 0, nil, errors.New("websocket: invalid 64-bit payload length (protocol error 1002)")
		}
	}
	if op >= wsOpClose && (size > wsMaxControl || !fin) {
		_ = w.closeWithCode(wsCloseProtocolError)
		return false, 0, nil, errors.New("websocket: invalid control frame")
	}
	if size > wsMaxMessage || uint64(have)+size > wsMaxMessage {
		_ = w.closeWithCode(wsCloseMessageTooBig)
		return false, 0, nil, errors.New("websocket: message too large")
	}
	payload = make([]byte, size)
	if _, err = io.ReadFull(w.br, payload); err != nil {
		return false, 0, nil, fmt.Errorf("websocket: read: %w", err)
	}
	return fin, op, payload, nil
}

func (w *wsConn) WriteText(text string) error {
	frame, err := clientFrame(wsOpText, []byte(text))
	if err != nil {
		return err
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.writeLocked(frame, wsWriteTimeout)
}

// writeFrame sends one control or data frame.
func (w *wsConn) writeFrame(op byte, payload []byte) error {
	frame, err := clientFrame(op, payload)
	if err != nil {
		return err
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.writeLocked(frame, wsWriteTimeout)
}

func (w *wsConn) writeLocked(frame []byte, timeout time.Duration) error {
	_ = w.conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := w.conn.Write(frame); err != nil {
		return fmt.Errorf("websocket: write: %w", err)
	}
	return nil
}

// clientFrame builds one unfragmented frame, masked as a client must.
func clientFrame(op byte, payload []byte) ([]byte, error) {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return nil, fmt.Errorf("websocket: frame mask: %w", err)
	}
	frame := make([]byte, 0, len(payload)+14)
	frame = append(frame, 0x80|op)
	switch n := len(payload); {
	case n < 126:
		frame = append(frame, 0x80|byte(n))
	case n <= 0xFFFF:
		frame = append(frame, 0x80|126)
		frame = binary.BigEndian.AppendUint16(frame, uint16(n))
	default:
		frame = append(frame, 0x80|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	return frame, nil
}

// Close encerra a conexão normalmente (código 1000) e fecha o socket.
func (w *wsConn) Close() error {
	return w.closeWithCode(wsCloseNormalClosure)
}

// closeWithCode envia um frame de close com o código especificado e encerra o socket.
func (w *wsConn) closeWithCode(code uint16) error {
	w.closeOnce.Do(func() {
		var payload [2]byte
		binary.BigEndian.PutUint16(payload[:], code)
		if frame, err := clientFrame(wsOpClose, payload[:]); err == nil && w.wmu.TryLock() {
			_ = w.writeLocked(frame, wsCloseTimeout)
			w.wmu.Unlock()
		}
		w.closeErr = w.conn.Close()
	})
	return w.closeErr
}
