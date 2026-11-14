package youtube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

const (
	defaultMinPollInterval = 1 * time.Second
	defaultPollInterval    = 5 * time.Second
	defaultMaxPollInterval = 60 * time.Second
	defaultInitialBackoff  = 1 * time.Second
	defaultMaxBackoff      = 30 * time.Second
	defaultEventsBuffer    = 64
	seenRingCapacity       = 1024
)

// ErrReadOnly is returned by Send because YouTube chat is read-only.
var ErrReadOnly = errors.New("youtube chat: read-only")

// RoomClient fetches watch-page metadata and polls innertube live chat.
// *Client implements it.
type RoomClient interface {
	Resolve(ctx context.Context, rawTarget string) (Resolved, error)
	PollChat(ctx context.Context, req PollRequest, now time.Time) (PollResult, error)
}

// ChatOptions configures a YouTube read-only Chat. Zero values select defaults.
type ChatOptions struct {
	Client          RoomClient
	MinPollInterval time.Duration
	PollInterval    time.Duration
	MaxPollInterval time.Duration
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	EventsBuffer    int
	Now             func() time.Time
}

var _ catalog.Chat = (*Chat)(nil)

// Chat is the read-only live chat of YouTube using unauthenticated innertube
// polling. It serves one channel at a time and numbers sessions from 1.
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

// NewChat creates a YouTube read-only Chat configured by opts.
func NewChat(opts ChatOptions) *Chat {
	if opts.Client == nil {
		opts.Client = NewClient(ClientConfig{})
	}
	if opts.MinPollInterval <= 0 {
		opts.MinPollInterval = defaultMinPollInterval
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = defaultPollInterval
	}
	if opts.MaxPollInterval <= 0 {
		opts.MaxPollInterval = defaultMaxPollInterval
	}
	if opts.MaxPollInterval < opts.MinPollInterval {
		opts.MaxPollInterval = opts.MinPollInterval
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = defaultInitialBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultMaxBackoff
	}
	if opts.MaxBackoff < opts.InitialBackoff {
		opts.MaxBackoff = opts.InitialBackoff
	}
	if opts.EventsBuffer <= 0 {
		opts.EventsBuffer = defaultEventsBuffer
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Chat{opts: opts, events: make(chan catalog.ChatEvent, opts.EventsBuffer)}
}

// Capabilities declares a read-only chat.
func (c *Chat) Capabilities() catalog.ChatCapabilities {
	return catalog.ReadOnlyChatCapabilities
}

// HasToken is always false: YouTube chat requires no account.
func (c *Chat) HasToken() bool { return false }

// Username is always empty: the session is anonymous/read-only.
func (c *Chat) Username() string { return "" }

// Events returns the read-only stream of chat events.
func (c *Chat) Events() <-chan catalog.ChatEvent { return c.events }

// Send rejects every outbound message because YouTube chat is read-only.
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

func roomTarget(ch catalog.Channel) (display, resolveArg string) {
	display = ch.Login
	if display == "" {
		display = ch.StableID()
	}
	if u := strings.TrimSpace(ch.URL); u != "" {
		return display, u
	}
	return display, display
}

// Join replaces any active session with one for ch.
func (c *Chat) Join(ctx context.Context, ch catalog.Channel) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("youtube chat: join canceled: %w", err)
	}
	name, target := roomTarget(ch)
	if name == "" || target == "" {
		return 0, errors.New("youtube chat: empty channel")
	}
	return c.start(ch)
}

// Reconnect restarts the session for the current channel under a new generation.
func (c *Chat) Reconnect() (uint64, error) {
	c.mu.Lock()
	ch, joined := c.target, c.joined
	c.mu.Unlock()
	if !joined {
		return 0, errors.New("youtube chat: no active channel to reconnect")
	}
	return c.start(ch)
}

func (c *Chat) start(ch catalog.Channel) (uint64, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, errors.New("youtube chat: closed")
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
	name, target := roomTarget(ch)
	go c.runSession(ctx, done, gen, name, target)
	return gen, nil
}

// Stop disconnects the active session and waits for its goroutine to exit.
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

// Close stops the active session and closes Events.
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

type finalError struct{ err error }

func (e *finalError) Error() string { return e.err.Error() }
func (e *finalError) Unwrap() error { return e.err }

func final(reason catalog.ChatErrorReason, err error) error {
	if reason != catalog.ChatErrorNone {
		err = &catalog.ChatError{Reason: reason, Err: err}
	}
	return &finalError{err: err}
}

func (c *Chat) runSession(ctx context.Context, done chan struct{}, gen uint64, name, target string) {
	defer c.sessions.Done()
	defer close(done)

	c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateConnecting})

	backoff := c.opts.InitialBackoff
	attempt := 0
	var resolved *Resolved

	for {
		if ctx.Err() != nil {
			return
		}
		if resolved == nil {
			res, err := c.opts.Client.Resolve(ctx, target)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				classified := classifyResolveError(err)
				var fatal *finalError
				if errors.As(classified, &fatal) {
					c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateError, Err: fatal.err})
					return
				}
				attempt++
				wait := errorBackoffWait(classified, backoff, c.opts.MaxBackoff)
				c.emit(ctx, catalog.ChatEvent{
					Generation:  gen,
					Channel:     name,
					State:       catalog.ChatStateReconnecting,
					Err:         classified,
					Attempt:     attempt,
					NextBackoff: wait,
				})
				if !waitCtx(ctx, wait) {
					return
				}
				backoff = min(backoff*2, c.opts.MaxBackoff)
				continue
			}
			if strings.TrimSpace(res.ChatContinuation) == "" {
				c.emit(ctx, catalog.ChatEvent{
					Generation: gen,
					Channel:    name,
					State:      catalog.ChatStateError,
					Err:        &catalog.ChatError{Reason: catalog.ChatErrorNoRoom, Err: ErrChatDisabled},
				})
				return
			}
			resolved = &res
			backoff, attempt = c.opts.InitialBackoff, 0
		}

		if err := c.pollLoop(ctx, gen, name, resolved); ctx.Err() != nil {
			return
		} else {
			var fatal *finalError
			if errors.As(err, &fatal) {
				c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateError, Err: fatal.err})
				return
			}
			attempt++
			wait := errorBackoffWait(err, backoff, c.opts.MaxBackoff)
			c.emit(ctx, catalog.ChatEvent{
				Generation:  gen,
				Channel:     name,
				State:       catalog.ChatStateReconnecting,
				Err:         err,
				Attempt:     attempt,
				NextBackoff: wait,
			})
			// Re-resolve watch page if continuation fails or disappears so a fresh
			// continuation can be picked up if the stream restarted.
			resolved = nil
			if !waitCtx(ctx, wait) {
				return
			}
			backoff = min(backoff*2, c.opts.MaxBackoff)
		}
	}
}

func (c *Chat) pollLoop(ctx context.Context, gen uint64, name string, resolved *Resolved) error {
	cont := resolved.ChatContinuation
	seen := newSeenIDs(seenRingCapacity)
	connected := false
	backoff := c.opts.InitialBackoff
	attempt := 0
	consecErrors := 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		res, err := c.opts.Client.PollChat(ctx, PollRequest{
			APIKey:        resolved.APIKey,
			ClientVersion: resolved.ClientVersion,
			Continuation:  cont,
		}, c.opts.Now())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			classified := classifyPollError(err)
			var fatal *finalError
			if errors.As(classified, &fatal) {
				return classified
			}
			consecErrors++
			attempt++
			wait := errorBackoffWait(classified, backoff, c.opts.MaxBackoff)
			c.emit(ctx, catalog.ChatEvent{
				Generation:  gen,
				Channel:     name,
				State:       catalog.ChatStateReconnecting,
				Err:         classified,
				Attempt:     attempt,
				NextBackoff: wait,
			})
			if consecErrors >= 3 {
				// After repeated poll errors (e.g. stale continuation), fall back to
				// re-resolving the watch page.
				return classified
			}
			if !waitCtx(ctx, wait) {
				return ctx.Err()
			}
			backoff = min(backoff*2, c.opts.MaxBackoff)
			continue
		}

		consecErrors = 0
		backoff = c.opts.InitialBackoff
		attempt = 0

		if !connected {
			connected = true
			c.emit(ctx, catalog.ChatEvent{Generation: gen, Channel: name, State: catalog.ChatStateConnected})
		}

		fresh := make([]catalog.ChatMessage, 0, len(res.Messages))
		for _, m := range res.Messages {
			if m.ID != "" {
				if seen.has(m.ID) {
					continue
				}
				seen.add(m.ID)
			}
			m.Generation = gen
			fresh = append(fresh, m)
		}
		if len(fresh) > 0 {
			c.emit(ctx, catalog.ChatEvent{
				Generation: gen,
				Channel:    name,
				Messages:   fresh,
			})
		}
		for _, delID := range res.DeletedIDs {
			c.emit(ctx, catalog.ChatEvent{
				Generation: gen,
				Channel:    name,
				ClearMsgID: delID,
			})
		}

		if strings.TrimSpace(res.NextContinuation) == "" {
			return final(catalog.ChatErrorNoRoom, ErrChatDisabled)
		}
		cont = res.NextContinuation

		wait := c.nextPollWait(res.Timeout)
		if !waitCtx(ctx, wait) {
			return ctx.Err()
		}
	}
}

// nextPollWait enforces the server-requested interval without ever polling faster
// than the server asked (up to MaxPollInterval). When the server gives no
// timeout, PollInterval is used instead (clamped to [MinPollInterval, MaxPollInterval]).
func (c *Chat) nextPollWait(serverTimeout time.Duration) time.Duration {
	if serverTimeout <= 0 {
		wait := c.opts.PollInterval
		if wait < c.opts.MinPollInterval {
			return c.opts.MinPollInterval
		}
		if wait > c.opts.MaxPollInterval {
			return c.opts.MaxPollInterval
		}
		return wait
	}
	if serverTimeout > c.opts.MaxPollInterval {
		return c.opts.MaxPollInterval
	}
	return serverTimeout
}

func errorBackoffWait(err error, backoff, maxBackoff time.Duration) time.Duration {
	wait := min(backoff, maxBackoff)
	var ce *catalog.Error
	if errors.As(err, &ce) && ce.ResetAfter > wait {
		wait = min(ce.ResetAfter, maxBackoff)
	}
	return wait
}

func waitCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func classifyResolveError(err error) error {
	switch {
	case errors.Is(err, ErrBotCheck):
		return final(catalog.ChatErrorBotCheck, err)
	case errors.Is(err, ErrNotLive):
		return final(catalog.ChatErrorVOD, err)
	case errors.Is(err, ErrOffline), errors.Is(err, ErrChannelNotFound), errors.Is(err, ErrInvalidTarget), errors.Is(err, ErrChatDisabled):
		return final(catalog.ChatErrorNoRoom, err)
	case catalog.KindOf(err) == catalog.Bloqueio:
		return final(catalog.ChatErrorBlocked, err)
	default:
		return err
	}
}

func classifyPollError(err error) error {
	switch {
	case errors.Is(err, ErrChatDisabled):
		return final(catalog.ChatErrorNoRoom, err)
	case catalog.KindOf(err) == catalog.Bloqueio:
		return final(catalog.ChatErrorBlocked, err)
	default:
		return err
	}
}

type seenIDs struct {
	cap  int
	ring []string
	next int
	set  map[string]struct{}
}

func newSeenIDs(capacity int) *seenIDs {
	return &seenIDs{
		cap: capacity,
		set: make(map[string]struct{}, capacity),
	}
}

func (s *seenIDs) has(id string) bool {
	_, ok := s.set[id]
	return ok
}

func (s *seenIDs) add(id string) {
	if id == "" || s.cap <= 0 {
		return
	}
	if _, ok := s.set[id]; ok {
		return
	}
	if len(s.ring) < s.cap {
		s.ring = append(s.ring, id)
	} else {
		delete(s.set, s.ring[s.next])
		s.ring[s.next] = id
		s.next = (s.next + 1) % s.cap
	}
	s.set[id] = struct{}{}
}
