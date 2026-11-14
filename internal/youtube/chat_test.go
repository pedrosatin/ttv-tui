package youtube

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type pollResponse struct {
	res PollResult
	err error
}

type fakeRoomClient struct {
	mu          sync.Mutex
	resolveFunc func(ctx context.Context, rawTarget string) (Resolved, error)
	polls       []PollRequest
	pollTimes   []time.Time
	queue       chan pollResponse
}

func newFakeRoomClient() *fakeRoomClient {
	return &fakeRoomClient{queue: make(chan pollResponse, 16)}
}

func (f *fakeRoomClient) Resolve(ctx context.Context, rawTarget string) (Resolved, error) {
	f.mu.Lock()
	fn := f.resolveFunc
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, rawTarget)
	}
	return Resolved{
		Target:           Target{Kind: targetHandle, ID: "@LofiGirl", PlaybackURL: "https://www.youtube.com/@LofiGirl/live"},
		VideoID:          "4t4AQ15yj1M",
		APIKey:           "PUBLIC_TEST_API_KEY",
		ClientVersion:    "2.20261112.10.00",
		ChatContinuation: "CONT_0",
		Availability:     catalog.AvailabilityLive,
	}, nil
}

func (f *fakeRoomClient) PollChat(ctx context.Context, req PollRequest, _ time.Time) (PollResult, error) {
	f.mu.Lock()
	f.polls = append(f.polls, req)
	f.pollTimes = append(f.pollTimes, time.Now())
	f.mu.Unlock()
	select {
	case resp := <-f.queue:
		return resp.res, resp.err
	case <-ctx.Done():
		return PollResult{}, ctx.Err()
	}
}

func nextChatEvent(t *testing.T, ch <-chan catalog.ChatEvent) catalog.ChatEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("chat events closed unexpectedly")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for chat event")
		return catalog.ChatEvent{}
	}
}

func TestChat_ReadOnlyMetadataAndSend(t *testing.T) {
	chat := NewChat(ChatOptions{Client: newFakeRoomClient()})
	defer chat.Close()

	if chat.Capabilities() != catalog.ReadOnlyChatCapabilities {
		t.Errorf("Capabilities = %+v, want read-only", chat.Capabilities())
	}
	if chat.HasToken() || chat.Username() != "" {
		t.Errorf("HasToken=%v Username=%q, want anonymous", chat.HasToken(), chat.Username())
	}
	upd, err := chat.Send(catalog.ChatSendRequest{Channel: "@LofiGirl", Nonce: "n1", Body: "hi"})
	if !errors.Is(err, ErrReadOnly) || upd.Status != catalog.ChatDeliveryRejected {
		t.Errorf("Send = (%+v, %v), want ErrReadOnly rejected", upd, err)
	}
}

func TestChat_PollsMessagesRespectsServerIntervalAndDedups(t *testing.T) {
	rc := newFakeRoomClient()
	rc.queue <- pollResponse{res: PollResult{
		NextContinuation: "CONT_1",
		Timeout:          80 * time.Millisecond,
		Messages: []catalog.ChatMessage{
			{ID: "m1", DisplayName: "@Alice", User: "@Alice", Body: "first", Kind: catalog.ChatMessageChat},
			{ID: "m2", DisplayName: "@Bob", User: "@Bob", Body: "second", Kind: catalog.ChatMessageChat},
		},
		DeletedIDs: []string{"m1"},
	}}
	rc.queue <- pollResponse{res: PollResult{
		NextContinuation: "CONT_2",
		Timeout:          20 * time.Millisecond,
		Messages: []catalog.ChatMessage{
			{ID: "m2", DisplayName: "@Bob", User: "@Bob", Body: "second", Kind: catalog.ChatMessageChat}, // duplicate ignored
			{ID: "m3", DisplayName: "@Carol", User: "@Carol", Body: "third", Kind: catalog.ChatMessageChat},
		},
	}}

	chat := NewChat(ChatOptions{
		Client:          rc,
		MinPollInterval: 5 * time.Millisecond,
		PollInterval:    50 * time.Millisecond,
		MaxPollInterval: 500 * time.Millisecond,
		InitialBackoff:  10 * time.Millisecond,
		MaxBackoff:      40 * time.Millisecond,
	})
	defer chat.Close()

	gen, err := chat.Join(context.Background(), catalog.Channel{Platform: platform.YouTube, Login: "@LofiGirl"})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	ev1 := nextChatEvent(t, chat.Events())
	if ev1.State != catalog.ChatStateConnecting || ev1.Generation != gen {
		t.Fatalf("ev1 = %+v", ev1)
	}
	ev2 := nextChatEvent(t, chat.Events())
	if ev2.State != catalog.ChatStateConnected {
		t.Fatalf("ev2 = %+v", ev2)
	}
	ev3 := nextChatEvent(t, chat.Events())
	if len(ev3.Messages) != 2 || ev3.Messages[0].ID != "m1" || ev3.Messages[1].ID != "m2" {
		t.Fatalf("ev3 = %+v", ev3)
	}
	ev4 := nextChatEvent(t, chat.Events())
	if ev4.ClearMsgID != "m1" {
		t.Fatalf("ev4 = %+v, want ClearMsgID m1 after batch", ev4)
	}
	ev5 := nextChatEvent(t, chat.Events())
	if len(ev5.Messages) != 1 || ev5.Messages[0].ID != "m3" {
		t.Fatalf("ev5 = %+v, want only deduped m3", ev5)
	}

	rc.mu.Lock()
	t0, t1 := rc.pollTimes[0], rc.pollTimes[1]
	polls := slices.Clone(rc.polls)
	rc.mu.Unlock()

	if gap := t1.Sub(t0); gap < 70*time.Millisecond {
		t.Errorf("polled too early: %v < 80ms server timeout", gap)
	}
	if len(polls) < 2 || polls[0].Continuation != "CONT_0" || polls[1].Continuation != "CONT_1" {
		t.Errorf("polls = %+v", polls)
	}
}

func TestChat_ErrorClassificationsAndBackoff(t *testing.T) {
	cases := []struct {
		name       string
		resolveErr error
		wantReason catalog.ChatErrorReason
	}{
		{"VOD", ErrNotLive, catalog.ChatErrorVOD},
		{"offline", ErrOffline, catalog.ChatErrorNoRoom},
		{"not found", ErrChannelNotFound, catalog.ChatErrorNoRoom},
		{"chat disabled", ErrChatDisabled, catalog.ChatErrorNoRoom},
		{"bot check", ErrBotCheck, catalog.ChatErrorBotCheck},
		{"HTTP 403 block", &catalog.Error{Platform: platform.YouTube, Kind: catalog.Bloqueio, StatusCode: 403}, catalog.ChatErrorBlocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := newFakeRoomClient()
			rc.resolveFunc = func(context.Context, string) (Resolved, error) {
				return Resolved{}, tc.resolveErr
			}
			chat := NewChat(ChatOptions{Client: rc})
			defer chat.Close()

			if _, err := chat.Join(context.Background(), catalog.Channel{Platform: platform.YouTube, Login: "@LofiGirl"}); err != nil {
				t.Fatalf("Join: %v", err)
			}
			_ = nextChatEvent(t, chat.Events()) // connecting
			ev := nextChatEvent(t, chat.Events())
			if ev.State != catalog.ChatStateError || catalog.ChatErrorReasonOf(ev.Err) != tc.wantReason {
				t.Fatalf("ev = %+v (reason=%q), want reason %q", ev, catalog.ChatErrorReasonOf(ev.Err), tc.wantReason)
			}
		})
	}

	t.Run("transient network error triggers backoff reconnect", func(t *testing.T) {
		rc := newFakeRoomClient()
		attempts := 0
		rc.resolveFunc = func(context.Context, string) (Resolved, error) {
			attempts++
			if attempts == 1 {
				return Resolved{}, &catalog.Error{Platform: platform.YouTube, Kind: catalog.Rede}
			}
			return Resolved{
				VideoID:          "4t4AQ15yj1M",
				ChatContinuation: "CONT_OK",
			}, nil
		}
		rc.queue <- pollResponse{res: PollResult{NextContinuation: "CONT_1", Timeout: 50 * time.Millisecond}}

		chat := NewChat(ChatOptions{
			Client:         rc,
			InitialBackoff: 15 * time.Millisecond,
			MaxBackoff:     60 * time.Millisecond,
		})
		defer chat.Close()

		if _, err := chat.Join(context.Background(), catalog.Channel{Platform: platform.YouTube, Login: "@LofiGirl"}); err != nil {
			t.Fatalf("Join: %v", err)
		}
		_ = nextChatEvent(t, chat.Events()) // connecting
		reconn := nextChatEvent(t, chat.Events())
		if reconn.State != catalog.ChatStateReconnecting || reconn.Attempt != 1 || reconn.NextBackoff != 15*time.Millisecond {
			t.Fatalf("reconn = %+v", reconn)
		}
		conn := nextChatEvent(t, chat.Events())
		if conn.State != catalog.ChatStateConnected {
			t.Fatalf("conn = %+v, want connected", conn)
		}
	})
}

func TestChat_NextPollWaitBounds(t *testing.T) {
	chat := NewChat(ChatOptions{
		MinPollInterval: 2 * time.Second,
		PollInterval:    5 * time.Second,
		MaxPollInterval: 30 * time.Second,
	})
	defer chat.Close()

	if got := chat.nextPollWait(10 * time.Second); got != 10*time.Second {
		t.Errorf("10s -> %v, want 10s", got)
	}
	if got := chat.nextPollWait(120 * time.Second); got != 30*time.Second {
		t.Errorf("120s -> %v, want ceiling 30s", got)
	}
	if got := chat.nextPollWait(0); got != 5*time.Second {
		t.Errorf("0 -> %v, want default 5s", got)
	}
}
