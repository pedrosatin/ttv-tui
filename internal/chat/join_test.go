package chat

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Join takes the resolved channel; the IRC form (lower case, no "#") is the
// Twitch implementation's business, not the caller's.
func TestManagerJoinTakesTheChannelAndNormalizesForIRC(t *testing.T) {
	mgr := New(Options{Dial: func(context.Context, string) (net.Conn, error) {
		return nil, errors.New("offline test")
	}})
	t.Cleanup(mgr.Close)

	for _, tc := range []struct {
		name string
		ch   catalog.Channel
		want string
	}{
		{"login", catalog.Channel{Platform: "twitch", Login: " #GauLes "}, "gaules"},
		{"qualified id only", catalog.Channel{Platform: "twitch", ID: "twitch:XQC"}, "xqc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mgr.Join(context.Background(), tc.ch); err != nil {
				t.Fatalf("Join: %v", err)
			}
			if got := mgr.Channel(); got != tc.want {
				t.Fatalf("channel = %q, want %q", got, tc.want)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mgr.Join(ctx, catalog.Channel{Login: "other"}); err == nil {
		t.Fatal("Join with a canceled context must fail")
	}
	if got := mgr.Channel(); got != "xqc" {
		t.Fatalf("a failed Join changed the channel to %q", got)
	}
}
