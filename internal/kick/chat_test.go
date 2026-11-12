package kick

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

const testTimeout = 5 * time.Second

// fakeSocket is a Socket driven by the test: in carries what the server says,
// out what the client wrote.
type fakeSocket struct {
	in     chan string
	out    chan string
	closed chan struct{}
	once   sync.Once
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{in: make(chan string, 64), out: make(chan string, 64), closed: make(chan struct{})}
}

func (s *fakeSocket) ReadText() (string, error) {
	select {
	case text, ok := <-s.in:
		if !ok {
			return "", io.EOF
		}
		return text, nil
	case <-s.closed:
		return "", net.ErrClosed
	}
}

func (s *fakeSocket) WriteText(text string) error {
	select {
	case s.out <- text:
		return nil
	case <-s.closed:
		return net.ErrClosed
	}
}

func (s *fakeSocket) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// wrote waits for the next frame the client wrote and returns its event.
func (s *fakeSocket) wrote(t *testing.T) frame {
	t.Helper()
	select {
	case text := <-s.out:
		f, err := parseFrame(text)
		if err != nil {
			t.Fatalf("client wrote %q: %v", text, err)
		}
		return f
	case <-time.After(testTimeout):
		t.Fatal("client wrote nothing")
		return frame{}
	}
}

// fakeDialer hands out the sockets the test queued, one per dial.
type fakeDialer struct {
	mu    sync.Mutex
	urls  []string
	queue chan *fakeSocket
	err   error
}

func newFakeDialer() *fakeDialer { return &fakeDialer{queue: make(chan *fakeSocket, 8)} }

func (d *fakeDialer) dial(ctx context.Context, rawURL string) (Socket, error) {
	d.mu.Lock()
	d.urls = append(d.urls, rawURL)
	err := d.err
	d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case s := <-d.queue:
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *fakeDialer) next() *fakeSocket {
	s := newFakeSocket()
	d.queue <- s
	return s
}

type fakeRooms struct {
	mu    sync.Mutex
	calls int
	errs  []error // returned in order before room
	room  Room
}

func (r *fakeRooms) Room(_ context.Context, slug string) (Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return Room{}, err
	}
	room := r.room
	room.Slug = slug
	return room, nil
}

func (r *fakeRooms) asked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func newTestChat(t *testing.T, rooms RoomFinder, d *fakeDialer, tune func(*ChatOptions)) *Chat {
	t.Helper()
	opts := ChatOptions{
		Rooms:            rooms,
		Dial:             d.dial,
		CoalesceInterval: time.Millisecond,
		InitialBackoff:   time.Millisecond,
		MaxBackoff:       4 * time.Millisecond,
	}
	if tune != nil {
		tune(&opts)
	}
	c := NewChat(opts)
	t.Cleanup(c.Close)
	return c
}

func nextEvent(t *testing.T, c *Chat) catalog.ChatEvent {
	t.Helper()
	select {
	case ev, ok := <-c.Events():
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(testTimeout):
		t.Fatal("no chat event")
		return catalog.ChatEvent{}
	}
}

// waitState drains events until one carries state.
func waitState(t *testing.T, c *Chat, state catalog.ChatState) catalog.ChatEvent {
	t.Helper()
	for {
		if ev := nextEvent(t, c); ev.State == state {
			return ev
		}
	}
}

var gaules = catalog.Channel{Platform: platform.Kick, Login: "gaules", URL: "https://kick.com/gaules"}

// subscribe plays the server side of the handshake on s, with the real frames.
func subscribe(t *testing.T, s *fakeSocket, lines []string) {
	t.Helper()
	s.in <- lines[0]
	if f := s.wrote(t); f.Event != eventSubscribe {
		t.Fatalf("client wrote %q after the handshake, want a subscription", f.Event)
	}
	s.in <- lines[1]
}

func TestChat_FixtureSession(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	sock := d.next()
	rooms := &fakeRooms{room: Room{ChatroomID: 66973867}}
	c := newTestChat(t, rooms, d, nil)

	if caps := c.Capabilities(); caps.CanSend || caps != catalog.ReadOnlyChatCapabilities {
		t.Fatalf("capabilities = %+v, want read-only", caps)
	}
	gen, err := c.Join(context.Background(), gaules)
	if err != nil || gen != 1 {
		t.Fatalf("Join = (%d, %v), want generation 1", gen, err)
	}
	if ev := nextEvent(t, c); ev.State != catalog.ChatStateConnecting || ev.Generation != 1 || ev.Channel != "gaules" || !ev.Anonymous {
		t.Fatalf("first event = %+v, want connecting to gaules", ev)
	}

	sock.in <- lines[0]
	sub := sock.wrote(t)
	var req struct {
		Auth    string `json:"auth"`
		Channel string `json:"channel"`
	}
	if err := sub.payload(&req); err != nil || sub.Event != eventSubscribe || req.Channel != "chatrooms.66973867.v2" || req.Auth != "" {
		t.Fatalf("subscription = %+v (%+v), err %v", sub, req, err)
	}
	sock.in <- lines[1]
	if ev := waitState(t, c, catalog.ChatStateConnected); ev.Generation != 1 || ev.Channel != "gaules" {
		t.Fatalf("connected event = %+v", ev)
	}

	for _, line := range lines[2:] {
		sock.in <- line
	}
	var bodies []string
	for len(bodies) < len(lines)-2 {
		ev := nextEvent(t, c)
		for _, m := range ev.Messages {
			if m.Generation != 1 {
				t.Errorf("message generation = %d, want 1", m.Generation)
			}
			bodies = append(bodies, m.Body)
		}
	}
	if bodies[0] != "@Viewer02 hmmm" || bodies[1] != "collectiblesGoldenW" {
		t.Errorf("bodies = %q", bodies)
	}

	d.mu.Lock()
	url := d.urls[0]
	d.mu.Unlock()
	if !strings.HasPrefix(url, "wss://"+DefaultPusherHost+"/app/"+DefaultPusherKey+"?") || !strings.Contains(url, "protocol=7") {
		t.Errorf("dialed %q, want the default key and host", url)
	}
}

func TestChat_ConfiguredKeyAndHost(t *testing.T) {
	d := newFakeDialer()
	d.next()
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 1}}, d, func(o *ChatOptions) {
		o.PusherKey, o.PusherHost = " minhachave ", "ws-eu.pusher.com"
	})
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	deadline := time.Now().Add(testTimeout)
	for {
		d.mu.Lock()
		urls := append([]string(nil), d.urls...)
		d.mu.Unlock()
		if len(urls) > 0 {
			if !strings.HasPrefix(urls[0], "wss://ws-eu.pusher.com/app/minhachave?") {
				t.Fatalf("dialed %q, want the configured key and host", urls[0])
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("never dialed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChat_ReconnectsAfterDrop(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	first := d.next()
	rooms := &fakeRooms{room: Room{ChatroomID: 66973867}}
	c := newTestChat(t, rooms, d, nil)
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	subscribe(t, first, lines)
	waitState(t, c, catalog.ChatStateConnected)

	// The server goes away: same generation, a new connection, no new lookup.
	close(first.in)
	ev := waitState(t, c, catalog.ChatStateReconnecting)
	if ev.Generation != 1 || ev.Err == nil || ev.Attempt != 1 {
		t.Fatalf("reconnecting event = %+v", ev)
	}
	second := d.next()
	subscribe(t, second, lines)
	if ev := waitState(t, c, catalog.ChatStateConnected); ev.Generation != 1 {
		t.Fatalf("connected again = %+v, want the same generation", ev)
	}
	second.in <- lines[2]
	for {
		if ev := nextEvent(t, c); len(ev.Messages) > 0 {
			break
		}
	}
	if n := rooms.asked(); n != 1 {
		t.Errorf("room looked up %d times, want 1", n)
	}
}

func TestChat_RetriesLookupOnNetworkError(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	sock := d.next()
	rede := &catalog.Error{Platform: platform.Kick, Kind: catalog.Rede, Err: errors.New("dial tcp: no route")}
	rooms := &fakeRooms{errs: []error{rede, rede}, room: Room{ChatroomID: 66973867}}
	c := newTestChat(t, rooms, d, nil)
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if ev := waitState(t, c, catalog.ChatStateReconnecting); !errors.Is(ev.Err, rede) {
		t.Fatalf("reconnecting err = %v, want the lookup error", ev.Err)
	}
	subscribe(t, sock, lines)
	waitState(t, c, catalog.ChatStateConnected)
	if n := rooms.asked(); n != 3 {
		t.Errorf("room looked up %d times, want 3", n)
	}
}

func TestChat_FinalErrors(t *testing.T) {
	blocked := statusError("channel", http.StatusForbidden)
	missing := statusError("channel", http.StatusNotFound)
	garbled := contractError("channel", errors.New("decode response"))
	tests := []struct {
		name   string
		ch     catalog.Channel
		rooms  *fakeRooms
		server func(s *fakeSocket)
		reason catalog.ChatErrorReason
	}{
		{name: "cloudflare block", ch: gaules, rooms: &fakeRooms{errs: []error{blocked}}, reason: catalog.ChatErrorBlocked},
		{name: "unknown channel", ch: gaules, rooms: &fakeRooms{errs: []error{missing}}, reason: catalog.ChatErrorNoRoom},
		{name: "unreadable channel", ch: gaules, rooms: &fakeRooms{errs: []error{garbled}}, reason: catalog.ChatErrorNone},
		{
			name:   "recording has no room",
			ch:     catalog.Channel{Platform: platform.Kick, Login: "xqc/videos/0a1b", URL: "https://kick.com/xqc/videos/0a1b"},
			rooms:  &fakeRooms{room: Room{ChatroomID: 668}},
			reason: catalog.ChatErrorNoRoom,
		},
		{
			name:  "key refused",
			ch:    gaules,
			rooms: &fakeRooms{room: Room{ChatroomID: 66973867}},
			server: func(s *fakeSocket) {
				s.in <- `{"event":"pusher:error","data":{"code":4001,"message":"App key x not in this cluster."}}`
			},
			reason: catalog.ChatErrorKeyRefused,
		},
		{
			name:  "other permanent pusher error",
			ch:    gaules,
			rooms: &fakeRooms{room: Room{ChatroomID: 66973867}},
			server: func(s *fakeSocket) {
				s.in <- `{"event":"pusher:error","data":{"code":4004,"message":"over quota"}}`
			},
			reason: catalog.ChatErrorNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDialer()
			sock := d.next()
			c := newTestChat(t, tc.rooms, d, nil)
			if _, err := c.Join(context.Background(), tc.ch); err != nil {
				t.Fatalf("Join: %v", err)
			}
			if tc.server != nil {
				tc.server(sock)
			}
			ev := waitState(t, c, catalog.ChatStateError)
			if ev.Err == nil {
				t.Fatal("error event without an error")
			}
			if got := catalog.ChatErrorReasonOf(ev.Err); got != tc.reason {
				t.Errorf("reason = %q, want %q (err %v)", got, tc.reason, ev.Err)
			}
			// Final: nothing follows, and the lookup is not repeated.
			select {
			case ev := <-c.Events():
				t.Errorf("event after a final error: %+v", ev)
			case <-time.After(20 * time.Millisecond):
			}
			if n := tc.rooms.asked(); n > 1 {
				t.Errorf("room looked up %d times after a final error", n)
			}
		})
	}
}

func TestChat_TransientPusherErrorReconnects(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	first := d.next()
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 66973867}}, d, nil)
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	first.in <- `{"event":"pusher:error","data":{"code":4200,"message":"reconnect"}}`
	waitState(t, c, catalog.ChatStateReconnecting)
	subscribe(t, d.next(), lines)
	waitState(t, c, catalog.ChatStateConnected)
}

func TestChat_PingsASilentConnection(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	sock := d.next()
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 66973867}}, d, func(o *ChatOptions) {
		// The answer window is wide so a loaded machine still answers in time.
		o.ActivityTimeout = 20 * time.Millisecond
		o.PongTimeout = time.Second
	})
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	subscribe(t, sock, lines)
	waitState(t, c, catalog.ChatStateConnected)

	// Silence: the client pings; an answer keeps the connection.
	if f := sock.wrote(t); f.Event != eventPing {
		t.Fatalf("client wrote %q on silence, want a ping", f.Event)
	}
	sock.in <- `{"event":"pusher:pong","data":"{}"}`
	// The server pings too and gets a pong.
	sock.in <- `{"event":"pusher:ping","data":"{}"}`
	sawPong := false
	for !sawPong {
		sawPong = sock.wrote(t).Event == eventPong
	}
	// No answer to the next ping: the connection is dropped and retried.
	ev := waitState(t, c, catalog.ChatStateReconnecting)
	if ev.Err == nil || !strings.Contains(ev.Err.Error(), "silent") {
		t.Errorf("reconnecting err = %v, want the silent connection", ev.Err)
	}
}

func TestChat_HandshakeTimeout(t *testing.T) {
	d := newFakeDialer()
	d.next() // connects and never speaks
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 1}}, d, func(o *ChatOptions) {
		o.HandshakeTimeout = 10 * time.Millisecond
	})
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ev := waitState(t, c, catalog.ChatStateReconnecting)
	if ev.Err == nil || !strings.Contains(ev.Err.Error(), "handshake") {
		t.Errorf("reconnecting err = %v, want the handshake timeout", ev.Err)
	}
}

func TestChat_DialFailureBacksOff(t *testing.T) {
	d := newFakeDialer()
	d.err = errors.New("dial tcp: connection refused")
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 1}}, d, nil)
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	var waits []time.Duration
	for len(waits) < 4 {
		ev := waitState(t, c, catalog.ChatStateReconnecting)
		if ev.Attempt != len(waits)+1 {
			t.Fatalf("attempt = %d, want %d", ev.Attempt, len(waits)+1)
		}
		waits = append(waits, ev.NextBackoff)
	}
	want := []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 4 * time.Millisecond}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("backoffs = %v, want %v", waits, want)
		}
	}
}

func TestChat_JoinReplacesTheSession(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	first := d.next()
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 66973867}}, d, nil)
	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	subscribe(t, first, lines)
	waitState(t, c, catalog.ChatStateConnected)

	second := d.next()
	other := catalog.Channel{Platform: platform.Kick, Login: "xqc"}
	gen, err := c.Join(context.Background(), other)
	if err != nil || gen != 2 {
		t.Fatalf("second Join = (%d, %v), want generation 2", gen, err)
	}
	select {
	case <-first.closed:
	case <-time.After(testTimeout):
		t.Fatal("the first connection was left open")
	}
	subscribe(t, second, lines)
	for {
		ev := nextEvent(t, c)
		if ev.Generation != 2 {
			t.Fatalf("event of a replaced session got through: %+v", ev)
		}
		if ev.State == catalog.ChatStateConnected {
			if ev.Channel != "xqc" {
				t.Fatalf("connected to %q, want xqc", ev.Channel)
			}
			break
		}
	}

	// Reconnect: same channel, next generation.
	d.next()
	if gen, err := c.Reconnect(); err != nil || gen != 3 {
		t.Fatalf("Reconnect = (%d, %v), want generation 3", gen, err)
	}
	if ev := waitState(t, c, catalog.ChatStateConnecting); ev.Channel != "xqc" || ev.Generation != 3 {
		t.Fatalf("after Reconnect: %+v", ev)
	}
}

func TestChat_StopAndClose(t *testing.T) {
	lines := chatFixture(t)
	d := newFakeDialer()
	sock := d.next()
	c := newTestChat(t, &fakeRooms{room: Room{ChatroomID: 66973867}}, d, nil)
	if _, err := c.Reconnect(); err == nil {
		t.Error("Reconnect with no channel: want an error")
	}
	if _, err := c.Join(context.Background(), catalog.Channel{Platform: platform.Kick}); err == nil {
		t.Error("Join with no channel: want an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Join(ctx, gaules); err == nil {
		t.Error("Join with a canceled context: want an error")
	}

	if _, err := c.Join(context.Background(), gaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	subscribe(t, sock, lines)
	waitState(t, c, catalog.ChatStateConnected)
	c.Stop()
	select {
	case <-sock.closed:
	default:
		t.Fatal("Stop left the connection open")
	}
	if _, err := c.Reconnect(); err == nil {
		t.Error("Reconnect after Stop: want an error")
	}

	upd, err := c.Send(catalog.ChatSendRequest{Channel: "gaules", Nonce: "n", Body: "oi"})
	if !errors.Is(err, ErrReadOnly) || upd.Status != catalog.ChatDeliveryRejected || upd.Nonce != "n" {
		t.Errorf("Send = (%+v, %v), want a read-only refusal", upd, err)
	}
	if c.HasToken() || c.Username() != "" {
		t.Error("an anonymous chat reports a login")
	}

	c.Close()
	c.Close()
	if _, ok := <-c.Events(); ok {
		// Events left in the buffer are fine; the channel must end.
		for range c.Events() {
		}
	}
	if _, err := c.Join(context.Background(), gaules); err == nil {
		t.Error("Join after Close: want an error")
	}
}
