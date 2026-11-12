package provider

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

type dummyTwitchProvider struct{}

func (d *dummyTwitchProvider) ID() string {
	return platform.Twitch
}

func (d *dummyTwitchProvider) Catalog() (catalog.Source, bool) {
	return nil, false
}

func (d *dummyTwitchProvider) StreamTarget(ch catalog.Channel) (player.Target, error) {
	return player.Target{
		URL:   "https://www.twitch.tv/" + ch.Login,
		Label: ch.Login,
	}, nil
}

func (d *dummyTwitchProvider) Chat() (Chat, bool) {
	return nil, false
}

func TestURLProvider(t *testing.T) {
	p := NewURLProvider()

	if id := p.ID(); id != "url" {
		t.Fatalf("ID() = %q, want %q", id, "url")
	}

	if src, ok := p.Catalog(); ok || src != nil {
		t.Fatalf("Catalog() = (%v, %v), want (nil, false)", src, ok)
	}

	if chat, ok := p.Chat(); ok || chat != nil {
		t.Fatalf("Chat() = (%v, %v), want (nil, false)", chat, ok)
	}

	t.Run("missing URL error", func(t *testing.T) {
		_, err := p.StreamTarget(catalog.Channel{})
		if err == nil {
			t.Fatal("StreamTarget on empty channel must return error")
		}

		_, err = p.StreamTarget(catalog.Channel{Platform: platform.Kick, Login: "xqc"})
		if err == nil || err.Error() != "missing URL for non-Twitch channel" {
			t.Fatalf("StreamTarget on non-Twitch channel without URL error = %v, want 'missing URL for non-Twitch channel'", err)
		}
	})

	t.Run("valid Kick URL", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{
			URL: "https://kick.com/xqc",
		})
		if err != nil {
			t.Fatalf("StreamTarget: %v", err)
		}
		if target.URL != "https://kick.com/xqc" {
			t.Fatalf("target.URL = %q, want https://kick.com/xqc", target.URL)
		}
		if target.Label != "kick:xqc" {
			t.Fatalf("target.Label = %q, want kick:xqc", target.Label)
		}
	})

	t.Run("valid YouTube URL", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{
			URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		})
		if err != nil {
			t.Fatalf("StreamTarget: %v", err)
		}
		if target.URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
			t.Fatalf("target.URL = %q", target.URL)
		}
		if target.Label != "youtube:dQw4w9WgXcQ" {
			t.Fatalf("target.Label = %q, want youtube:dQw4w9WgXcQ", target.Label)
		}
	})

	t.Run("custom label preserved", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{
			Platform: platform.Kick,
			Login:    "xqc",
			URL:      "https://kick.com/xqc",
		})
		if err != nil {
			t.Fatalf("StreamTarget: %v", err)
		}
		if target.Label != "kick:xqc" {
			t.Fatalf("target.Label = %q, want kick:xqc", target.Label)
		}
	})
}

func TestRegistry(t *testing.T) {
	tw := &dummyTwitchProvider{}
	reg := NewRegistry(tw)

	t.Run("resolves twitch by platform", func(t *testing.T) {
		p := reg.ProviderFor(catalog.Channel{Platform: platform.Twitch, Login: "gaules"})
		if p.ID() != platform.Twitch {
			t.Fatalf("resolved provider ID = %q, want twitch", p.ID())
		}
	})

	t.Run("resolves twitch by default for empty platform without URL", func(t *testing.T) {
		p := reg.ProviderFor(catalog.Channel{Login: "gaules"})
		if p.ID() != platform.Twitch {
			t.Fatalf("resolved provider ID = %q, want twitch", p.ID())
		}
	})

	t.Run("resolves url for non-twitch or unknown platform", func(t *testing.T) {
		p := reg.ProviderFor(catalog.Channel{Platform: platform.Kick, URL: "https://kick.com/xqc"})
		if p.ID() != "url" {
			t.Fatalf("resolved provider ID = %q, want url", p.ID())
		}
	})

	t.Run("resolves url from URL parsing when platform is empty", func(t *testing.T) {
		p := reg.ProviderFor(catalog.Channel{URL: "https://kick.com/xqc"})
		if p.ID() != "url" {
			t.Fatalf("resolved provider ID = %q, want url", p.ID())
		}
	})

	t.Run("streamTarget dispatch", func(t *testing.T) {
		target, err := reg.StreamTarget(catalog.Channel{Login: "gaules"})
		if err != nil {
			t.Fatalf("StreamTarget: %v", err)
		}
		if target.URL != "https://www.twitch.tv/gaules" {
			t.Fatalf("target.URL = %q", target.URL)
		}

		target, err = reg.StreamTarget(catalog.Channel{URL: "https://kick.com/xqc"})
		if err != nil {
			t.Fatalf("StreamTarget url: %v", err)
		}
		if target.URL != "https://kick.com/xqc" {
			t.Fatalf("target.URL = %q", target.URL)
		}
	})
}
