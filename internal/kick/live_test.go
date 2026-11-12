package kick

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// TestLive_Contract talks to the real Kick. It is skipped unless
// TTV_KICK_LIVE_SLUG names a channel that is live now: neither the site JSON
// nor the Pusher key has a contract, and this is how to check them by hand.
//
//	TTV_KICK_LIVE_SLUG=<slug> go test ./internal/kick -run TestLive_Contract -v
func TestLive_Contract(t *testing.T) {
	slug := os.Getenv("TTV_KICK_LIVE_SLUG")
	if slug == "" {
		t.Skip("TTV_KICK_LIVE_SLUG not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := NewClient(ClientConfig{})
	ch, err := client.Livestream(ctx, slug)
	if err != nil {
		t.Fatalf("Livestream(%q): %v", slug, err)
	}
	if ch.Availability != catalog.AvailabilityLive {
		t.Fatalf("Livestream(%q) availability = %v, want live", slug, ch.Availability)
	}
	t.Logf("live: title=%q viewers=%v category=%v", deref(ch.Title), derefInt(ch.Viewers), deref(ch.CategoryName))

	chat := NewChat(ChatOptions{Rooms: client, PusherKey: os.Getenv("TTV_KICK_LIVE_KEY")})
	defer chat.Close()
	if _, err := chat.Join(ctx, catalog.Channel{Platform: platform.Kick, Login: slug}); err != nil {
		t.Fatalf("Join: %v", err)
	}
	connected, messages := false, 0
	for messages == 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("no chat message before the deadline (connected=%v)", connected)
		case ev := <-chat.Events():
			if ev.State == catalog.ChatStateError {
				t.Fatalf("chat error: %v (reason %q)", ev.Err, catalog.ChatErrorReasonOf(ev.Err))
			}
			if ev.State == catalog.ChatStateConnected {
				connected = true
			}
			if ev.State != "" {
				t.Logf("state %s err=%v", ev.State, ev.Err)
			}
			for _, m := range ev.Messages {
				messages++
				t.Logf("message from a %d-rune name, %d runes, badges=%v", len([]rune(m.DisplayName)), len([]rune(m.Body)), m.Badges)
			}
		}
	}
	if !connected {
		t.Fatal("messages arrived without a connected state")
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return -1
	}
	return *n
}
