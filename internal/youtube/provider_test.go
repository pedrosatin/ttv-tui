package youtube

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

func TestProvider_CapabilitiesAndStreamTarget(t *testing.T) {
	bare := NewProvider()
	if bare.ID() != platform.YouTube {
		t.Errorf("ID = %q, want youtube", bare.ID())
	}
	if _, ok := bare.Browser(); ok {
		t.Error("YouTube must not expose a Browser")
	}
	if _, ok := bare.Searcher(); ok {
		t.Error("YouTube must not expose a Searcher")
	}
	if _, ok := bare.Resolver(); ok {
		t.Error("Resolver without WithResolver")
	}
	if _, ok := bare.Chat(); ok {
		t.Error("Chat without WithChat")
	}

	chat := NewChat(ChatOptions{Client: newFakeRoomClient()})
	defer chat.Close()
	full := NewProvider(WithResolver(stubResolver{}), WithChat(chat))
	if r, ok := full.Resolver(); !ok || r == nil {
		t.Error("Resolver missing")
	}
	if c, ok := full.Chat(); !ok || c != catalog.Chat(chat) {
		t.Error("Chat missing")
	}

	tests := []struct {
		name  string
		ch    catalog.Channel
		url   string
		label string
	}{
		{
			name:  "watch url",
			ch:    catalog.Channel{Platform: platform.YouTube, ID: "youtube:4t4AQ15yj1M", Login: "4t4AQ15yj1M", URL: "https://www.youtube.com/watch?v=4t4AQ15yj1M"},
			url:   "https://www.youtube.com/watch?v=4t4AQ15yj1M",
			label: "youtube:4t4AQ15yj1M",
		},
		{
			name:  "handle url without /live canonicalizes to /live",
			ch:    catalog.Channel{Platform: platform.YouTube, ID: "youtube:@LofiGirl", Login: "@LofiGirl", URL: "https://www.youtube.com/@LofiGirl"},
			url:   "https://www.youtube.com/@LofiGirl/live",
			label: "youtube:@LofiGirl",
		},
		{
			name:  "bare handle without URL",
			ch:    catalog.Channel{Platform: platform.YouTube, Login: "@LofiGirl"},
			url:   "https://www.youtube.com/@LofiGirl/live",
			label: "youtube:@LofiGirl",
		},
		{
			name:  "youtu.be preserves case",
			ch:    catalog.Channel{Platform: platform.YouTube, Login: "AbCdEf12345", URL: "https://youtu.be/AbCdEf12345"},
			url:   "https://youtu.be/AbCdEf12345",
			label: "youtube:AbCdEf12345",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := full.StreamTarget(tc.ch)
			if err != nil {
				t.Fatalf("StreamTarget: %v", err)
			}
			if got.URL != tc.url || got.Label != tc.label || !got.NoAdSignal || len(got.ExtraArgs) != 0 || len(got.Plugins) != 0 {
				t.Errorf("got = %+v, want url=%q label=%q NoAdSignal=true", got, tc.url, tc.label)
			}
		})
	}
	if _, err := full.StreamTarget(catalog.Channel{Platform: platform.YouTube}); err == nil {
		t.Error("empty channel: want error")
	}
}
