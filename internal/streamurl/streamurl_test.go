package streamurl

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/store"
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
			wantPlat:   store.PlatformTwitch,
			wantLogin:  "gaules",
			wantLabel:  "gaules",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with www",
			input:      "https://www.twitch.tv/xqc",
			wantOK:     true,
			wantPlat:   store.PlatformTwitch,
			wantLogin:  "xqc",
			wantLabel:  "xqc",
			wantTwitch: true,
		},
		{
			name:       "Twitch channel with trailing slash",
			input:      "https://www.twitch.tv/shroud/",
			wantOK:     true,
			wantPlat:   store.PlatformTwitch,
			wantLogin:  "shroud",
			wantLabel:  "shroud",
			wantTwitch: true,
		},
		{
			name:   "Twitch home page is not a channel",
			input:  "https://www.twitch.tv/",
			wantOK: false,
		},
		{
			name:   "Twitch sub-directory is not a channel",
			input:  "https://www.twitch.tv/directory/game/Minecraft",
			wantOK: false,
		},
		{
			name:       "Kick channel",
			input:      "https://kick.com/xqc",
			wantOK:     true,
			wantPlat:   store.PlatformKick,
			wantLogin:  "xqc",
			wantLabel:  "kick:xqc",
			wantTwitch: false,
		},
		{
			name:       "Kick channel with www",
			input:      "https://www.kick.com/hikaru",
			wantOK:     true,
			wantPlat:   store.PlatformKick,
			wantLogin:  "hikaru",
			wantLabel:  "kick:hikaru",
			wantTwitch: false,
		},
		{
			name:       "YouTube watch URL",
			input:      "https://www.youtube.com/watch?v=5qap5aO4i9A",
			wantOK:     true,
			wantPlat:   store.PlatformYouTube,
			wantLogin:  "5qap5aO4i9A",
			wantLabel:  "youtube:5qap5aO4i9A",
			wantTwitch: false,
		},
		{
			name:       "YouTube handle live",
			input:      "https://youtube.com/@LofiGirl/live",
			wantOK:     true,
			wantPlat:   store.PlatformYouTube,
			wantLogin:  "@LofiGirl",
			wantLabel:  "youtube:@LofiGirl",
			wantTwitch: false,
		},
		{
			name:       "YouTube short youtu.be URL",
			input:      "https://youtu.be/dQw4w9WgXcQ",
			wantOK:     true,
			wantPlat:   store.PlatformYouTube,
			wantLogin:  "dQw4w9WgXcQ",
			wantLabel:  "youtube:dQw4w9WgXcQ",
			wantTwitch: false,
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
		})
	}
}
