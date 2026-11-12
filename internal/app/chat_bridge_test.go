package app

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// newTwitchChatBridge returns a bridge whose only chat is mgr, registered the
// way Run registers the Twitch one.
func newTwitchChatBridge(t *testing.T, mgr *chat.Manager) *chatBridge {
	t.Helper()
	reg := provider.NewRegistry(twitch.NewProvider(nil, twitch.WithChat(mgr)))
	return newChatBridge(context.Background(), reg)
}

// testOnlyPlatform is the platform of the read-only fake chat.
const testOnlyPlatform = "testonly"

// fakeChatImpl is a catalog.Chat that numbers its own sessions from 1, like
// every real implementation does, and records what the bridge asks of it.
type fakeChatImpl struct {
	caps   catalog.ChatCapabilities
	token  bool
	events chan catalog.ChatEvent

	mu         sync.Mutex
	gen        uint64
	joined     []catalog.Channel
	sent       []catalog.ChatSendRequest
	stops      int
	reconnects int
	// onJoin runs inside Join, before it returns.
	onJoin func(f *fakeChatImpl, gen uint64)
}

func newFakeChatImpl(caps catalog.ChatCapabilities) *fakeChatImpl {
	return &fakeChatImpl{caps: caps, events: make(chan catalog.ChatEvent, 16)}
}

func (f *fakeChatImpl) Capabilities() catalog.ChatCapabilities { return f.caps }

func (f *fakeChatImpl) Join(_ context.Context, ch catalog.Channel) (uint64, error) {
	f.mu.Lock()
	f.gen++
	gen := f.gen
	f.joined = append(f.joined, ch)
	onJoin := f.onJoin
	f.mu.Unlock()
	if onJoin != nil {
		onJoin(f, gen)
	}
	return gen, nil
}

func (f *fakeChatImpl) Reconnect() (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	f.reconnects++
	return f.gen, nil
}

func (f *fakeChatImpl) Stop() {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
}

func (f *fakeChatImpl) Send(req catalog.ChatSendRequest) (catalog.ChatSendStatusUpdate, error) {
	f.mu.Lock()
	f.sent = append(f.sent, req)
	f.mu.Unlock()
	return catalog.ChatSendStatusUpdate{Generation: req.Generation, Channel: req.Channel, Nonce: req.Nonce, Body: req.Body, Status: catalog.ChatDeliverySending}, nil
}

func (f *fakeChatImpl) Events() <-chan catalog.ChatEvent { return f.events }
func (f *fakeChatImpl) Close()                           {}
func (f *fakeChatImpl) HasToken() bool                   { return f.token }
func (f *fakeChatImpl) Username() string                 { return "" }

func (f *fakeChatImpl) state() (gen uint64, joined []catalog.Channel, sent []catalog.ChatSendRequest, stops int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gen, append([]catalog.Channel(nil), f.joined...), append([]catalog.ChatSendRequest(nil), f.sent...), f.stops
}

// fakeProvider is a provider with nothing but a chat.
type fakeProvider struct {
	id   string
	chat catalog.Chat
}

func (p fakeProvider) ID() string                         { return p.id }
func (p fakeProvider) Browser() (catalog.Browser, bool)   { return nil, false }
func (p fakeProvider) Searcher() (catalog.Searcher, bool) { return nil, false }
func (p fakeProvider) Resolver() (catalog.Resolver, bool) { return nil, false }
func (p fakeProvider) Chat() (catalog.Chat, bool)         { return p.chat, p.chat != nil }
func (p fakeProvider) StreamTarget(catalog.Channel) (catalog.Stream, error) {
	return catalog.Stream{}, nil
}

// bridgeFixture wires a Twitch fake and a read-only fake behind one bridge
// whose merged events are collected in out.
type bridgeFixture struct {
	bridge   *chatBridge
	twitch   *fakeChatImpl
	readOnly *fakeChatImpl
	out      chan tea.Msg
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	f := &bridgeFixture{
		twitch:   newFakeChatImpl(catalog.TwitchChatCapabilities),
		readOnly: newFakeChatImpl(catalog.ReadOnlyChatCapabilities),
		out:      make(chan tea.Msg, 64),
	}
	f.twitch.token = true
	reg := provider.NewRegistry(
		fakeProvider{id: platform.Twitch, chat: f.twitch},
		fakeProvider{id: testOnlyPlatform, chat: f.readOnly},
		fakeProvider{id: platform.YouTube},
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.bridge = newChatBridge(ctx, reg)
	done := make(chan struct{})
	go func() {
		f.bridge.forward(ctx, func(m tea.Msg) { f.out <- m })
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	return f
}

func (f *bridgeFixture) next(t *testing.T) ui.ChatEventMsg {
	t.Helper()
	select {
	case m := <-f.out:
		return m.(ui.ChatEventMsg)
	case <-time.After(2 * time.Second):
		t.Fatal("no event reached the consumer")
		return ui.ChatEventMsg{}
	}
}

// none fails when an event reaches the consumer within a short while. The
// events of the chat are delivered in order, so an event pushed after the one
// that must be dropped is the marker that the dropped one was looked at.
func (f *bridgeFixture) none(t *testing.T) {
	t.Helper()
	select {
	case m := <-f.out:
		t.Fatalf("unexpected event reached the consumer: %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
}

func join(t *testing.T, b *chatBridge, ch catalog.Channel) ui.ChatEventMsg {
	t.Helper()
	msg := b.Join(ch)()
	ev, ok := msg.(ui.ChatEventMsg)
	if !ok {
		t.Fatalf("Join returned %T", msg)
	}
	return ev
}

var (
	twitchFoo   = catalog.Channel{Platform: platform.Twitch, Login: "foo"}
	readOnlyFoo = catalog.Channel{Platform: testOnlyPlatform, Login: "foo", ID: "Room-42", URL: "https://example.test/foo"}
)

// Twitch -> read-only platform -> Twitch. Each chat numbers its sessions from
// 1; the bridge must give the UI one growing sequence, drop what the chats that
// are not active emit, and stop the one it leaves.
func TestChatBridgeOneGenerationAcrossPlatforms(t *testing.T) {
	f := newBridgeFixture(t)

	ev := join(t, f.bridge, twitchFoo)
	g1 := ev.Generation
	if g1 == 0 || ev.Platform != platform.Twitch || !ev.FromJoin {
		t.Fatalf("Join of Twitch = %+v", ev)
	}
	f.twitch.events <- catalog.ChatEvent{Generation: 1, Channel: "foo", State: catalog.ChatStateConnected}
	if got := f.next(t); got.Generation != g1 || got.Platform != platform.Twitch {
		t.Fatalf("Twitch event = gen %d platform %q, want gen %d", got.Generation, got.Platform, g1)
	}

	ev = join(t, f.bridge, readOnlyFoo)
	g2 := ev.Generation
	if g2 <= g1 || ev.Platform != testOnlyPlatform {
		t.Fatalf("read-only Join gen %d (platform %q), want above %d", g2, ev.Platform, g1)
	}
	if _, _, _, stops := f.twitch.state(); stops != 1 {
		t.Fatalf("Twitch chat stopped %d times on leaving, want 1", stops)
	}

	// A late event of the Twitch session, then one of the read-only chat.
	f.twitch.events <- catalog.ChatEvent{Generation: 1, Channel: "foo", Messages: []catalog.ChatMessage{{Generation: 1, Body: "late"}}}
	f.readOnly.events <- catalog.ChatEvent{Generation: 1, Channel: "foo", State: catalog.ChatStateConnected, Messages: []catalog.ChatMessage{{Generation: 1, Body: "now"}}}
	got := f.next(t)
	if got.Platform != testOnlyPlatform || got.Generation != g2 || len(got.Messages) != 1 || got.Messages[0].Body != "now" || got.Messages[0].Generation != g2 {
		t.Fatalf("event after the switch = %+v, want the read-only one renumbered to %d", got, g2)
	}
	f.none(t)

	ev = join(t, f.bridge, twitchFoo)
	g3 := ev.Generation
	if g3 <= g2 {
		t.Fatalf("second Twitch session got gen %d, want above %d", g3, g2)
	}
	if _, _, _, stops := f.readOnly.state(); stops != 1 {
		t.Fatalf("read-only chat stopped %d times on leaving, want 1", stops)
	}
	f.readOnly.events <- catalog.ChatEvent{Generation: 1, Channel: "foo", Messages: []catalog.ChatMessage{{Generation: 1, Body: "late read-only"}}}
	f.twitch.events <- catalog.ChatEvent{Generation: 2, Channel: "foo", State: catalog.ChatStateConnected}
	if got := f.next(t); got.Platform != platform.Twitch || got.Generation != g3 {
		t.Fatalf("event after returning = %+v, want Twitch gen %d", got, g3)
	}
	// The first Twitch session is over: its events carry an old number and go.
	f.twitch.events <- catalog.ChatEvent{Generation: 1, Channel: "foo", Messages: []catalog.ChatMessage{{Generation: 1, Body: "stale"}}}
	f.none(t)
}

// A session can emit before its Join returns; the Join must answer with the
// number the early event already got.
func TestChatBridgeEventBeforeJoinReturnsKeepsItsGeneration(t *testing.T) {
	f := newBridgeFixture(t)
	f.twitch.onJoin = func(f *fakeChatImpl, gen uint64) {
		f.events <- catalog.ChatEvent{Generation: gen, Channel: "foo", State: catalog.ChatStateConnecting}
	}
	ev := join(t, f.bridge, twitchFoo)
	early := f.next(t)
	if early.Generation == 0 || early.Generation != ev.Generation {
		t.Fatalf("early event gen %d, Join gen %d: they are one session", early.Generation, ev.Generation)
	}
}

func TestChatBridgeCapabilitiesComeByChannel(t *testing.T) {
	f := newBridgeFixture(t)
	if caps, ok := f.bridge.Capabilities(twitchFoo); !ok || !caps.CanSend {
		t.Fatalf("Twitch caps = %+v, %v", caps, ok)
	}
	if caps, ok := f.bridge.Capabilities(readOnlyFoo); !ok || caps.CanSend {
		t.Fatalf("read-only caps = %+v, %v", caps, ok)
	}
	// Registered, but without a chat; and not registered at all.
	if _, ok := f.bridge.Capabilities(catalog.Channel{Platform: platform.YouTube, ID: "x"}); ok {
		t.Fatal("a provider without a chat must report none")
	}
	if _, ok := f.bridge.Capabilities(catalog.Channel{Platform: "nowhere", ID: "x"}); ok {
		t.Fatal("an unregistered platform must report no chat")
	}
}

// Join hands the implementation the channel whole, case and all.
func TestChatBridgeJoinPassesTheChannelIntact(t *testing.T) {
	f := newBridgeFixture(t)
	room := catalog.Channel{Platform: testOnlyPlatform, ID: "AbC-dEf_123", URL: "https://example.test/watch?v=AbC-dEf_123"}
	ev := join(t, f.bridge, room)
	if ev.Channel != "AbC-dEf_123" {
		t.Fatalf("Join event channel = %q, want the identifier as is", ev.Channel)
	}
	if _, joined, _, _ := f.readOnly.state(); len(joined) != 1 || joined[0] != room {
		t.Fatalf("implementation joined %+v, want %+v", joined, room)
	}
}

func TestChatBridgeSendGoesToActiveChatWithItsGeneration(t *testing.T) {
	f := newBridgeFixture(t)
	g1 := join(t, f.bridge, twitchFoo).Generation

	// Twitch numbers its second session 2; the UI holds the bridge's number.
	g2 := join(t, f.bridge, twitchFoo).Generation
	if g2 <= g1 {
		t.Fatalf("rejoin gen %d, want above %d", g2, g1)
	}

	req := ui.ChatSendRequest{Generation: g2, Channel: "foo", Nonce: "n1", Body: "hi"}
	ev := f.bridge.Send(req)().(ui.ChatEventMsg)
	_, _, sent, _ := f.twitch.state()
	if len(sent) != 1 || sent[0].Generation != 2 {
		t.Fatalf("Twitch chat received %+v, want its own generation 2", sent)
	}
	if len(ev.SendUpdates) != 1 || ev.SendUpdates[0].Generation != g2 || ev.SendUpdates[0].Status != catalog.ChatDeliverySending {
		t.Fatalf("send update = %+v, want the UI generation %d", ev.SendUpdates, g2)
	}

	// A request of the first session is discarded without reaching any chat.
	ev = f.bridge.Send(ui.ChatSendRequest{Generation: g1, Channel: "foo", Nonce: "n2", Body: "old"})().(ui.ChatEventMsg)
	if len(ev.SendUpdates) != 1 || ev.SendUpdates[0].Status != catalog.ChatDeliveryDiscarded || ev.SendUpdates[0].Reason != catalog.ChatRejectReconnected {
		t.Fatalf("stale send = %+v, want discarded as reconnected", ev.SendUpdates)
	}
	ev = f.bridge.Send(ui.ChatSendRequest{Generation: g1, Channel: "other", Nonce: "n3", Body: "old"})().(ui.ChatEventMsg)
	if ev.SendUpdates[0].Reason != catalog.ChatRejectChannelSwitched {
		t.Fatalf("send for another room = %+v, want channel_switched", ev.SendUpdates)
	}
	if _, _, sent, _ = f.twitch.state(); len(sent) != 1 {
		t.Fatalf("stale requests reached the chat: %+v", sent)
	}
}

func TestChatBridgeReconnectAndStopGoToActiveChat(t *testing.T) {
	f := newBridgeFixture(t)
	join(t, f.bridge, readOnlyFoo)
	g := f.bridge.Reconnect()().(ui.ChatEventMsg)
	if f.readOnly.reconnects != 1 || f.twitch.reconnects != 0 {
		t.Fatalf("reconnects: read-only %d, twitch %d", f.readOnly.reconnects, f.twitch.reconnects)
	}
	if g.Platform != testOnlyPlatform || g.Generation == 0 || g.Channel != "foo" {
		t.Fatalf("Reconnect event = %+v", g)
	}

	stop := f.bridge.Stop()().(ui.ChatEventMsg)
	if _, _, _, stops := f.readOnly.state(); stops != 1 {
		t.Fatalf("read-only chat stopped %d times, want 1", stops)
	}
	if stop.State != ui.ChatStateDisconnected || stop.Generation != g.Generation {
		t.Fatalf("Stop event = %+v", stop)
	}
	// After Stop nothing from the chat reaches the UI.
	f.readOnly.events <- catalog.ChatEvent{Generation: 2, Channel: "foo", Messages: []catalog.ChatMessage{{Body: "after stop"}}}
	f.none(t)
}

// A Stop that was asked for before a Join but runs after it must not take the
// new chat down.
func TestChatBridgeStopBeforeJoinLeavesTheJoinedChat(t *testing.T) {
	f := newBridgeFixture(t)
	stop := f.bridge.Stop()
	joinCmd := f.bridge.Join(twitchFoo)
	_ = joinCmd()
	_ = stop()
	if _, _, _, stops := f.twitch.state(); stops != 0 {
		t.Fatalf("the late Stop stopped the new chat (%d stops)", stops)
	}
}

func TestChatBridgeHasTokenFollowsTheSelectedChat(t *testing.T) {
	f := newBridgeFixture(t)
	if !f.bridge.HasToken() {
		t.Fatal("before any Join the bridge answers for the default platform")
	}
	_ = f.bridge.Join(readOnlyFoo)
	if f.bridge.HasToken() {
		t.Fatal("the read-only chat has no token")
	}
	_ = f.bridge.Join(twitchFoo)
	if !f.bridge.HasToken() {
		t.Fatal("the Twitch chat has a token")
	}
}
