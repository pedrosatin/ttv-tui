package youtube

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// TestLive_Contract runs against live YouTube when TTV_YOUTUBE_LIVE_TARGET is
// set (e.g. "@LofiGirl" or a live watch URL), keeping usernames/messages
// anonymized in test logs.
//
//	TTV_YOUTUBE_LIVE_TARGET=@LofiGirl go test ./internal/youtube -run TestLive_Contract -v
func TestLive_Contract(t *testing.T) {
	target := os.Getenv("TTV_YOUTUBE_LIVE_TARGET")
	if target == "" {
		t.Skip("TTV_YOUTUBE_LIVE_TARGET not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client := NewClient(ClientConfig{})
	res, err := client.Resolve(ctx, target)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", target, err)
	}
	if res.Availability != catalog.AvailabilityLive {
		t.Fatalf("Availability = %v, want live", res.Availability)
	}
	t.Logf("live video=%s viewers=%v continuation_len=%d", res.VideoID, res.Viewers, len(res.ChatContinuation))

	chat := NewChat(ChatOptions{Client: client})
	defer chat.Close()
	if _, err := chat.Join(ctx, catalog.Channel{Platform: platform.YouTube, Login: res.Target.ID, URL: res.Target.PlaybackURL}); err != nil {
		t.Fatalf("Join: %v", err)
	}

	connected, messages := false, 0
	for messages == 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("no chat messages received before deadline (connected=%v)", connected)
		case ev := <-chat.Events():
			if ev.State == catalog.ChatStateError {
				t.Fatalf("chat error: %v (reason %q)", ev.Err, catalog.ChatErrorReasonOf(ev.Err))
			}
			if ev.State == catalog.ChatStateConnected {
				connected = true
			}
			for _, m := range ev.Messages {
				messages++
				t.Logf("msg #%d: author_runes=%d body_runes=%d", messages, len([]rune(m.DisplayName)), len([]rune(m.Body)))
			}
		}
	}
	if !connected {
		t.Fatal("messages received without connected state")
	}
}
