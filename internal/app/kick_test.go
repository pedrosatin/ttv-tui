package app

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/kick"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// scriptedSocket is a kick.Socket that plays the server side of a Pusher
// session: the handshake, then the frames it was given, one after each
// subscription.
type scriptedSocket struct {
	frames []string
	in     chan string
	closed chan struct{}
	once   sync.Once
}

func newScriptedSocket(frames []string) *scriptedSocket {
	s := &scriptedSocket{frames: frames, in: make(chan string, len(frames)+1), closed: make(chan struct{})}
	s.in <- frames[0]
	return s
}

func (s *scriptedSocket) ReadText() (string, error) {
	select {
	case text := <-s.in:
		return text, nil
	case <-s.closed:
		return "", net.ErrClosed
	}
}

// WriteText answers a subscription with the rest of the script.
func (s *scriptedSocket) WriteText(text string) error {
	if strings.Contains(text, "pusher:subscribe") {
		for _, f := range s.frames[1:] {
			s.in <- f
		}
	}
	return nil
}

func (s *scriptedSocket) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

type fixedRooms struct {
	room kick.Room
	err  error
}

func (r fixedRooms) Room(context.Context, string) (kick.Room, error) { return r.room, r.err }

type fixedResolver struct{ channels []catalog.Channel }

func (r fixedResolver) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return r.channels, nil
}

// kickFrames reads the real Pusher session kept with the kick package.
func kickFrames(tb testing.TB) []string {
	tb.Helper()
	frames, err := readKickFrames()
	if err != nil {
		tb.Fatal(err)
	}
	return frames
}

func readKickFrames() ([]string, error) {
	data, err := os.ReadFile(filepath.Join("..", "kick", "testdata", "pusher_chat.ndjson"))
	if err != nil {
		return nil, err
	}
	var frames []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			frames = append(frames, line)
		}
	}
	if len(frames) < 3 {
		return nil, errors.New("kick fixture has no chat frames")
	}
	return frames, nil
}

// offlineKickProvider is a Kick provider that never touches the network: the
// room is fixed, the chat plays frames and the live state is the one given.
func offlineKickProvider(frames []string, rooms kick.RoomFinder, live ...catalog.Channel) (*kick.Provider, *kick.Chat) {
	c := kick.NewChat(kick.ChatOptions{
		Rooms: rooms,
		Dial: func(context.Context, string) (kick.Socket, error) {
			return newScriptedSocket(frames), nil
		},
		CoalesceInterval: time.Millisecond,
	})
	return kick.NewProvider(kick.WithResolver(fixedResolver{channels: live}), kick.WithChat(c)), c
}

var kickGaules = catalog.Channel{Platform: platform.Kick, ID: "kick:gaules", Login: "gaules", DisplayName: "gaules", URL: "https://kick.com/gaules"}

// A Kick channel goes to the Kick chat through the same bridge that serves
// Twitch: the events come out tagged with the platform, in the single
// generation sequence, with the messages of the real session.
func TestChatBridgeServesTheKickChat(t *testing.T) {
	frames := kickFrames(t)
	prov, kc := offlineKickProvider(frames, fixedRooms{room: kick.Room{ChatroomID: 66973867}})
	defer kc.Close()
	tw := newFakeChatImpl(catalog.TwitchChatCapabilities)
	reg := provider.NewRegistry(fakeProvider{id: platform.Twitch, chat: tw}, prov, provider.NewURLProvider())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newChatBridge(ctx, reg)
	out := make(chan tea.Msg, 64)
	go b.forward(ctx, func(m tea.Msg) { out <- m })

	if caps, ok := b.Capabilities(kickGaules); !ok || caps.CanSend {
		t.Fatalf("Capabilities(kick) = (%+v, %v), want a read-only chat", caps, ok)
	}
	// A Twitch session first, so the Kick one cannot be generation 1 by accident.
	if ev := join(t, b, twitchFoo); ev.Generation != 1 {
		t.Fatalf("Twitch join generation = %d, want 1", ev.Generation)
	}
	ev := join(t, b, kickGaules)
	if ev.Generation != 2 || ev.Platform != platform.Kick || ev.Channel != "gaules" || !ev.Anonymous {
		t.Fatalf("Kick join = %+v, want generation 2 on kick/gaules, anonymous", ev)
	}
	if _, _, _, stops := tw.state(); stops != 1 {
		t.Fatalf("Twitch chat stopped %d times on leaving for Kick, want 1", stops)
	}
	if b.HasToken() || b.Username() != "" {
		t.Error("the bridge reports a login while the Kick chat is the selected one")
	}

	var bodies []string
	connected := false
	deadline := time.After(5 * time.Second)
	for len(bodies) < len(frames)-2 {
		select {
		case m := <-out:
			ev := m.(ui.ChatEventMsg)
			if ev.Generation != 2 || ev.Platform != platform.Kick {
				t.Fatalf("event = %+v, want generation 2 from kick", ev)
			}
			connected = connected || ev.State == ui.ChatStateConnected
			for _, msg := range ev.Messages {
				if msg.Generation != 2 {
					t.Fatalf("message generation = %d, want 2", msg.Generation)
				}
				bodies = append(bodies, msg.Body)
			}
		case <-deadline:
			t.Fatalf("got %d of %d messages (connected=%v)", len(bodies), len(frames)-2, connected)
		}
	}
	if !connected || bodies[0] != "@Viewer02 hmmm" || bodies[1] != "collectiblesGoldenW" {
		t.Fatalf("connected=%v bodies=%q", connected, bodies)
	}

	// Nothing can be sent: the request never reaches a connection.
	upd := b.Send(ui.ChatSendRequest{Generation: 2, Channel: "gaules", Nonce: "n1", Body: "oi"})().(ui.ChatEventMsg)
	if len(upd.SendUpdates) != 1 || upd.SendUpdates[0].Status == catalog.ChatDeliverySending || upd.SendUpdates[0].Status == catalog.ChatDeliveryConfirmed {
		t.Fatalf("Send on the Kick chat = %+v, want a refusal", upd.SendUpdates)
	}
}

// A failure the Kick chat explains reaches the UI with its reason, so the panel
// can say it in the user's language.
func TestChatBridgeCarriesTheKickErrorReason(t *testing.T) {
	blocked := &catalog.Error{Platform: platform.Kick, Kind: catalog.Bloqueio, StatusCode: 403, Err: errors.New("request refused by the site")}
	prov, kc := offlineKickProvider(kickFrames(t), fixedRooms{err: blocked})
	defer kc.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newChatBridge(ctx, provider.NewRegistry(prov))
	out := make(chan tea.Msg, 16)
	go b.forward(ctx, func(m tea.Msg) { out <- m })

	join(t, b, kickGaules)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-out:
			ev := m.(ui.ChatEventMsg)
			if ev.State != ui.ChatStateError {
				continue
			}
			if ev.ErrReason != catalog.ChatErrorBlocked || ev.ErrText == "" || ev.AuthFailed {
				t.Fatalf("error event = %+v, want the blocked reason with its text", ev)
			}
			return
		case <-deadline:
			t.Fatal("no error event")
		}
	}
}

func TestMapChatEvent_ReasonOnlyWhenTheErrorHasOne(t *testing.T) {
	plain := mapChatEvent(platform.Twitch, catalog.ChatEvent{State: catalog.ChatStateReconnecting, Err: io.ErrUnexpectedEOF})
	if plain.ErrReason != catalog.ChatErrorNone || plain.ErrText == "" {
		t.Errorf("plain error = %+v, want text and no reason", plain)
	}
	keyed := mapChatEvent(platform.Kick, catalog.ChatEvent{State: catalog.ChatStateError, Err: &catalog.ChatError{Reason: catalog.ChatErrorKeyRefused, Err: errors.New("pusher: error 4001")}})
	if keyed.ErrReason != catalog.ChatErrorKeyRefused || keyed.ErrText != "pusher: error 4001" {
		t.Errorf("keyed error = %+v", keyed)
	}
}

func TestKickProviderFromConfig(t *testing.T) {
	prov, stop := kickProvider(nil, config.Config{KickPusherKey: "abc"})
	if prov.ID() != platform.Kick {
		t.Fatalf("ID = %q", prov.ID())
	}
	if _, ok := prov.Resolver(); !ok {
		t.Error("the default Kick provider has no resolver")
	}
	c, ok := prov.Chat()
	if !ok || c.Capabilities().CanSend {
		t.Fatalf("the default Kick provider chat = (%v, %v), want a read-only one", c, ok)
	}
	stop()
	if _, open := <-c.Events(); open {
		t.Error("stop left the chat of the default provider open")
	}

	// An injected provider is stopped, not closed: closing is the caller's job.
	injected, kc := offlineKickProvider(kickFrames(t), fixedRooms{room: kick.Room{ChatroomID: 1}})
	defer kc.Close()
	got, stopInjected := kickProvider(injected, config.Config{})
	if got != injected {
		t.Fatal("the injected provider was replaced")
	}
	if _, err := kc.Join(context.Background(), kickGaules); err != nil {
		t.Fatalf("Join: %v", err)
	}
	stopInjected()
	if _, err := kc.Reconnect(); err == nil {
		t.Error("stop left the session of the injected chat active")
	}
	if _, err := kc.Join(context.Background(), kickGaules); err != nil {
		t.Errorf("the injected chat was closed, not stopped: %v", err)
	}
	_, stopBare := kickProvider(kick.NewProvider(), config.Config{})
	stopBare()
}

// stubSource stands for a Twitch source that is never called.
type stubSource struct{ catalog.Source }

func TestResolverOptions_OnePerPlatformWithAResolver(t *testing.T) {
	kp, kc := offlineKickProvider(kickFrames(t), fixedRooms{})
	defer kc.Close()
	tests := []struct {
		name string
		reg  *provider.Registry
		want int
	}{
		{"twitch and kick", provider.NewRegistry(twitch.NewProvider(stubSource{}), kp, provider.NewURLProvider()), 2},
		{"twitch without a source", provider.NewRegistry(twitch.NewProvider(nil), kp, provider.NewURLProvider()), 1},
		{"none", provider.NewRegistry(twitch.NewProvider(nil), kick.NewProvider()), 0},
	}
	for _, tc := range tests {
		if got := len(resolverOptions(tc.reg)); got != tc.want {
			t.Errorf("%s: %d resolver options, want %d", tc.name, got, tc.want)
		}
	}
}

// The playback bridge asks the Kick provider for the target, with the same URL
// and session label the URL provider gave before Kick had one.
func TestKickProviderKeepsThePastedURLTarget(t *testing.T) {
	kp, kc := offlineKickProvider(kickFrames(t), fixedRooms{})
	defer kc.Close()
	reg := provider.NewRegistry(twitch.NewProvider(nil), kp, provider.NewURLProvider())
	for _, ch := range []catalog.Channel{
		kickGaules,
		{Platform: platform.Kick, ID: "kick:xqc/videos/0a1b", Login: "xqc/videos/0a1b", DisplayName: "xqc", URL: "https://kick.com/xqc/videos/0a1b"},
	} {
		want, err := provider.NewURLProvider().StreamTarget(ch)
		if err != nil {
			t.Fatalf("URLProvider.StreamTarget: %v", err)
		}
		got, err := reg.StreamTarget(ch)
		if err != nil {
			t.Fatalf("Registry.StreamTarget: %v", err)
		}
		if got.URL != want.URL || got.Label != want.Label {
			t.Errorf("target of %s = %+v, want %+v", ch.Login, got, want)
		}
	}
}
