package chat

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/config"
)

// ErrAuthentication marks a refused chat login or incomplete credentials.
var ErrAuthentication = errors.New("chat: authentication failed")

// DefaultAddr is the production Twitch IRC over TLS endpoint.
const DefaultAddr = "irc.chat.twitch.tv:6697"

const (
	defaultCoalesceInterval = 50 * time.Millisecond
	defaultInitialBackoff   = 500 * time.Millisecond
	defaultMaxBackoff       = 30 * time.Second
	defaultDialTimeout      = 10 * time.Second
	defaultEventsBuffer     = 64
	scannerInitialBuf       = 8 * 1024
	scannerMaxBuf           = 64 * 1024
)

// State represents the observable lifecycle state of a chat session.
type State string

const (
	StateDisconnected State = "desconectado"
	StateConnecting   State = "conectando"
	StateConnected    State = "conectado"
	StateReconnecting State = "reconectando"
	StateError        State = "erro"
)

// DialFunc establishes a network connection to addr. Production uses TLS;
// tests inject a local TCP or net.Pipe dialer without network access.
type DialFunc func(ctx context.Context, addr string) (net.Conn, error)

// Options configures a chat Manager. Zero values select production defaults.
//
// Channel Switching Strategy:
// Calling Join(login) increments the session generation, cancels the previous
// session's context and closes its underlying connection immediately, and spawns
// a fresh isolated session for the new channel. This guarantees that buffered
// lines or late events from the old channel cannot leak into the new channel.
type Options struct {
	Addr             string
	Token            string
	Username         string
	Dial             DialFunc
	TLSConfig        *tls.Config
	CoalesceInterval time.Duration
	// PingInterval is the heartbeat cadence; PongTimeout is the reply allowance.
	// Any complete incoming IRC line counts as proof of life.
	PingInterval time.Duration
	PongTimeout  time.Duration
	// SendTimeout bounds total queue/confirmation time and each socket write.
	SendTimeout    time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	EventsBuffer   int
	MaxSendQueue   int
	RandNick       func() string
	Now            func() time.Time
	After          func(d time.Duration) <-chan time.Time
}

// SessionEvent is emitted by Manager on state changes, ROOMSTATE updates,
// CLEARCHAT/CLEARMSG moderation actions, outbound delivery updates, or
// coalesced message batches. Every SessionEvent carries Generation so the UI
// can discard stale updates.
type SessionEvent struct {
	Generation  uint64
	Channel     string
	State       State
	Anonymous   bool
	Messages    []Message
	SendUpdates []SendStatusUpdate
	RoomState   *RoomState
	ClearChat   bool
	ClearUser   string
	ClearMsgID  string
	// BanDuration is set for CLEARCHAT timeouts (seconds). Nil with ClearUser
	// set and ClearChat true means a permanent ban.
	BanDuration *int
	Err         error
	Attempt     int
	NextBackoff time.Duration
}

// Manager supervises at most one active channel chat session at a time.
type Manager struct {
	opts   Options
	events chan SessionEvent

	mu        sync.Mutex
	gen       uint64
	channel   string
	cancel    context.CancelFunc
	done      chan struct{}
	closed    bool
	sendCh    chan SendRequest
	pending   []SendRequest
	inFlight  []SendRequest
	limiter   *rateLimiter
	nextNonce uint64
	connected bool
}

// New creates a chat Manager with the given options.
func New(opts Options) *Manager {
	if opts.Addr == "" {
		opts.Addr = DefaultAddr
	}
	opts.Token = config.NormalizeToken(opts.Token)
	opts.Username = strings.ToLower(strings.TrimSpace(opts.Username))
	if opts.CoalesceInterval <= 0 {
		opts.CoalesceInterval = defaultCoalesceInterval
	}
	if opts.PingInterval <= 0 {
		opts.PingInterval = 15 * time.Second
	}
	if opts.PongTimeout <= 0 {
		opts.PongTimeout = 10 * time.Second
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = 10 * time.Second
	}
	if opts.InitialBackoff <= 0 {
		opts.InitialBackoff = defaultInitialBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultMaxBackoff
	}
	if opts.EventsBuffer <= 0 {
		opts.EventsBuffer = defaultEventsBuffer
	}
	if opts.MaxSendQueue <= 0 {
		opts.MaxSendQueue = DefaultMaxSendQueue
	}
	if opts.RandNick == nil {
		opts.RandNick = randomJustinfanNick
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.After == nil {
		opts.After = time.After
	}
	if opts.Dial == nil {
		tlsCfg := opts.TLSConfig
		opts.Dial = func(ctx context.Context, addr string) (net.Conn, error) {
			d := &net.Dialer{Timeout: defaultDialTimeout}
			cfg := tlsCfg
			if cfg == nil {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					host = "irc.chat.twitch.tv"
				}
				cfg = &tls.Config{
					ServerName: host,
					MinVersion: tls.VersionTLS12,
				}
			} else {
				cfg = cfg.Clone()
			}
			tlsDialer := tls.Dialer{
				NetDialer: d,
				Config:    cfg,
			}
			conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				return nil, fmt.Errorf("chat: tls dial %s: %w", addr, err)
			}
			return conn, nil
		}
	}
	return &Manager{
		opts:    opts,
		events:  make(chan SessionEvent, opts.EventsBuffer),
		limiter: newRateLimiter(opts.Now),
	}
}

// HasToken reports whether the Manager is configured with a user access token.
func (m *Manager) HasToken() bool {
	return m.opts.Token != ""
}

// Username returns the normalized login of the authenticated user, if any.
func (m *Manager) Username() string {
	return m.opts.Username
}

// Events returns the stream of SessionEvents emitted by the manager.
func (m *Manager) Events() <-chan SessionEvent {
	return m.events
}

// Generation returns the active session generation.
func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

// Channel returns the normalized login of the currently active channel.
func (m *Manager) Channel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.channel
}

// NextNonce allocates a unique outbound message nonce for tracking delivery.
func (m *Manager) NextNonce() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextNonce++
	return fmt.Sprintf("ttv-%d-%d", m.gen, m.nextNonce)
}

// Send enqueues an outbound message tied to req.Generation and req.Channel.
//
// Rules enforced:
//  1. Without an access token, returns ErrNoToken immediately.
//  2. Empty/whitespace-only bodies return ErrEmptyMessage without touching the wire.
//  3. If the session generation or channel has already changed since the message
//     was composed, the message is discarded and never transmitted to the new
//     session: RejectChannelSwitched for another channel, RejectReconnected for
//     a new session on the same one.
//  4. If the 20 msgs / 30s window or queue capacity is exceeded, the excess message
//     is discarded with RejectRateLimited rather than queued unboundedly.
func (m *Manager) Send(req SendRequest) (SendStatusUpdate, error) {
	body := sanitizeOutboundBody(req.Body)
	if body == "" {
		return SendStatusUpdate{}, ErrEmptyMessage
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return SendStatusUpdate{}, ErrSessionClosed
	}
	if m.opts.Token == "" {
		gen := m.gen
		ch := m.channel
		m.mu.Unlock()
		upd := SendStatusUpdate{
			Generation: gen,
			Channel:    ch,
			Nonce:      req.Nonce,
			Body:       body,
			Status:     DeliveryRejected,
			Reason:     RejectUnauthenticated,
		}
		return upd, ErrNoToken
	}

	reqChan := normalizeChannel(req.Channel)
	if m.channel == "" || m.sendCh == nil {
		m.mu.Unlock()
		return SendStatusUpdate{}, ErrNoActiveChannel
	}

	if (req.Generation != 0 && req.Generation != m.gen) || (reqChan != "" && reqChan != m.channel) {
		activeGen := m.gen
		activeChan := m.channel
		m.mu.Unlock()
		reason := RejectChannelSwitched
		if reqChan == "" || reqChan == activeChan {
			reason = RejectReconnected
		}
		upd := SendStatusUpdate{
			Generation: activeGen,
			Channel:    activeChan,
			Nonce:      req.Nonce,
			Body:       body,
			Status:     DeliveryDiscarded,
			Reason:     reason,
		}
		m.emitUpdate(activeGen, activeChan, upd)
		return upd, ErrChannelSwitched
	}

	if req.Nonce == "" {
		m.nextNonce++
		req.Nonce = fmt.Sprintf("ttv-%d-%d", m.gen, m.nextNonce)
	}
	req.Generation = m.gen
	req.Channel = m.channel
	req.Body = body
	req.expiresAt = time.Now().Add(m.opts.SendTimeout)

	if !m.connected {
		gen, ch := m.gen, m.channel
		m.mu.Unlock()
		upd := SendStatusUpdate{Generation: gen, Channel: ch, Nonce: req.Nonce, Body: body, Status: DeliveryDiscarded, Reason: RejectConnectionLost}
		return upd, ErrDisconnected
	}
	pendingCount := len(m.pending)
	if pendingCount >= m.opts.MaxSendQueue {
		gen := m.gen
		ch := m.channel
		m.mu.Unlock()
		upd := SendStatusUpdate{
			Generation: gen,
			Channel:    ch,
			Nonce:      req.Nonce,
			Body:       body,
			Status:     DeliveryDiscarded,
			Reason:     RejectRateLimited,
		}
		m.emitUpdate(gen, ch, upd)
		return upd, ErrRateLimited
	}

	if _, ok := m.limiter.NextAllowed(req.Channel, pendingCount); !ok {
		gen := m.gen
		ch := m.channel
		m.mu.Unlock()
		upd := SendStatusUpdate{
			Generation: gen,
			Channel:    ch,
			Nonce:      req.Nonce,
			Body:       body,
			Status:     DeliveryDiscarded,
			Reason:     RejectRateLimited,
		}
		m.emitUpdate(gen, ch, upd)
		return upd, ErrRateLimited
	}

	m.pending = append(m.pending, req)
	sendCh := m.sendCh
	gen := m.gen
	ch := m.channel
	m.mu.Unlock()

	select {
	case sendCh <- req:
	default:
	}
	return SendStatusUpdate{
		Generation: gen,
		Channel:    ch,
		Nonce:      req.Nonce,
		Body:       body,
		Status:     DeliverySending,
	}, nil
}

// Join cancels any previous session (including any pending reconnect backoff),
// increments the generation counter, discards any unsent messages from the old
// channel, and starts a new session for channelLogin.
func (m *Manager) Join(channelLogin string) (uint64, error) {
	ch := normalizeChannel(channelLogin)
	if ch == "" {
		return 0, fmt.Errorf("chat: empty channel login")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, fmt.Errorf("chat: manager closed")
	}
	prevCancel := m.cancel
	prevDone := m.done
	prevSendCh := m.sendCh
	prevChannel := m.channel
	discarded := m.collectPendingLocked(prevSendCh)

	m.connected = false
	m.gen++
	gen := m.gen
	m.channel = ch
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sendCh := make(chan SendRequest, m.opts.MaxSendQueue)
	m.cancel = cancel
	m.done = done
	m.sendCh = sendCh
	m.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
	if prevDone != nil {
		<-prevDone
	}

	m.mu.Lock()
	lateDiscarded := m.collectPendingLocked(prevSendCh)
	m.mu.Unlock()
	discarded = append(discarded, lateDiscarded...)

	m.drainStaleEvents(gen)

	if len(discarded) > 0 && prevChannel != "" {
		reason := RejectChannelSwitched
		if prevChannel == ch {
			reason = RejectReconnected
		}
		updates := make([]SendStatusUpdate, 0, len(discarded))
		for _, req := range discarded {
			updates = append(updates, SendStatusUpdate{
				Generation: gen,
				Channel:    ch,
				Nonce:      req.Nonce,
				Body:       req.Body,
				Status:     DeliveryDiscarded,
				Reason:     reason,
			})
		}
		m.emit(ctx, SessionEvent{
			Generation:  gen,
			Channel:     ch,
			State:       StateConnecting,
			Anonymous:   m.opts.Token == "",
			SendUpdates: updates,
		})
	}

	go m.runSession(ctx, done, gen, ch, sendCh)
	return gen, nil
}

func (m *Manager) collectPendingLocked(sendCh chan SendRequest) []SendRequest {
	var out []SendRequest
	if len(m.pending) > 0 {
		out = append(out, m.pending...)
		m.pending = nil
	}
	if len(m.inFlight) > 0 {
		out = append(out, m.inFlight...)
		m.inFlight = nil
	}
	if sendCh != nil {
		for {
			select {
			case <-sendCh:
			default:
				return out
			}
		}
	}
	return out
}

// Reconnect restarts the session for the current channel under a new generation.
func (m *Manager) Reconnect() (uint64, error) {
	m.mu.Lock()
	ch := m.channel
	m.mu.Unlock()
	if ch == "" {
		return 0, fmt.Errorf("chat: no active channel to reconnect")
	}
	return m.Join(ch)
}

// Stop cancels the active session and waits for its goroutine to exit.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.sendCh = nil
	m.pending = nil
	m.inFlight = nil
	m.channel = ""
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Close stops the active session and closes the Events channel.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	cancel := m.cancel
	done := m.done
	m.cancel = nil
	m.done = nil
	m.sendCh = nil
	m.pending = nil
	m.inFlight = nil
	m.channel = ""
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	close(m.events)
}

func (m *Manager) drainStaleEvents(minGen uint64) {
	for {
		select {
		case ev := <-m.events:
			if ev.Generation >= minGen {
				select {
				case m.events <- ev:
				default:
				}
				return
			}
		default:
			return
		}
	}
}

func (m *Manager) isCurrentGen(gen uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.gen == gen
}

func (m *Manager) emitUpdate(gen uint64, channel string, upd SendStatusUpdate) {
	m.mu.Lock()
	if m.closed || m.gen != gen {
		m.mu.Unlock()
		return
	}
	ch := m.events
	anon := m.opts.Token == ""
	m.mu.Unlock()

	ev := SessionEvent{
		Generation:  gen,
		Channel:     channel,
		Anonymous:   anon,
		SendUpdates: []SendStatusUpdate{upd},
	}
	select {
	case ch <- ev:
	default:
	}
}

func (m *Manager) emit(ctx context.Context, ev SessionEvent) {
	if !m.isCurrentGen(ev.Generation) {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}

	m.mu.Lock()
	if m.closed || m.gen != ev.Generation {
		m.mu.Unlock()
		return
	}
	ch := m.events
	m.mu.Unlock()

	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

func (m *Manager) runSession(ctx context.Context, done chan struct{}, gen uint64, channel string, sendCh <-chan SendRequest) {
	defer close(done)

	anon := m.opts.Token == ""
	roomState := RoomState{FollowersOnly: -1}

	m.emit(ctx, SessionEvent{
		Generation: gen,
		Channel:    channel,
		State:      StateConnecting,
		Anonymous:  anon,
	})

	if !anon && m.opts.Username == "" {
		m.emit(ctx, SessionEvent{Generation: gen, Channel: channel, State: StateError, Err: ErrAuthentication})
		return
	}

	backoff := m.opts.InitialBackoff
	attempt := 0
	refusals := 0

	for {
		if ctx.Err() != nil {
			return
		}

		connectedOnce, err := m.connectAndServe(ctx, gen, channel, anon, &roomState, sendCh)
		if ctx.Err() != nil {
			return
		}
		if connectedOnce {
			backoff = m.opts.InitialBackoff
			attempt = 0
			refusals = 0
		}

		m.mu.Lock()
		var failed []SendRequest
		if m.gen == gen {
			m.connected = false
			failed = m.collectPendingLocked(m.sendCh)
		}
		m.mu.Unlock()
		failedUpdates := make([]SendStatusUpdate, 0, len(failed))
		for _, req := range failed {
			failedUpdates = append(failedUpdates, SendStatusUpdate{Generation: gen, Channel: channel, Nonce: req.Nonce, Body: req.Body, Status: DeliveryDiscarded, Reason: RejectConnectionLost})
		}
		if errors.Is(err, ErrAuthentication) {
			refusals++
			if refusals >= 3 {
				m.emit(ctx, SessionEvent{Generation: gen, Channel: channel, State: StateError, Err: ErrAuthentication, SendUpdates: failedUpdates})
				return
			}
		}
		attempt++
		wait := backoff
		if wait > m.opts.MaxBackoff {
			wait = m.opts.MaxBackoff
		}

		m.emit(ctx, SessionEvent{
			Generation:  gen,
			Channel:     channel,
			State:       StateReconnecting,
			Anonymous:   anon,
			Err:         err,
			Attempt:     attempt,
			NextBackoff: wait,
			SendUpdates: failedUpdates,
		})

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		backoff *= 2
		if backoff > m.opts.MaxBackoff {
			backoff = m.opts.MaxBackoff
		}
	}
}

func (m *Manager) matchInFlight(nonce, user, body string) (SendRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inFlight) == 0 {
		return SendRequest{}, false
	}
	if nonce != "" {
		for i, req := range m.inFlight {
			if req.Nonce == nonce {
				m.inFlight = append(m.inFlight[:i], m.inFlight[i+1:]...)
				return req, true
			}
		}
	}
	if m.opts.Username != "" && strings.EqualFold(user, m.opts.Username) {
		for i, req := range m.inFlight {
			if req.Body == body {
				m.inFlight = append(m.inFlight[:i], m.inFlight[i+1:]...)
				return req, true
			}
		}
	}
	return SendRequest{}, false
}

func (m *Manager) matchInFlightByUserState(nonce string) (SendRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inFlight) == 0 {
		return SendRequest{}, false
	}
	if nonce != "" {
		for i, req := range m.inFlight {
			if req.Nonce == nonce {
				m.inFlight = append(m.inFlight[:i], m.inFlight[i+1:]...)
				return req, true
			}
		}
	}
	req := m.inFlight[0]
	m.inFlight = m.inFlight[1:]
	return req, true
}

func (m *Manager) popEarliestInFlight() (SendRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.inFlight) == 0 {
		return SendRequest{}, false
	}
	req := m.inFlight[0]
	m.inFlight = m.inFlight[1:]
	return req, true
}

func (m *Manager) writeHandshake(w io.Writer, channel string, anon bool) (string, error) {
	nick := m.opts.Username
	var b strings.Builder
	b.WriteString("CAP REQ :twitch.tv/tags twitch.tv/commands\r\n")
	if !anon {
		fmt.Fprintf(&b, "PASS oauth:%s\r\n", m.opts.Token)
		fmt.Fprintf(&b, "NICK %s\r\n", nick)
	} else {
		nick = m.opts.RandNick()
		if nick == "" {
			nick = "justinfan12345"
		}
		fmt.Fprintf(&b, "NICK %s\r\n", nick)
	}
	fmt.Fprintf(&b, "JOIN #%s\r\n", channel)

	if _, err := io.WriteString(w, b.String()); err != nil {
		return "", fmt.Errorf("write irc handshake: %w", err)
	}
	return nick, nil
}

func normalizeChannel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "#")
	return s
}

func randomJustinfanNick() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "justinfan42424"
	}
	n := binary.LittleEndian.Uint32(buf[:])%90000 + 10000
	return fmt.Sprintf("justinfan%d", n)
}

func (m *Manager) expireSends(gen uint64, channel string) []SendStatusUpdate {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen != gen {
		return nil
	}
	now := time.Now()
	var updates []SendStatusUpdate
	expire := func(queue []SendRequest) []SendRequest {
		kept := queue[:0]
		for _, req := range queue {
			if !req.expiresAt.IsZero() && !now.Before(req.expiresAt) {
				updates = append(updates, SendStatusUpdate{Generation: gen, Channel: channel, Nonce: req.Nonce, Body: req.Body, Status: DeliveryDiscarded, Reason: RejectTimeout})
			} else {
				kept = append(kept, req)
			}
		}
		return kept
	}
	m.pending = expire(m.pending)
	m.inFlight = expire(m.inFlight)
	return updates
}
