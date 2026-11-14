package youtube

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOK   bool
		wantID   string
		wantPath string
		wantPlay string
	}{
		{
			name:     "watch url",
			input:    "https://www.youtube.com/watch?v=4t4AQ15yj1M",
			wantOK:   true,
			wantID:   "4t4AQ15yj1M",
			wantPath: "/watch?v=4t4AQ15yj1M",
			wantPlay: "https://www.youtube.com/watch?v=4t4AQ15yj1M",
		},
		{
			name:     "youtu.be short url",
			input:    "https://youtu.be/dQw4w9WgXcQ",
			wantOK:   true,
			wantID:   "dQw4w9WgXcQ",
			wantPath: "/watch?v=dQw4w9WgXcQ",
			wantPlay: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name:     "/live/id url",
			input:    "https://www.youtube.com/live/4t4AQ15yj1M?feature=shared",
			wantOK:   true,
			wantID:   "4t4AQ15yj1M",
			wantPath: "/watch?v=4t4AQ15yj1M",
			wantPlay: "https://www.youtube.com/watch?v=4t4AQ15yj1M",
		},
		{
			// streamlink has no matcher for /attribution_link: playback goes by
			// the canonical watch URL of the same video.
			name:     "attribution link with v= plays the canonical watch url",
			input:    "https://www.youtube.com/attribution_link?v=dQw4w9WgXcQ",
			wantOK:   true,
			wantID:   "dQw4w9WgXcQ",
			wantPath: "/watch?v=dQw4w9WgXcQ",
			wantPlay: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name:     "@handle/live url",
			input:    "https://www.youtube.com/@LofiGirl/live",
			wantOK:   true,
			wantID:   "@LofiGirl",
			wantPath: "/@LofiGirl/live",
			wantPlay: "https://www.youtube.com/@LofiGirl/live",
		},
		{
			name:     "@handle url canonicalizes playback to /live",
			input:    "https://youtube.com/@LofiGirl",
			wantOK:   true,
			wantID:   "@LofiGirl",
			wantPath: "/@LofiGirl/live",
			wantPlay: "https://www.youtube.com/@LofiGirl/live",
		},
		{
			name:     "bare @handle",
			input:    "@LofiGirl",
			wantOK:   true,
			wantID:   "@LofiGirl",
			wantPath: "/@LofiGirl/live",
			wantPlay: "https://www.youtube.com/@LofiGirl/live",
		},
		{
			name:     "qualified youtube handle",
			input:    "youtube:@LofiGirl",
			wantOK:   true,
			wantID:   "@LofiGirl",
			wantPath: "/@LofiGirl/live",
			wantPlay: "https://www.youtube.com/@LofiGirl/live",
		},
		{
			name:     "qualified youtube video id preserves case",
			input:    "youtube:AbCdEf12345",
			wantOK:   true,
			wantID:   "AbCdEf12345",
			wantPath: "/watch?v=AbCdEf12345",
			wantPlay: "https://www.youtube.com/watch?v=AbCdEf12345",
		},
		{
			name:     "/channel/id/live url",
			input:    "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
			wantOK:   true,
			wantID:   "channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantPath: "/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
			wantPlay: "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
		},
		{
			name:     "/channel/id url canonicalizes playback to /live",
			input:    "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantOK:   true,
			wantID:   "channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantPath: "/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
			wantPlay: "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
		},
		{
			name:     "/c/name/live url",
			input:    "https://www.youtube.com/c/NomeCanal/live",
			wantOK:   true,
			wantID:   "c/NomeCanal",
			wantPath: "/c/NomeCanal/live",
			wantPlay: "https://www.youtube.com/c/NomeCanal/live",
		},
		{
			name:     "/user/name/live url",
			input:    "https://www.youtube.com/user/NomeUser/live",
			wantOK:   true,
			wantID:   "user/NomeUser",
			wantPath: "/user/NomeUser/live",
			wantPlay: "https://www.youtube.com/user/NomeUser/live",
		},
		{
			name:     "/embed/video_id url",
			input:    "https://www.youtube.com/embed/4t4AQ15yj1M",
			wantOK:   true,
			wantID:   "4t4AQ15yj1M",
			wantPath: "/watch?v=4t4AQ15yj1M",
			wantPlay: "https://www.youtube.com/watch?v=4t4AQ15yj1M",
		},
		{
			name:     "/embed/live_stream?channel=id url",
			input:    "https://www.youtube.com/embed/live_stream?channel=UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantOK:   true,
			wantID:   "channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantPath: "/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
			wantPlay: "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
		},
		{
			name:     "/v/video_id url",
			input:    "https://www.youtube.com/v/dQw4w9WgXcQ",
			wantOK:   true,
			wantID:   "dQw4w9WgXcQ",
			wantPath: "/watch?v=dQw4w9WgXcQ",
			wantPlay: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		},
		{
			name:     "qualified youtube channel ref",
			input:    "youtube:channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantOK:   true,
			wantID:   "channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ",
			wantPath: "/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
			wantPlay: "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/live",
		},
		{name: "empty", input: "", wantOK: false},
		{name: "other domain", input: "https://twitch.tv/gaules", wantOK: false},
		{name: "watch without v", input: "https://www.youtube.com/watch", wantOK: false},
		{name: "bare /live", input: "https://www.youtube.com/live", wantOK: false},
		{name: "bare @", input: "@", wantOK: false},
		{name: "path traversal handle", input: "@..", wantOK: false},
		{name: "channel subpath not live", input: "https://www.youtube.com/@LofiGirl/videos", wantOK: false},
		{name: "channel id subpath not live", input: "https://www.youtube.com/channel/UC-9-kyTW8ZkZNDHQJ6FgpwQ/videos", wantOK: false},
		{name: "invalid 10-char video id", input: "https://www.youtube.com/watch?v=short12345", wantOK: false},
		{name: "invalid 12-char video id", input: "https://www.youtube.com/watch?v=twelvechars1", wantOK: false},
		{name: "credentials rejected", input: "https://u:p@www.youtube.com/watch?v=dQw4w9WgXcQ", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTarget(tc.input)
			if (err == nil) != tc.wantOK {
				t.Fatalf("ParseTarget(%q) err=%v, wantOK=%v", tc.input, err, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.ID != tc.wantID || got.FetchPath != tc.wantPath || got.PlaybackURL != tc.wantPlay {
				t.Errorf("ParseTarget(%q) = %+v, want ID=%q path=%q play=%q", tc.input, got, tc.wantID, tc.wantPath, tc.wantPlay)
			}
		})
	}
}

func TestClient_ResolveWatchPageFixtures(t *testing.T) {
	liveHTML := loadFixture(t, "watch_live.html")
	vodHTML := loadFixture(t, "watch_vod.html")
	offlineSlateHTML := loadFixture(t, "watch_offline_slate.html")
	channelNoLiveHTML := loadFixture(t, "channel_no_live.html")
	botCheckHTML := loadFixture(t, "watch_bot_check.html")
	loginRequiredHTML := []byte(`<!DOCTYPE html><html><head><link rel="canonical" href="https://www.youtube.com/watch?v=agegate1234"></head><body><script>var ytInitialPlayerResponse = {"playabilityStatus": {"status": "LOGIN_REQUIRED", "reason": "Sign in to confirm your age"}};</script></body></html>`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/@LofiGirl/live":
			_, _ = w.Write(liveHTML)
		case "/watch?v=dQw4w9WgXcQ":
			_, _ = w.Write(vodHTML)
		case "/@GitHub/live":
			_, _ = w.Write(offlineSlateHTML)
		case "/@Fireship/live":
			_, _ = w.Write(channelNoLiveHTML)
		case "/watch?v=botcheck123":
			_, _ = w.Write(botCheckHTML)
		case "/watch?v=agegate1234":
			_, _ = w.Write(loginRequiredHTML)
		case "/@Missing404/live":
			http.NotFound(w, r)
		case "/watch?v=forbidden43":
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	ctx := context.Background()

	t.Run("live watch page", func(t *testing.T) {
		res, err := client.Resolve(ctx, "https://www.youtube.com/@LofiGirl")
		if err != nil {
			t.Fatalf("Resolve live: %v", err)
		}
		if res.Availability != catalog.AvailabilityLive {
			t.Errorf("Availability = %v, want live", res.Availability)
		}
		if res.VideoID != "4t4AQ15yj1M" || res.Author != "Lofi Girl" || !strings.Contains(res.Title, "christmas lofi radio") {
			t.Errorf("unexpected metadata: %+v", res)
		}
		if res.Viewers == nil || *res.Viewers != 3560 {
			t.Errorf("Viewers = %v, want 3560", res.Viewers)
		}
		if res.APIKey != "PUBLIC_TEST_API_KEY" || res.ChatContinuation != "TEST_RELOAD_CONTINUATION" {
			t.Errorf("APIKey=%q Continuation=%q", res.APIKey, res.ChatContinuation)
		}
	})

	t.Run("VOD watch page", func(t *testing.T) {
		res, err := client.Resolve(ctx, "https://youtu.be/dQw4w9WgXcQ")
		if !errors.Is(err, ErrNotLive) {
			t.Fatalf("err = %v, want ErrNotLive", err)
		}
		if res.Availability != catalog.AvailabilityOffline {
			t.Errorf("Availability = %v, want offline", res.Availability)
		}
	})

	t.Run("offline slate watch page", func(t *testing.T) {
		res, err := client.Resolve(ctx, "@GitHub")
		if !errors.Is(err, ErrOffline) {
			t.Fatalf("err = %v, want ErrOffline", err)
		}
		if res.Availability != catalog.AvailabilityOffline {
			t.Errorf("Availability = %v, want offline", res.Availability)
		}
	})

	t.Run("channel without live stream", func(t *testing.T) {
		res, err := client.Resolve(ctx, "@Fireship")
		if !errors.Is(err, ErrOffline) {
			t.Fatalf("err = %v, want ErrOffline", err)
		}
		if res.Availability != catalog.AvailabilityOffline {
			t.Errorf("Availability = %v, want offline", res.Availability)
		}
	})

	t.Run("anti-bot verification challenge", func(t *testing.T) {
		_, err := client.Resolve(ctx, "https://www.youtube.com/watch?v=botcheck123")
		if !errors.Is(err, ErrBotCheck) {
			t.Fatalf("err = %v, want ErrBotCheck", err)
		}
		if catalog.KindOf(err) != catalog.Bloqueio {
			t.Errorf("Kind = %v, want Bloqueio", catalog.KindOf(err))
		}
	})

	t.Run("login required without bot check", func(t *testing.T) {
		_, err := client.Resolve(ctx, "https://www.youtube.com/watch?v=agegate1234")
		if !errors.Is(err, ErrLoginRequired) {
			t.Fatalf("err = %v, want ErrLoginRequired", err)
		}
		if errors.Is(err, ErrBotCheck) {
			t.Fatalf("err = %v, must not match ErrBotCheck", err)
		}
	})

	t.Run("404 channel not found", func(t *testing.T) {
		_, err := client.Resolve(ctx, "@Missing404")
		if !errors.Is(err, ErrChannelNotFound) {
			t.Fatalf("err = %v, want ErrChannelNotFound", err)
		}
	})

	t.Run("403 blocked", func(t *testing.T) {
		_, err := client.Resolve(ctx, "https://www.youtube.com/watch?v=forbidden43")
		if catalog.KindOf(err) != catalog.Bloqueio {
			t.Fatalf("Kind = %v, want Bloqueio (err=%v)", catalog.KindOf(err), err)
		}
	})
}

func TestClient_ChannelsByIDs(t *testing.T) {
	liveHTML := loadFixture(t, "watch_live.html")
	vodHTML := loadFixture(t, "watch_vod.html")
	channelNoLiveHTML := loadFixture(t, "channel_no_live.html")
	botCheckHTML := loadFixture(t, "watch_bot_check.html")
	loginRequiredHTML := []byte(`<!DOCTYPE html><html><body><script>var ytInitialPlayerResponse = {"playabilityStatus": {"status": "LOGIN_REQUIRED", "reason": "This video is available to this channel's members on level: Member (or any higher level)."}};</script></body></html>`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/@LofiGirl/live":
			_, _ = w.Write(liveHTML)
		case "/watch?v=dQw4w9WgXcQ":
			_, _ = w.Write(vodHTML)
		case "/@Fireship/live":
			_, _ = w.Write(channelNoLiveHTML)
		case "/watch?v=botcheck123":
			_, _ = w.Write(botCheckHTML)
		case "/watch?v=membersOnly":
			_, _ = w.Write(loginRequiredHTML)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
	chs, err := client.ChannelsByIDs(context.Background(), []string{
		"@LofiGirl",
		"youtube:@LofiGirl", // duplicate collapsed
		"@Fireship",
		"dQw4w9WgXcQ",
		"membersOnly",      // LOGIN_REQUIRED (members/age/private) omitted instead of failing batch
		"@DoesNotExist404", // 404 omitted
	})
	if err != nil {
		t.Fatalf("ChannelsByIDs: %v", err)
	}
	if len(chs) != 3 {
		t.Fatalf("len = %d (%+v), want 3", len(chs), chs)
	}
	if chs[0].Platform != platform.YouTube || chs[0].Availability != catalog.AvailabilityLive || chs[0].Viewers == nil || *chs[0].Viewers != 3560 {
		t.Errorf("chs[0] = %+v, want live LofiGirl", chs[0])
	}
	if chs[1].Availability != catalog.AvailabilityOffline || chs[1].Login != "@Fireship" {
		t.Errorf("chs[1] = %+v, want offline @Fireship", chs[1])
	}
	if chs[2].Availability != catalog.AvailabilityOffline || chs[2].Login != "dQw4w9WgXcQ" {
		t.Errorf("chs[2] = %+v, want offline VOD", chs[2])
	}

	// Case sensitivity: @lofigirl and @LofiGirl are distinct YouTube targets.
	if got := uniqueTargets([]string{"@LofiGirl", "@lofigirl"}); len(got) != 2 {
		t.Errorf("uniqueTargets collapsed case on YouTube: %v", got)
	}

	// Bot check fails batch with Bloqueio so UI preserves saved states.
	if _, err := client.ChannelsByIDs(context.Background(), []string{"botcheck123"}); catalog.KindOf(err) != catalog.Bloqueio {
		t.Errorf("bot check batch err = %v, want Bloqueio", err)
	}
}

func TestParseRetryAfterCapsHugeSeconds(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	h := http.Header{}
	h.Set("Retry-After", "99999999999")
	d, at, ok := parseRetryAfter(h, now)
	if !ok || d != 24*time.Hour || !at.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("parseRetryAfter(huge) = %v, %v, %v; want 24h from now", d, at, ok)
	}
	h.Set("Retry-After", now.Add(90*time.Second).Format(http.TimeFormat))
	if d, _, ok := parseRetryAfter(h, now); !ok || d != 90*time.Second {
		t.Fatalf("parseRetryAfter(date) = %v, %v; want 90s", d, ok)
	}
}
