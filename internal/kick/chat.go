package kick

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

const (
	// DefaultPusherKey is the application key the Kick site presents to Pusher.
	// Kick does not publish it: it comes from open clients that read the same
	// chat, and Kick may rotate it. The config key kick_pusher_key replaces it.
	DefaultPusherKey = "34bf7a0ff419a2a775b9"
	// DefaultPusherHost is the Pusher cluster of that application.
	DefaultPusherHost = "ws-us2.pusher.com"

	defaultCoalesceInterval = 50 * time.Millisecond
	defaultInitialBackoff   = 500 * time.Millisecond
	defaultMaxBackoff       = 30 * time.Second
	defaultStableAfter      = 30 * time.Second
	defaultHandshakeTimeout = 10 * time.Second
	defaultPongTimeout      = 30 * time.Second
	// defaultActivityTimeout is used until the server says its own; the server
	// may only make it shorter.
	defaultActivityTimeout = 120 * time.Second
	defaultEventsBuffer    = 64
	readBuffer             = 64
)

// ErrReadOnly is what Send returns: this chat has no way to send.
var ErrReadOnly = errors.New("kick chat: read-only")

// RoomFinder finds the chat room of a channel slug. *Client implements it.
type RoomFinder interface {
	Room(ctx context.Context, slug string) (Room, error)
}

// ChatOptions configures a Chat. Zero values select production defaults.
type ChatOptions struct {
	// Rooms resolves the chat room of a channel; nil means a default Client.
	Rooms RoomFinder
	// PusherKey replaces DefaultPusherKey.
	PusherKey string
	// PusherHost replaces DefaultPusherHost.
	PusherHost string
	Dial       DialFunc

	CoalesceInterval time.Duration
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	// StableAfter is how long a confirmed connection must last before the
	// backoff starts over. A server that accepts the subscription and drops it
	// right away keeps the growing wait instead of a reconnect every
	// InitialBackoff.
	StableAfter      time.Duration
	HandshakeTimeout time.Duration
	// ActivityTimeout is how long the connection may stay silent before the
	// client pings; PongTimeout is how long the answer may take.
	ActivityTimeout time.Duration
	PongTimeout     time.Duration
	EventsBuffer    int
	Now             func() time.Time
}

var _ catalog.Chat = (*Chat)(nil)

// Chat is the read-only chat of Kick: an anonymous subscription to the Pusher
// channel of the room, the same one the site opens. It serves one channel at a
// time and numbers its sessions from 1, as catalog.Chat asks.
type Chat struct {
	opts   ChatOptions
	events chan catalog.ChatEvent

	sessions  sync.WaitGroup
	closeOnce sync.Once
	mu        sync.Mutex
	gen       uint64
	target    catalog.Channel
	joined    bool
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool
}

// NewChat creates a Chat with the given options.
func NewChat(opts ChatOptions) *Chat {
	if opts.Rooms == nil {
		opts.Rooms = NewClient(ClientConfig{})
	}
	opts.PusherKey = strings.TrimSpace(opts.PusherKey)
	if opts.PusherKey == "" {
		opts.PusherKey = DefaultPusherKey
	}
	opts.PusherHost = strings.TrimSpace(opts.PusherHost)
	if opts.PusherHost == "" {
		opts.PusherHost = DefaultPusherHost
	}
	if opts.Dial == nil {
		opts.Dial = dialWebSocket
	}
	if opts.CoalesceInterval <= 0 {
		opts.CoalesceInterval = defaultCoalesceInterval
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = defaultInitialBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultMaxBackoff
	}
	if opts.StableAfter <= 0 {
		opts.StableAfter = defaultStableAfter
	}
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = defaultHandshakeTimeout
	}
	if opts.ActivityTimeout <= 0 {
		opts.ActivityTimeout = defaultActivityTimeout
	}
	if opts.PongTimeout <= 0 {
		opts.PongTimeout = defaultPongTimeout
	}
	if opts.EventsBuffer <= 0 {
		opts.EventsBuffer = defaultEventsBuffer
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Chat{opts: opts, events: make(chan catalog.ChatEvent, opts.EventsBuffer)}
}

// Capabilities declares a chat that only reads.
func (c *Chat) Capabilities() catalog.ChatCapabilities {
	return catalog.ReadOnlyChatCapabilities
}

// HasToken is always false: the chat is read without an account.
func (c *Chat) HasToken() bool { return false }

// Username is always empty: the subscription is anonymous.
func (c *Chat) Username() string { return "" }

// Events returns the stream of events of the chat.
func (c *Chat) Events() <-chan catalog.ChatEvent { return c.events }

// Send refuses every message: sending needs an account, which this chat does
// not have.
func (c *Chat) Send(req catalog.ChatSendRequest) (catalog.ChatSendStatusUpdate, error) {
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	return catalog.ChatSendStatusUpdate{
		Generation: gen,
		Channel:    req.Channel,
		Nonce:      req.Nonce,
		Body:       req.Body,
		Status:     catalog.ChatDeliveryRejected,
		Reason:     catalog.ChatRejectOther,
	}, ErrReadOnly
}

// roomName is the name the events carry for ch: the one the app shows.
func roomName(ch catalog.Channel) string {
	if ch.Login != "" {
		return ch.Login
	}
	return ch.StableID()
}

// Join replaces the active session with one for ch. The room is looked up and
// the connection made in the background; what happens arrives as events.
func (c *Chat) Join(ctx context.Context, ch catalog.Channel) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("kick chat: join canceled: %w", err)
	}
	if roomName(ch) == "" {
		return 0, errors.New("kick chat: empty channel")
	}
	return c.start(ch)
}

// Reconnect restarts the session of the current channel under a new generation.
func (c *Chat) Reconnect() (uint64, error) {
	c.mu.Lock()
	ch, joined := c.target, c.joined
	c.mu.Unlock()
	if !joined {
		return 0, errors.New("kick chat: no active channel to reconnect")
	}
	return c.start(ch)
}

func (c *Chat) start(ch catalog.Channel) (uint64, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, errors.New("kick chat: closed")
	}
	prevCancel, prevDone := c.cancel, c.done
	c.gen++
	gen := c.gen
	c.target, c.joined = ch, true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.cancel, c.done = cancel, done
	c.sessions.Add(1)
	c.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	if prevDone != nil {
		<-prevDone
	}
	go c.runSession(ctx, done, gen, roomName(ch), Slug(ch))
	return gen, nil
}

// Stop ends the active session and waits for it.
func (c *Chat) Stop() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done = nil, nil
	c.target, c.joined = catalog.Channel{}, false
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Close stops the active session and closes the Events channel.
func (c *Chat) Close() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		cancel, done := c.cancel, c.done
		c.cancel, c.done = nil, nil
		c.target, c.joined = catalog.Channel{}, false
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}
		c.sessions.Wait()
		close(c.events)
	})
}

// emit delivers ev unless its session was replaced or ended.
func (c *Chat) emit(ctx context.Context, ev catalog.ChatEvent) {
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	stale := c.closed || c.gen != ev.Generation
	c.mu.Unlock()
	if stale {
		return
	}
	ev.Anonymous = true
	select {
	case c.events <- ev:
	case <-ctx.Done():
	}
}

// finalError marks a failure that trying again would only repeat.
type finalError struct{ err error }

func (e *finalError) Error() string { return e.err.Error() }
func (e *finalError) Unwrap() error { return e.err }

func final(reason catalog.ChatErrorReason, err error) error {
	if reason != catalog.ChatErrorNone {
		err = &catalog.ChatError{Reason: reason, Err: err}
	}
	return &finalError{err: err}
}

func (c *Chat) runSession(ctx context.Context, done chan struct{}, gen uint64, name, slug string) {
	defer c.sessions.Done()
	defer close(done)

	c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateConnecting})
	if slug == "" {
		// A recording or a clip: the room of the channel is not its chat.
		err := &catalog.ChatError{Reason: catalog.ChatErrorNoRoom, Err: errors.New("kick chat: not a live channel")}
		c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateError, Err: err})
		return
	}

	var room *Room
	backoff := c.opts.InitialBackoff
	attempt := 0
	for {
		started := time.Now()
		connected, err := c.attempt(ctx, gen, name, slug, &room)
		if ctx.Err() != nil {
			return
		}
		var fatal *finalError
		if errors.As(err, &fatal) {
			c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateError, Err: fatal.err})
			return
		}
		if connected && time.Since(started) >= c.opts.StableAfter {
			backoff, attempt = c.opts.InitialBackoff, 0
		}
		attempt++
		wait := min(backoff, c.opts.MaxBackoff)
		c.emit(ctx, catalog.ChatEvent{
			Generation:  gen,
			Channel:     name,
			State:       catalog.ChatStateReconnecting,
			Err:         err,
			Attempt:     attempt,
			NextBackoff: wait,
		})
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, c.opts.MaxBackoff)
	}
}

// attempt finds the room when it is not known yet and serves one connection.
// connected reports whether the subscription was confirmed.
func (c *Chat) attempt(ctx context.Context, gen uint64, name, slug string, room **Room) (connected bool, err error) {
	if *room == nil {
		found, err := c.opts.Rooms.Room(ctx, slug)
		if err != nil {
			return false, lookupError(err)
		}
		*room = &found
	}
	return c.serve(ctx, gen, name, (*room).ChatroomID)
}

// lookupError decides whether a failed room lookup is worth repeating. The
// network, a server error and a rate limit pass; a channel that does not exist,
// a blocked request and an answer that cannot be read do not.
func lookupError(err error) error {
	switch {
	case errors.Is(err, ErrChannelNotFound), errors.Is(err, ErrInvalidSlug):
		return final(catalog.ChatErrorNoRoom, err)
	case catalog.KindOf(err) == catalog.Bloqueio:
		return final(catalog.ChatErrorBlocked, err)
	case catalog.KindOf(err) == catalog.Contrato:
		return final(catalog.ChatErrorNone, err)
	default:
		return err
	}
}

func (c *Chat) socketURL() string {
	u := url.URL{
		Scheme:   "wss",
		Host:     c.opts.PusherHost,
		Path:     "/app/" + c.opts.PusherKey,
		RawQuery: "protocol=7&client=ttv-tui&version=1.0&flash=false",
	}
	return u.String()
}

type readResult struct {
	text string
	err  error
}

// serve runs one connection: handshake, subscription and the read loop. It
// returns when the connection ends, with the reason.
func (c *Chat) serve(ctx context.Context, gen uint64, name string, roomID int64) (connected bool, err error) {
	dialCtx, cancelDial := context.WithTimeout(ctx, c.opts.HandshakeTimeout)
	sock, err := c.opts.Dial(dialCtx, c.socketURL())
	cancelDial()
	if err != nil {
		return false, fmt.Errorf("kick chat: connect: %w", err)
	}
	defer func() { _ = sock.Close() }()

	// The reader ends when the socket closes; quit lets it drop a result nobody
	// will take.
	frames := make(chan readResult, readBuffer)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for {
			text, rerr := sock.ReadText()
			select {
			case frames <- readResult{text: text, err: rerr}:
			case <-quit:
				return
			}
			if rerr != nil {
				return
			}
		}
	}()

	channel := fmt.Sprintf("chatrooms.%d.v2", roomID)
	activity := c.opts.ActivityTimeout
	idle := time.NewTimer(c.opts.HandshakeTimeout)
	defer idle.Stop()
	flush := time.NewTimer(time.Hour)
	flush.Stop()
	defer flush.Stop()

	var (
		batch        []catalog.ChatMessage
		ready        bool
		awaitingPong bool
	)
	emitBatch := func() {
		if len(batch) == 0 {
			return
		}
		c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, Messages: batch})
		batch = nil
	}

	for {
		select {
		case <-ctx.Done():
			return connected, ctx.Err()
		case <-flush.C:
			emitBatch()
		case <-idle.C:
			switch {
			case !ready:
				return connected, errors.New("kick chat: handshake timed out")
			case awaitingPong:
				return connected, errors.New("kick chat: connection went silent")
			}
			if err := sock.WriteText(controlFrame(eventPing)); err != nil {
				return connected, fmt.Errorf("kick chat: ping: %w", err)
			}
			awaitingPong = true
			idle.Reset(c.opts.PongTimeout)
		case r := <-frames:
			if r.err != nil {
				emitBatch()
				return connected, fmt.Errorf("kick chat: %w", r.err)
			}
			f, perr := parseFrame(r.text)
			if perr != nil {
				// Not a frame of the protocol: nothing to act on.
				continue
			}
			// Any message proves the connection is alive.
			awaitingPong = false
			if ready {
				idle.Reset(activity)
			}
			switch f.Event {
			case eventEstablished:
				var est established
				if err := f.payload(&est); err != nil {
					return connected, err
				}
				if d := time.Duration(est.ActivityTimeout) * time.Second; d > 0 && d < activity {
					activity = d
				}
				ready = true
				idle.Reset(activity)
				if err := sock.WriteText(subscribeFrame(channel)); err != nil {
					return connected, fmt.Errorf("kick chat: subscribe: %w", err)
				}
			case eventSubscribed:
				if f.Channel != channel {
					continue
				}
				connected = true
				c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateConnected})
			case eventSubscribeErr:
				return connected, final(catalog.ChatErrorNone, fmt.Errorf("kick chat: subscription to %s refused", channel))
			case eventError:
				emitBatch()
				return connected, serverError(f)
			case eventPing:
				if err := sock.WriteText(controlFrame(eventPong)); err != nil {
					return connected, fmt.Errorf("kick chat: pong: %w", err)
				}
			case eventChatMessage:
				if f.Channel != channel {
					continue
				}
				msg, ok, merr := parseChatMessage(f, c.opts.Now())
				if merr != nil || !ok {
					continue
				}
				msg.Generation = gen
				if len(batch) == 0 {
					flush.Reset(c.opts.CoalesceInterval)
				}
				batch = append(batch, msg)
			}
		}
	}
}

// serverError classifies a pusher:error. A key no application answers to is
// final and has its own reason; the other permanent codes are final too.
func serverError(f frame) error {
	var pe pusherError
	if err := f.payload(&pe); err != nil {
		return err
	}
	switch {
	case pe.Code == pusherAppMissing:
		return final(catalog.ChatErrorKeyRefused, pe)
	case pe.permanent():
		return final(catalog.ChatErrorNone, pe)
	default:
		return pe
	}
}
