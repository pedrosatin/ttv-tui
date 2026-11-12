package chat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Rate-limit parameters enforced per channel and globally across the session.
const (
	RateLimitWindowCount    = 20
	RateLimitWindowDuration = 30 * time.Second
	RateLimitChannelGap     = time.Second
	DefaultMaxSendQueue     = 20
)

// Outbound errors returned synchronously or reported via SendStatusUpdate.
var (
	ErrNoToken         = errors.New("chat: missing access token (run 'ttv-tui login --from-browser')")
	ErrEmptyMessage    = errors.New("chat: empty message")
	ErrNoActiveChannel = errors.New("chat: no active channel")
	ErrRateLimited     = errors.New("chat: rate limit exceeded, message discarded")
	ErrChannelSwitched = errors.New("chat: channel switched before send completed, message discarded")
	ErrDisconnected    = errors.New("chat: disconnected, message not sent")
	ErrSessionClosed   = errors.New("chat: session closed before send completed")
)

// DeliveryStatus tracks the lifecycle of an outbound user message.
type DeliveryStatus = catalog.ChatDeliveryStatus

const (
	DeliveryNone      = catalog.ChatDeliveryNone
	DeliverySending   = catalog.ChatDeliverySending
	DeliveryConfirmed = catalog.ChatDeliveryConfirmed
	DeliveryRejected  = catalog.ChatDeliveryRejected
	DeliveryDiscarded = catalog.ChatDeliveryDiscarded
)

// RejectReason classifies why a sent or queued message was rejected or discarded.
type RejectReason = catalog.ChatRejectReason

const (
	RejectTimeout         = catalog.ChatRejectTimeout
	RejectConnectionLost  = catalog.ChatRejectConnectionLost
	RejectNone            = catalog.ChatRejectNone
	RejectSlowMode        = catalog.ChatRejectSlowMode
	RejectFollowersOnly   = catalog.ChatRejectFollowersOnly
	RejectSubsOnly        = catalog.ChatRejectSubsOnly
	RejectEmoteOnly       = catalog.ChatRejectEmoteOnly
	RejectR9K             = catalog.ChatRejectR9K
	RejectRateLimited     = catalog.ChatRejectRateLimited
	RejectUnauthenticated = catalog.ChatRejectUnauthenticated
	RejectChannelSwitched = catalog.ChatRejectChannelSwitched
	RejectReconnected     = catalog.ChatRejectReconnected
	RejectBanned          = catalog.ChatRejectBanned
	RejectOther           = catalog.ChatRejectOther
)

// SendRequest is an alias for catalog.ChatSendRequest.
type SendRequest = catalog.ChatSendRequest

// SendStatusUpdate is an alias for catalog.ChatSendStatusUpdate.
type SendStatusUpdate = catalog.ChatSendStatusUpdate

// ClassifyRejectNotice maps a Twitch IRC NOTICE msg-id or body to a structured
// RejectReason if the NOTICE represents a message delivery rejection or auth failure.
func ClassifyRejectNotice(msgID, body string) (RejectReason, bool) {
	id := strings.ToLower(strings.TrimSpace(msgID))
	switch {
	case id == "msg_slowmode" || strings.HasPrefix(id, "msg_slow"):
		return RejectSlowMode, true
	case id == "msg_followersonly" || id == "msg_followersonly_followed" || id == "msg_followersonly_zero" || strings.HasPrefix(id, "msg_followersonly"):
		return RejectFollowersOnly, true
	case id == "msg_subsonly":
		return RejectSubsOnly, true
	case id == "msg_emoteonly":
		return RejectEmoteOnly, true
	case id == "msg_r9k" || id == "msg_duplicate":
		return RejectR9K, true
	case id == "msg_ratelimit" || id == "Category_msg_ratelimit":
		return RejectRateLimited, true
	case id == "msg_banned" || id == "msg_suspended" || id == "msg_timedout" || id == "msg_channel_suspended":
		return RejectBanned, true
	case id == "msg_requires_verified_phone_number" || id == "msg_verified_email" || id == "msg_facebook" || id == "no_permission" || id == "unrecognized_cmd":
		return RejectOther, true
	case strings.HasPrefix(id, "msg_"):
		return RejectOther, true
	}

	if id != "" {
		return RejectNone, false
	}
	lower := strings.ToLower(strings.TrimSpace(body))
	switch {
	case strings.Contains(lower, "login authentication failed") || strings.Contains(lower, "improperly formatted auth") || strings.Contains(lower, "unsuccessful login"):
		return RejectUnauthenticated, true
	case strings.Contains(lower, "slow mode"):
		return RejectSlowMode, true
	case strings.Contains(lower, "followers-only") || strings.Contains(lower, "followers only"):
		return RejectFollowersOnly, true
	case strings.Contains(lower, "subscribers-only") || strings.Contains(lower, "subscribers only"):
		return RejectSubsOnly, true
	case strings.Contains(lower, "emote-only") || strings.Contains(lower, "emote only"):
		return RejectEmoteOnly, true
	case strings.Contains(lower, "sending messages too quickly"):
		return RejectRateLimited, true
	}
	return RejectNone, false
}

// rateLimiter enforces Twitch's 20 messages per 30s window and 1 message per
// second per channel without unbounded queueing.
type rateLimiter struct {
	now        func() time.Time
	window     []time.Time
	lastByChan map[string]time.Time
}

func newRateLimiter(now func() time.Time) *rateLimiter {
	if now == nil {
		now = time.Now
	}
	return &rateLimiter{
		now:        now,
		lastByChan: make(map[string]time.Time),
	}
}

func (r *rateLimiter) prune(now time.Time) {
	cutoff := now.Add(-RateLimitWindowDuration)
	keep := 0
	for keep < len(r.window) && !r.window[keep].After(cutoff) {
		keep++
	}
	if keep > 0 {
		copy(r.window, r.window[keep:])
		r.window = r.window[:len(r.window)-keep]
	}
}

// NextAllowed calculates when a message to channel (coupled with pendingCount
// already-queued items ahead of it) would be allowed to transmit, or returns
// ok=false if the 20 messages / 30s window is exhausted.
func (r *rateLimiter) NextAllowed(channel string, pendingCount int) (wait time.Duration, ok bool) {
	now := r.now()
	r.prune(now)

	if len(r.window)+pendingCount >= RateLimitWindowCount {
		return 0, false
	}

	ch := normalizeChannel(channel)
	last, hasLast := r.lastByChan[ch]
	earliest := now
	if hasLast {
		nextChan := last.Add(time.Duration(pendingCount+1) * RateLimitChannelGap)
		if nextChan.After(earliest) {
			earliest = nextChan
		}
	} else if pendingCount > 0 {
		nextChan := now.Add(time.Duration(pendingCount) * RateLimitChannelGap)
		if nextChan.After(earliest) {
			earliest = nextChan
		}
	}

	if earliest.After(now) {
		return earliest.Sub(now), true
	}
	return 0, true
}

// DelayUntilReady returns the duration from now until the head of the queue for
// channel can be transmitted, or (0, false) if the 20/30s window is currently full.
func (r *rateLimiter) DelayUntilReady(channel string) (wait time.Duration, allowedNow bool, windowFull bool) {
	now := r.now()
	r.prune(now)

	if len(r.window) >= RateLimitWindowCount {
		oldest := r.window[0]
		untilWindow := oldest.Add(RateLimitWindowDuration).Sub(now)
		if untilWindow < 0 {
			untilWindow = 0
		}
		return untilWindow, false, true
	}

	ch := normalizeChannel(channel)
	if last, ok := r.lastByChan[ch]; ok {
		next := last.Add(RateLimitChannelGap)
		if next.After(now) {
			return next.Sub(now), false, false
		}
	}
	return 0, true, false
}

// Record marks a message as transmitted on channel at the current clock time.
func (r *rateLimiter) Record(channel string) {
	now := r.now()
	r.prune(now)
	r.window = append(r.window, now)
	r.lastByChan[normalizeChannel(channel)] = now
}

// FormatPrivMsg builds the wire-formatted IRCv3 PRIVMSG command including client-nonce.
func FormatPrivMsg(channel, nonce, body string) string {
	ch := normalizeChannel(channel)
	clean := sanitizeOutboundBody(body)
	if nonce != "" {
		return fmt.Sprintf("@client-nonce=%s PRIVMSG #%s :%s\r\n", sanitizeTagToken(nonce), ch, clean)
	}
	return fmt.Sprintf("PRIVMSG #%s :%s\r\n", ch, clean)
}

// IsBlockedCommand reports whether body would be published as plain text instead
// of running as a command. Twitch dropped chat commands over IRC: only /me still
// works, so /ban and friends reach the whole channel as an ordinary message.
// Only the start of the message counts, judged on the text that goes on the wire.
func IsBlockedCommand(body string) bool {
	return catalog.IsBlockedCommand(body)
}

func sanitizeOutboundBody(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\r' || r == '\n' || r == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func sanitizeTagToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
