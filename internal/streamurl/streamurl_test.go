package streamurl

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOK     bool
		wantPlat   string
		wantLogin  string
		wantLabel  string
		wantTwitch bool
	}{
		{
			name:       "Twitch channel https",
			input:      "https://twitch.tv/gaules",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "gaules",
			wantLabel:  "gaules",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with www",
			input:      "https://www.twitch.tv/xqc",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "xqc",
			wantLabel:  "xqc",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with m. subdomain",
			input:      "https://m.twitch.tv/shroud",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "shroud",
			wantLabel:  "shroud",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with trailing slash",
			input:      "https://www.twitch.tv/shroud/",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "shroud",
			wantLabel:  "shroud",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with extra subpath opens channel",
			input:      "https://twitch.tv/gaules/videos",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "gaules",
			wantLabel:  "gaules",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with clips subpath opens channel",
			input:      "https://www.twitch.tv/xqc/clips",
			wantOK:     true,
			wantPlat:   platform.Twitch,
			wantLogin:  "xqc",
			wantLabel:  "xqc",
			wantTwitch: true,
		},
		{
			name:   "Twitch home page is not a channel",
			input:  "https://www.twitch.tv/",
			wantOK: false,
		},
		{
			name:   "Twitch reserved directory",
			input:  "https://www.twitch.tv/directory/game/Minecraft",
			wantOK: false,
		},
		{
			name:   "Twitch reserved videos",
			input:  "https://www.twitch.tv/videos/123456",
			wantOK: false,
		},
		{
			name:   "Twitch reserved downloads",
			input:  "https://twitch.tv/downloads",
			wantOK: false,
		},
		{
			name:   "Twitch reserved settings",
			input:  "https://twitch.tv/settings/profile",
			wantOK: false,
		},
		{
			name:   "Twitch reserved search",
			input:  "https://twitch.tv/search?term=minecraft",
			wantOK: false,
		},
		{
			name:   "Twitch reserved subscriptions",
			input:  "https://twitch.tv/subscriptions",
			wantOK: false,
		},
		{
			name:   "Twitch reserved inventory",
			input:  "https://twitch.tv/inventory",
			wantOK: false,
		},
		{
			name:   "Twitch reserved wallet",
			input:  "https://twitch.tv/wallet",
			wantOK: false,
		},
		{
			name:   "Twitch reserved drops",
			input:  "https://twitch.tv/drops",
			wantOK: false,
		},
		{
			name:   "Twitch reserved p",
			input:  "https://twitch.tv/p/about",
			wantOK: false,
		},
		{
			name:   "Twitch reserved jobs",
			input:  "https://twitch.tv/jobs",
			wantOK: false,
		},
		{
			name:   "Twitch reserved turbo",
			input:  "https://twitch.tv/turbo",
			wantOK: false,
		},
		{
			name:       "Kick channel",
			input:      "https://kick.com/xqc",
			wantOK:     true,
			wantPlat:   platform.Kick,
			wantLogin:  "xqc",
			wantLabel:  "kick:xqc",
			wantTwitch: false,
		},
		{
			name:       "Kick channel uppercase slug is lowercased",
			input:      "https://www.kick.com/Hikaru",
			wantOK:     true,
			wantPlat:   platform.Kick,
			wantLogin:  "hikaru",
			wantLabel:  "kick:hikaru",
			wantTwitch: false,
		},
		{
			name:       "Kick channel on m.kick.com is Kick",
			input:      "https://m.kick.com/XQC",
			wantOK:     true,
			wantPlat:   platform.Kick,
			wantLogin:  "xqc",
			wantLabel:  "kick:xqc",
			wantTwitch: false,
		},
		{
			name:   "Kick bare domain without slug is rejected",
			input:  "https://kick.com/",
			wantOK: false,
		},
		{
			name:   "Kick www bare domain without slug is rejected",
			input:  "https://www.kick.com",
			wantOK: false,
		},
		{
			name:       "YouTube watch URL with v param",
			input:      "https://www.youtube.com/watch?v=5qap5aO4i9A",
			wantOK:     true,
			wantPlat:   platform.YouTube,
			wantLogin:  "5qap5aO4i9A",
			wantLabel:  "youtube:5qap5aO4i9A",
			wantTwitch: false,
		},
		{
			name:   "YouTube watch URL without v param is rejected",
			input:  "https://www.youtube.com/watch",
			wantOK: false,
		},
		{
			name:   "YouTube watch URL with other param but no v is rejected",
			input:  "https://www.youtube.com/watch?feature=shared",
			wantOK: false,
		},
		{
			name:   "YouTube bare domain is rejected",
			input:  "https://www.youtube.com/",
			wantOK: false,
		},
		{
			name:       "YouTube handle live",
			input:      "https://youtube.com/@LofiGirl/live",
			wantOK:     true,
			wantPlat:   platform.YouTube,
			wantLogin:  "@LofiGirl",
			wantLabel:  "youtube:@LofiGirl",
			wantTwitch: false,
		},
		{
			name:       "YouTube short youtu.be URL preserves case",
			input:      "https://youtu.be/dQw4w9WgXcQ",
			wantOK:     true,
			wantPlat:   platform.YouTube,
			wantLogin:  "dQw4w9WgXcQ",
			wantLabel:  "youtube:dQw4w9WgXcQ",
			wantTwitch: false,
		},
		{
			name:   "YouTube short youtu.be bare domain is rejected",
			input:  "https://youtu.be/",
			wantOK: false,
		},
		{
			name:       "Other platform Trovo",
			input:      "https://trovo.live/channel1",
			wantOK:     true,
			wantPlat:   "trovo.live",
			wantLogin:  "channel1",
			wantLabel:  "trovo.live:channel1",
			wantTwitch: false,
		},
		{
			name:       "Other platform with www",
			input:      "https://www.facebook.com/gaming/streamer",
			wantOK:     true,
			wantPlat:   "facebook.com",
			wantLogin:  "streamer",
			wantLabel:  "facebook.com:streamer",
			wantTwitch: false,
		},
		{
			name:       "Arbitrary stream URL",
			input:      "http://example.com/live/feed.m3u8",
			wantOK:     true,
			wantPlat:   "example.com",
			wantLogin:  "feed.m3u8",
			wantLabel:  "example.com:feed.m3u8",
			wantTwitch: false,
		},
		{
			name:   "URL with user and password credentials rejected",
			input:  "https://user:secret@example.com/live",
			wantOK: false,
		},
		{
			name:   "URL with user credentials rejected",
			input:  "https://user@example.com/live",
			wantOK: false,
		},
		{
			name:   "Plain search query",
			input:  "gaules",
			wantOK: false,
		},
		{
			name:   "Query with spaces",
			input:  "just chatting",
			wantOK: false,
		},
		{
			name:   "URL with space inside rejected",
			input:  "https://kick.com/x qc",
			wantOK: false,
		},
		{
			name:   "URL with leading space rejected",
			input:  " https://kick.com/xqc",
			wantOK: false,
		},
		{
			name:   "URL with trailing space rejected",
			input:  "https://kick.com/xqc ",
			wantOK: false,
		},
		{
			name:   "Missing scheme",
			input:  "kick.com/xqc",
			wantOK: false,
		},
		{
			name:   "Invalid scheme ftp",
			input:  "ftp://example.com/live",
			wantOK: false,
		},
		{
			name:   "Empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "URL with control characters",
			input:  "https://kick.com/xqc\x00",
			wantOK: false,
		},
		{
			name:   "URL with embedded newline",
			input:  "https://kick.com/\nxqc",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := Parse(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if info.Platform != tc.wantPlat {
				t.Errorf("Platform = %q, want %q", info.Platform, tc.wantPlat)
			}
			if info.Login != tc.wantLogin {
				t.Errorf("Login = %q, want %q", info.Login, tc.wantLogin)
			}
			if info.Label != tc.wantLabel {
				t.Errorf("Label = %q, want %q", info.Label, tc.wantLabel)
			}
			if info.IsTwitch != tc.wantTwitch {
				t.Errorf("IsTwitch = %v, want %v", info.IsTwitch, tc.wantTwitch)
			}
			if info.URL != tc.input {
				t.Errorf("URL = %q, want %q", info.URL, tc.input)
			}
		})
	}
}

func TestIsStreamURL(t *testing.T) {
	if !IsStreamURL("https://kick.com/xqc") {
		t.Error("expected true for valid stream URL")
	}
	if IsStreamURL("https://kick.com/") {
		t.Error("expected false for bare kick URL without slug")
	}
	if IsStreamURL("just chatting") {
		t.Error("expected false for plain query")
	}
}
