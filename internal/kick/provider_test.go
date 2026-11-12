package kick

import (
	"context"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type stubResolver struct{}

func (stubResolver) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func TestProvider_Capabilities(t *testing.T) {
	bare := NewProvider()
	if bare.ID() != platform.Kick {
		t.Errorf("ID = %q", bare.ID())
	}
	if _, ok := bare.Browser(); ok {
		t.Error("Browser without WithBrowser")
	}
	if _, ok := bare.Searcher(); ok {
		t.Error("Searcher without WithSearcher")
	}
	if _, ok := bare.Resolver(); ok {
		t.Error("Resolver without WithResolver")
	}
	if _, ok := bare.Chat(); ok {
		t.Error("Chat without WithChat")
	}

	chat := NewChat(ChatOptions{Rooms: &fakeRooms{}})
	defer chat.Close()
	client := NewClient(ClientConfig{})
	full := NewProvider(WithBrowser(client), WithSearcher(client), WithResolver(stubResolver{}), WithChat(chat))
	if b, ok := full.Browser(); !ok || b != catalog.Browser(client) {
		t.Error("Browser missing")
	}
	if s, ok := full.Searcher(); !ok || s != catalog.Searcher(client) {
		t.Error("Searcher missing")
	}
	if r, ok := full.Resolver(); !ok || r == nil {
		t.Error("Resolver missing")
	}
	if c, ok := full.Chat(); !ok || c != catalog.Chat(chat) {
		t.Error("Chat missing")
	}
}

func TestProvider_StreamTarget(t *testing.T) {
	p := NewProvider()
	tests := []struct {
		name  string
		ch    catalog.Channel
		url   string
		label string
	}{
		{"pasted url", catalog.Channel{Platform: platform.Kick, ID: "kick:gaules", Login: "gaules", URL: "https://kick.com/gaules"}, "https://kick.com/gaules", "kick:gaules"},
		{"saved without url", catalog.Channel{Platform: platform.Kick, Login: "Gaules"}, "https://kick.com/Gaules", "kick:Gaules"},
		{"recording keeps its url", catalog.Channel{Platform: platform.Kick, Login: "xqc/videos/0a1b", URL: "https://kick.com/xqc/videos/0a1b"}, "https://kick.com/xqc/videos/0a1b", "kick:xqc/videos/0a1b"},
		{"qualified id only", catalog.Channel{Platform: platform.Kick, ID: "kick:xqc"}, "https://kick.com/xqc", "kick:xqc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.StreamTarget(tc.ch)
			if err != nil {
				t.Fatalf("StreamTarget: %v", err)
			}
			if got.URL != tc.url || got.Label != tc.label || len(got.ExtraArgs) != 0 {
				t.Errorf("target = %+v, want url %q label %q", got, tc.url, tc.label)
			}
			// The label is the session identity the UI compares with. A channel
			// with no login has no label of its own to compare.
			if tc.ch.Login != "" && got.Label != tc.ch.PlayerLabel() {
				t.Errorf("label %q differs from the channel's player label %q", got.Label, tc.ch.PlayerLabel())
			}
		})
	}
	if _, err := p.StreamTarget(catalog.Channel{Platform: platform.Kick}); err == nil {
		t.Error("empty channel: want an error")
	}
}
