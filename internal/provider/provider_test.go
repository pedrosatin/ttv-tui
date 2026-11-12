package provider

import (
	"reflect"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type dummyTwitchProvider struct{}

func (d *dummyTwitchProvider) ID() string {
	return platform.Twitch
}

func (d *dummyTwitchProvider) Browser() (catalog.Browser, bool) {
	return nil, false
}

func (d *dummyTwitchProvider) Searcher() (catalog.Searcher, bool) {
	return nil, false
}

func (d *dummyTwitchProvider) Resolver() (catalog.Resolver, bool) {
	return nil, false
}

func (d *dummyTwitchProvider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	return catalog.Stream{
		URL:   "https://www.twitch.tv/" + ch.Login,
		Label: ch.Login,
	}, nil
}

func (d *dummyTwitchProvider) Chat() (catalog.Chat, bool) {
	return nil, false
}

type dummyKickProvider struct{}

func (d *dummyKickProvider) ID() string {
	return platform.Kick
}

func (d *dummyKickProvider) Browser() (catalog.Browser, bool) {
	return nil, false
}

func (d *dummyKickProvider) Searcher() (catalog.Searcher, bool) {
	return nil, false
}

func (d *dummyKickProvider) Resolver() (catalog.Resolver, bool) {
	return nil, false
}

func (d *dummyKickProvider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	return catalog.Stream{
		URL:   "https://kick.com/" + ch.Login,
		Label: "kick:" + ch.Login,
	}, nil
}

func (d *dummyKickProvider) Chat() (catalog.Chat, bool) {
	return nil, false
}

func TestURLProvider(t *testing.T) {
	p := NewURLProvider()

	if id := p.ID(); id != "url" {
		t.Fatalf("ID() = %q, want %q", id, "url")
	}

	if b, ok := p.Browser(); ok || b != nil {
		t.Fatalf("Browser() = (%v, %v), want (nil, false)", b, ok)
	}
	if s, ok := p.Searcher(); ok || s != nil {
		t.Fatalf("Searcher() = (%v, %v), want (nil, false)", s, ok)
	}
	if r, ok := p.Resolver(); ok || r != nil {
		t.Fatalf("Resolver() = (%v, %v), want (nil, false)", r, ok)
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
		if err == nil {
			t.Fatal("StreamTarget on channel without URL must return error")
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

	t.Run("Get returns registered provider or false", func(t *testing.T) {
		p, ok := reg.Get("twitch")
		if !ok || p != tw {
			t.Fatalf("Get(\"twitch\") = (%v, %v), want (%v, true)", p, ok, tw)
		}
		p, ok = reg.Get("TWITCH")
		if !ok || p != tw {
			t.Fatalf("Get(\"TWITCH\") = (%v, %v), want (%v, true)", p, ok, tw)
		}
		p, ok = reg.Get("url")
		if !ok || p.ID() != "url" {
			t.Fatalf("Get(\"url\") = (%v, %v), want (urlProvider, true)", p, ok)
		}
		p, ok = reg.Get("kick")
		if ok || p != nil {
			t.Fatalf("Get(\"kick\") = (%v, %v), want (nil, false)", p, ok)
		}
	})

	t.Run("IDs returns sorted list of registered platforms", func(t *testing.T) {
		// Its own registry: registering kick here must not leak into the
		// subtests below, which expect kick to fall back to the URL provider.
		reg := NewRegistry(tw)
		want := []string{"twitch", "url"}
		if got := reg.IDs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("IDs() = %v, want %v", got, want)
		}

		kick := &dummyKickProvider{}
		reg.Register(kick)
		wantWithKick := []string{"kick", "twitch", "url"}
		if got := reg.IDs(); !reflect.DeepEqual(got, wantWithKick) {
			t.Fatalf("IDs() with kick = %v, want %v", got, wantWithKick)
		}
	})

	t.Run("resolves twitch by platform", func(t *testing.T) {
		p, err := reg.ProviderFor(catalog.Channel{Platform: platform.Twitch, Login: "gaules"})
		if err != nil {
			t.Fatalf("ProviderFor twitch: %v", err)
		}
		if p.ID() != platform.Twitch {
			t.Fatalf("resolved provider ID = %q, want twitch", p.ID())
		}
	})

	t.Run("resolves twitch by default for empty platform without URL", func(t *testing.T) {
		p, err := reg.ProviderFor(catalog.Channel{Login: "gaules"})
		if err != nil {
			t.Fatalf("ProviderFor empty platform: %v", err)
		}
		if p.ID() != platform.Twitch {
			t.Fatalf("resolved provider ID = %q, want twitch", p.ID())
		}
	})

	t.Run("resolves url for unhandled platform with stream URL", func(t *testing.T) {
		// New registry without kick registered
		cleanReg := NewRegistry(tw)
		p, err := cleanReg.ProviderFor(catalog.Channel{Platform: platform.Kick, URL: "https://kick.com/xqc"})
		if err != nil {
			t.Fatalf("ProviderFor kick with URL: %v", err)
		}
		if p.ID() != "url" {
			t.Fatalf("resolved provider ID = %q, want url", p.ID())
		}
	})

	t.Run("resolves url from URL parsing when platform is empty", func(t *testing.T) {
		p, err := reg.ProviderFor(catalog.Channel{URL: "https://kick.com/xqc"})
		if err != nil {
			t.Fatalf("ProviderFor empty platform with URL: %v", err)
		}
		if p.ID() != "url" {
			t.Fatalf("resolved provider ID = %q, want url", p.ID())
		}
	})

	t.Run("returns clear error for unhandled platform without URL", func(t *testing.T) {
		cleanReg := NewRegistry(tw)
		p, err := cleanReg.ProviderFor(catalog.Channel{Platform: platform.Kick, Login: "xqc"})
		if err == nil {
			t.Fatalf("ProviderFor kick without URL returned provider %v, want error", p)
		}
		if p != nil {
			t.Fatalf("ProviderFor kick without URL returned non-nil provider: %v", p)
		}
		_, err = cleanReg.StreamTarget(catalog.Channel{Platform: platform.Kick, Login: "xqc"})
		if err == nil {
			t.Fatal("StreamTarget on kick without URL must return error")
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
