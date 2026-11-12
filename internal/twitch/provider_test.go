package twitch

import (
	"reflect"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

func TestTwitchProviderMetadata(t *testing.T) {
	p := NewProvider(nil)

	if id := p.ID(); id != platform.Twitch {
		t.Fatalf("ID() = %q, want %q", id, platform.Twitch)
	}

	if src, ok := p.Catalog(); ok || src != nil {
		t.Fatalf("Catalog() with nil source = (%v, %v), want (nil, false)", src, ok)
	}

	if chat, ok := p.Chat(); ok || chat != nil {
		t.Fatalf("Chat() = (%v, %v), want (nil, false)", chat, ok)
	}
}

func TestTwitchProviderStreamTarget(t *testing.T) {
	p := NewProvider(nil)

	t.Run("empty channel gives empty target and no error", func(t *testing.T) {
		target, err := p.StreamTarget(catalog.Channel{})
		if err != nil {
			t.Fatalf("StreamTarget error = %v, want nil", err)
		}
		if target.URL != "" || target.Label != "" {
			t.Fatalf("target = %+v, want empty", target)
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

		// Ensure streamlink command argv matches standard baseline
		cmd := player.DefaultCommand(target, "/run/user/1000/ttv-tui/1/i", "/run/user/1000/ttv-tui/1/mpv-input.conf")
		wantArgs := []string{
			"streamlink",
			"--no-config",
			"--loglevel", "info",
			"https://www.twitch.tv/gaules",
			"best",
			"--player", "mpv",
			"--player-args", "--no-config --no-input-default-bindings --no-osc --osd-level=1 --osd-bar=no " +
				"--volume-max=100 --input-ipc-server=/run/user/1000/ttv-tui/1/i --force-window=immediate " +
				"--hwdec=auto-safe --input-conf=/run/user/1000/ttv-tui/1/mpv-input.conf",
		}
		if !reflect.DeepEqual(cmd.Args, wantArgs) {
			t.Fatalf("streamlink argv mismatch:\ngot:  %v\nwant: %v", cmd.Args, wantArgs)
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
