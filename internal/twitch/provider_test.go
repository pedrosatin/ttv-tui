package twitch

import (
	"context"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type dummySource struct{}

func (d *dummySource) Categories(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	return catalog.Page[catalog.Category]{}, nil
}

func (d *dummySource) Channels(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{}, nil
}

func (d *dummySource) Search(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{}, nil
}

func (d *dummySource) SearchCategories(ctx context.Context, query string) ([]catalog.Category, error) {
	return nil, nil
}

func (d *dummySource) ChannelsByIDs(ctx context.Context, ids []string) ([]catalog.Channel, error) {
	return nil, nil
}

func (d *dummySource) ChannelsByLogins(ctx context.Context, logins []string) ([]catalog.Channel, error) {
	return nil, nil
}

func TestTwitchProviderMetadata(t *testing.T) {
	p := NewProvider(nil)

	if id := p.ID(); id != platform.Twitch {
		t.Fatalf("ID() = %q, want %q", id, platform.Twitch)
	}

	if b, ok := p.Browser(); ok || b != nil {
		t.Fatalf("Browser() with nil source = (%v, %v), want (nil, false)", b, ok)
	}
	if s, ok := p.Searcher(); ok || s != nil {
		t.Fatalf("Searcher() with nil source = (%v, %v), want (nil, false)", s, ok)
	}
	if r, ok := p.Resolver(); ok || r != nil {
		t.Fatalf("Resolver() with nil source = (%v, %v), want (nil, false)", r, ok)
	}

	if chat, ok := p.Chat(); ok || chat != nil {
		t.Fatalf("Chat() = (%v, %v), want (nil, false)", chat, ok)
	}

	src := &dummySource{}
	withSrc := NewProvider(src)
	if b, ok := withSrc.Browser(); !ok || b == nil {
		t.Fatalf("Browser() with source = (%v, %v), want non-nil, true", b, ok)
	}
	if s, ok := withSrc.Searcher(); !ok || s == nil {
		t.Fatalf("Searcher() with source = (%v, %v), want non-nil, true", s, ok)
	}
	if r, ok := withSrc.Resolver(); !ok || r == nil {
		t.Fatalf("Resolver() with source = (%v, %v), want non-nil, true", r, ok)
	}
}

func TestTwitchProviderStreamTarget(t *testing.T) {
	p := NewProvider(nil)

	t.Run("empty channel returns error", func(t *testing.T) {
		_, err := p.StreamTarget(catalog.Channel{})
		if err == nil {
			t.Fatal("StreamTarget on empty channel must return error")
		}
	})

	t.Run("login builds standard Twitch URL and label", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{Login: "gaules"})
		if err != nil {
			t.Fatalf("StreamTarget error: %v", err)
		}
		wantURL := "https://www.twitch.tv/gaules"
		if target.URL != wantURL {
			t.Fatalf("target.URL = %q, want %q", target.URL, wantURL)
		}
		if target.Label != "gaules" {
			t.Fatalf("target.Label = %q, want %q", target.Label, "gaules")
		}
	})

	t.Run("URL with login extracts login and preserves URL", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{
			URL: "https://www.twitch.tv/alanzoka",
		})
		if err != nil {
			t.Fatalf("StreamTarget error: %v", err)
		}
		if target.URL != "https://www.twitch.tv/alanzoka" {
			t.Fatalf("target.URL = %q", target.URL)
		}
		if target.Label != "alanzoka" {
			t.Fatalf("target.Label = %q, want alanzoka", target.Label)
		}
	})
}
