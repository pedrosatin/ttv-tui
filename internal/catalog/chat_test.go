package catalog

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestChatCapabilities_Predefined(t *testing.T) {
	t.Run("twitch capabilities", func(t *testing.T) {
		caps := TwitchChatCapabilities
		if !caps.CanSend {
			t.Fatal("expected Twitch chat to allow sending")
		}
		if caps.MaxMessageLength != 500 {
			t.Fatalf("expected Twitch MaxMessageLength = 500, got %d", caps.MaxMessageLength)
		}
		if !caps.SupportsSlowMode {
			t.Fatal("expected Twitch chat to support slow mode")
		}
		if !caps.SupportsUserNotice {
			t.Fatal("expected Twitch chat to support user notices")
		}
	})

	t.Run("read-only capabilities", func(t *testing.T) {
		caps := ReadOnlyChatCapabilities
		if caps.CanSend {
			t.Fatal("expected read-only chat to disallow sending")
		}
		if caps.MaxMessageLength != 0 {
			t.Fatalf("expected read-only MaxMessageLength = 0, got %d", caps.MaxMessageLength)
		}
		if caps.SupportsSlowMode {
			t.Fatal("expected read-only chat not to support slow mode")
		}
		if caps.SupportsUserNotice {
			t.Fatal("expected read-only chat not to support user notices")
		}
	})
}

// readOnlyMockChat verifies that catalog.Chat can be implemented by a read-only platform.
type readOnlyMockChat struct {
	channel string
	gen     uint64
	events  chan ChatEvent
	stopped bool
}

func newReadOnlyMockChat() *readOnlyMockChat {
	return &readOnlyMockChat{
		events: make(chan ChatEvent, 16),
	}
}

func (m *readOnlyMockChat) Capabilities() ChatCapabilities {
	return ReadOnlyChatCapabilities
}

func (m *readOnlyMockChat) Join(_ context.Context, ch Channel) (uint64, error) {
	m.channel = ch.ID
	m.gen++
	m.stopped = false
	return m.gen, nil
}

func (m *readOnlyMockChat) Reconnect() (uint64, error) {
	m.gen++
	return m.gen, nil
}

func (m *readOnlyMockChat) Stop() {
	m.stopped = true
}

func (m *readOnlyMockChat) Send(req ChatSendRequest) (ChatSendStatusUpdate, error) {
	return ChatSendStatusUpdate{
		Generation: req.Generation,
		Channel:    req.Channel,
		Nonce:      req.Nonce,
		Body:       req.Body,
		Status:     ChatDeliveryDiscarded,
		Reason:     ChatRejectOther,
	}, nil
}

func (m *readOnlyMockChat) Events() <-chan ChatEvent {
	return m.events
}

func (m *readOnlyMockChat) Close() {
	close(m.events)
}

func (m *readOnlyMockChat) HasToken() bool {
	return false
}

func (m *readOnlyMockChat) Username() string {
	return ""
}

func TestReadOnlyMockChat_Contract(t *testing.T) {
	var chat Chat = newReadOnlyMockChat()
	caps := chat.Capabilities()
	if caps.CanSend {
		t.Fatal("read-only chat must declare CanSend = false")
	}

	gen, err := chat.Join(context.Background(), Channel{ID: "streamer", Platform: "kick"})
	if err != nil || gen != 1 {
		t.Fatalf("Join failed: gen=%d err=%v", gen, err)
	}

	upd, err := chat.Send(ChatSendRequest{Generation: gen, Channel: "streamer", Body: "hello"})
	if err != nil {
		t.Fatalf("Send returned unexpected err: %v", err)
	}
	if upd.Status != ChatDeliveryDiscarded {
		t.Fatalf("expected Send on read-only chat to be discarded, got %v", upd.Status)
	}

	chat.Stop()
	chat.Close()
}

func TestHTTPStatusKind(t *testing.T) {
	for status, want := range map[int]ErrorKind{
		429: RateLimit,
		500: Servidor,
		503: Servidor,
		599: Servidor,
		400: Contrato,
		401: Contrato,
		404: Contrato,
		302: Contrato,
	} {
		if got := HTTPStatusKind(status); got != want {
			t.Errorf("HTTPStatusKind(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestChatErrorReasonOf(t *testing.T) {
	cause := errors.New("pusher: error 4001")
	keyed := &ChatError{Reason: ChatErrorKeyRefused, Err: cause}
	wrapped := fmt.Errorf("session: %w", keyed)
	if got := ChatErrorReasonOf(wrapped); got != ChatErrorKeyRefused {
		t.Errorf("reason of a wrapped ChatError = %q, want key_refused", got)
	}
	if !errors.Is(wrapped, cause) || keyed.Error() != cause.Error() {
		t.Errorf("ChatError hides its cause: %v", keyed)
	}
	if got := ChatErrorReasonOf(cause); got != ChatErrorNone {
		t.Errorf("reason of a plain error = %q, want none", got)
	}
	if got := ChatErrorReasonOf(nil); got != ChatErrorNone {
		t.Errorf("reason of nil = %q, want none", got)
	}
	bare := &ChatError{Reason: ChatErrorNoRoom}
	if bare.Error() != "chat: no_room" || bare.Unwrap() != nil {
		t.Errorf("ChatError without a cause = %q / %v", bare.Error(), bare.Unwrap())
	}
	var none *ChatError
	if none.Error() != "<nil>" || none.Unwrap() != nil {
		t.Errorf("nil ChatError = %q / %v", none.Error(), none.Unwrap())
	}
}
