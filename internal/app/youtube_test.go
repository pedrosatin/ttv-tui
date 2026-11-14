package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
	"github.com/pedrosatin/ttv-tui/internal/youtube"
)

func mustYTFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "youtube", "testdata", name))
	if err != nil {
		t.Fatalf("read youtube fixture %s: %v", name, err)
	}
	return b
}

func TestYouTubeProviderFactory(t *testing.T) {
	prov, stop := youtubeProvider(nil)
	if prov.ID() != platform.YouTube {
		t.Fatalf("ID = %q, want youtube", prov.ID())
	}
	if _, ok := prov.Browser(); ok {
		t.Error("YouTube provider must not expose Browser")
	}
	if _, ok := prov.Searcher(); ok {
		t.Error("YouTube provider must not expose Searcher")
	}
	if r, ok := prov.Resolver(); !ok || r == nil {
		t.Error("default YouTube provider missing Resolver")
	}
	c, ok := prov.Chat()
	if !ok || c.Capabilities().CanSend {
		t.Fatalf("default YouTube chat = (%v, %v), want read-only", c, ok)
	}
	stop()
	if _, open := <-c.Events(); open {
		t.Error("stop left default YouTube chat events open")
	}

	// An injected provider is stopped, not closed.
	customChat := youtube.NewChat(youtube.ChatOptions{})
	defer customChat.Close()
	injected := youtube.NewProvider(youtube.WithChat(customChat))
	got, stopInjected := youtubeProvider(injected)
	if got != injected {
		t.Fatal("injected provider was replaced")
	}
	stopInjected()
	if _, err := customChat.Reconnect(); err == nil {
		t.Error("stop left injected YouTube session active")
	}
}

func TestChatBridgeServesYouTubeChat(t *testing.T) {
	watchHTML := mustYTFixture(t, "watch_live.html")
	chatJSON := mustYTFixture(t, "get_live_chat.json")

	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/watch" && r.URL.Query().Get("v") == "live1234567":
			_, _ = w.Write(watchHTML)
		case r.URL.Path == "/youtubei/v1/live_chat/get_live_chat":
			if polls.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(chatJSON)
				return
			}
			// Subsequent polls stay quiet until stopped.
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := youtube.NewClient(youtube.ClientConfig{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	yc := youtube.NewChat(youtube.ChatOptions{
		Client:          client,
		MinPollInterval: time.Millisecond,
		PollInterval:    5 * time.Millisecond,
		MaxPollInterval: 20 * time.Millisecond,
	})
	defer yc.Close()

	ytProv := youtube.NewProvider(youtube.WithResolver(client), youtube.WithChat(yc))
	tw := newFakeChatImpl(catalog.TwitchChatCapabilities)
	reg := provider.NewRegistry(fakeProvider{id: platform.Twitch, chat: tw}, ytProv, provider.NewURLProvider())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newChatBridge(ctx, reg)
	out := make(chan tea.Msg, 64)
	go b.forward(ctx, func(m tea.Msg) { out <- m })

	ch := catalog.Channel{
		Platform: platform.YouTube,
		ID:       "live1234567",
		Login:    "live1234567",
		URL:      "https://www.youtube.com/watch?v=live1234567",
	}
	if caps, ok := b.Capabilities(ch); !ok || caps.CanSend {
		t.Fatalf("Capabilities(youtube) = (%+v, %v), want read-only chat", caps, ok)
	}
	ev := join(t, b, ch)
	if ev.Platform != platform.YouTube || ev.Channel != "live1234567" || !ev.Anonymous {
		t.Fatalf("join event = %+v", ev)
	}
	if b.HasToken() || b.Username() != "" {
		t.Error("YouTube bridge reported a login")
	}

	var gotMsgs []ui.ChatMessage
	connected := false
	deadline := time.After(5 * time.Second)
	for len(gotMsgs) == 0 || !connected {
		select {
		case m := <-out:
			ce := m.(ui.ChatEventMsg)
			if ce.Platform != platform.YouTube {
				t.Fatalf("event platform = %q, want youtube", ce.Platform)
			}
			if ce.State == ui.ChatStateConnected {
				connected = true
			}
			gotMsgs = append(gotMsgs, ce.Messages...)
		case <-deadline:
			t.Fatalf("timeout waiting for YouTube messages (connected=%v msgs=%d)", connected, len(gotMsgs))
		}
	}
	if len(gotMsgs) < 2 {
		t.Fatalf("got %d messages from fixture, want >= 2", len(gotMsgs))
	}

	upd := b.Send(ui.ChatSendRequest{Generation: ev.Generation, Channel: "live1234567", Nonce: "n1", Body: "hi"})().(ui.ChatEventMsg)
	if len(upd.SendUpdates) != 1 || upd.SendUpdates[0].Status == catalog.ChatDeliveryConfirmed {
		t.Fatalf("Send on read-only YouTube chat = %+v, want refusal", upd.SendUpdates)
	}
}

func TestChatBridgePropagatesYouTubeErrorReasons(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    catalog.ChatErrorReason
	}{
		{"vod", "watch_vod.html", catalog.ChatErrorVOD},
		{"bot_check", "watch_bot_check.html", catalog.ChatErrorBotCheck},
		{"offline", "channel_no_live.html", catalog.ChatErrorNoRoom},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := mustYTFixture(t, tc.fixture)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(html)
			}))
			defer srv.Close()

			client := youtube.NewClient(youtube.ClientConfig{BaseURL: srv.URL, HTTPClient: srv.Client()})
			yc := youtube.NewChat(youtube.ChatOptions{Client: client})
			defer yc.Close()

			prov := youtube.NewProvider(youtube.WithResolver(client), youtube.WithChat(yc))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b := newChatBridge(ctx, provider.NewRegistry(prov))
			out := make(chan tea.Msg, 16)
			go b.forward(ctx, func(m tea.Msg) { out <- m })

			join(t, b, catalog.Channel{
				Platform: platform.YouTube,
				Login:    "@SomeChannel",
				URL:      "https://www.youtube.com/@SomeChannel/live",
			})

			deadline := time.After(5 * time.Second)
			for {
				select {
				case m := <-out:
					ce := m.(ui.ChatEventMsg)
					if ce.State != ui.ChatStateError {
						continue
					}
					if ce.ErrReason != tc.want {
						t.Fatalf("ErrReason = %q, want %q (errText=%q)", ce.ErrReason, tc.want, ce.ErrText)
					}
					return
				case <-deadline:
					t.Fatal("timeout waiting for ChatStateError")
				}
			}
		})
	}
}

func TestPlaybackBridgeUsesYouTubeProviderTarget(t *testing.T) {
	var started player.Target
	mgr := player.New(player.Options{
		RuntimeDir: t.TempDir(),
		StopWait:   100 * time.Millisecond,
		Command: func(target player.Target, socket string) *exec.Cmd {
			started = target
			return exec.Command(os.Args[0], "-test.run=^$")
		},
	})
	defer mgr.Close()

	ytProv := youtube.NewProvider()
	bridge := newPlaybackBridge(mgr)
	bridge.setRegistry(provider.NewRegistry(twitch.NewProvider(nil), ytProv, provider.NewURLProvider()))

	// @handle without explicit /live suffix gets canonical /@handle/live target URL.
	cmd := bridge.StartTarget(ui.PlayTarget{
		Channel: catalog.Channel{
			Platform: platform.YouTube,
			ID:       "@LofiGirl",
			Login:    "@LofiGirl",
			URL:      "https://www.youtube.com/@LofiGirl",
		},
	}, 1)
	msg := cmd().(ui.PlayerStatusMsg)
	if msg.State == ui.PlayerStateError {
		t.Fatalf("StartTarget failed: %s", msg.LastLine)
	}
	if started.URL != "https://www.youtube.com/@LofiGirl/live" {
		t.Errorf("started.URL = %q, want https://www.youtube.com/@LofiGirl/live", started.URL)
	}
	if started.Label != "youtube:@LofiGirl" {
		t.Errorf("started.Label = %q, want youtube:@LofiGirl", started.Label)
	}
}

func TestMapPlayerEventYouTubeFailures(t *testing.T) {
	for _, tc := range []struct {
		failure player.Failure
		want    ui.PlayerFailure
	}{
		{player.FailureVOD, ui.PlayerFailureVOD},
		{player.FailureBotCheck, ui.PlayerFailureBotCheck},
	} {
		msg := mapPlayerEvent(player.Event{
			Generation: 2,
			State:      player.StateError,
			Login:      "youtube:AbCdEf",
			Failure:    tc.failure,
			LastLine:   "detail line",
		})
		if msg.Failure != tc.want {
			t.Errorf("mapPlayerEvent(%q) = %q, want %q", tc.failure, msg.Failure, tc.want)
		}
	}

	// Ensure unused json/errors imports aren't introduced; check mapChatEvent directly too.
	ce := mapChatEvent(platform.YouTube, catalog.ChatEvent{
		State: catalog.ChatStateError,
		Err:   &catalog.ChatError{Reason: catalog.ChatErrorBotCheck, Err: errors.New("bot")},
	})
	if ce.ErrReason != catalog.ChatErrorBotCheck || ce.ErrText != "bot" {
		t.Fatalf("mapChatEvent = %+v", ce)
	}
}
