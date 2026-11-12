package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// chatLane is the chat of one platform as the bridge sees it. Each
// implementation numbers its own sessions; implGen is the last one the bridge
// saw and gen the number it gave that session in the sequence the UI sees.
type chatLane struct {
	platform string
	chat     catalog.Chat
	implGen  uint64
	gen      uint64
}

// chatBridge adapts the chats of every registered platform to ui.Chat and
// ui.ChatSender, so the UI never imports network/TLS logic.
//
// At most one lane is active: the one of the channel joined last. Joining a
// channel of another platform stops the lane that was active. The events of all
// lanes reach one consumer (forward), but only the active lane's get through,
// and their generations are renumbered from one counter that never goes back,
// so the UI can compare generations across platforms.
type chatBridge struct {
	ctx   context.Context
	lanes map[string]*chatLane

	mu      sync.Mutex
	nextGen uint64
	active  *chatLane
	// channel is the name of the channel the active lane joined.
	channel string
	// selected is the lane of the channel joined last, the one HasToken and
	// Username answer for. It is set when Join is called, before its Cmd runs.
	selected *chatLane

	// joins counts the Joins, Reconnects and Stops asked for. They run in
	// Update, in the order of the selections; their Cmds run in goroutines that
	// may not keep that order.
	joins atomic.Uint64
	// joining serializes those Cmds, so one that is no longer the latest
	// cannot reach a chat after the latest one did.
	joining sync.Mutex
}

// newChatBridge collects the chat of every provider in reg that has one. ctx
// bounds the Joins it starts.
func newChatBridge(ctx context.Context, reg *provider.Registry) *chatBridge {
	b := &chatBridge{ctx: ctx, lanes: make(map[string]*chatLane)}
	if reg == nil {
		return b
	}
	for _, id := range reg.IDs() {
		prov, ok := reg.Get(id)
		if !ok {
			continue
		}
		if c, ok := prov.Chat(); ok && c != nil {
			plat := platform.Normalize(id)
			b.lanes[plat] = &chatLane{platform: plat, chat: c}
		}
	}
	b.selected = b.lanes[platform.Default]
	return b
}

func (b *chatBridge) lane(ch catalog.Channel) *chatLane {
	return b.lanes[platform.Normalize(ch.Platform)]
}

func (b *chatBridge) Capabilities(ch catalog.Channel) (catalog.ChatCapabilities, bool) {
	l := b.lane(ch)
	if l == nil {
		return catalog.ChatCapabilities{}, false
	}
	return l.chat.Capabilities(), true
}

func (b *chatBridge) HasToken() bool {
	b.mu.Lock()
	l := b.selected
	b.mu.Unlock()
	return l != nil && l.chat.HasToken()
}

func (b *chatBridge) Username() string {
	b.mu.Lock()
	l := b.selected
	b.mu.Unlock()
	if l == nil {
		return ""
	}
	return l.chat.Username()
}

// mapGenLocked gives the UI generation of session implGen of l. A session newer
// than any seen starts a new UI generation; an older one is stale and has none.
// A session may show up in an event before the Join that started it returns.
func (b *chatBridge) mapGenLocked(l *chatLane, implGen uint64) (uint64, bool) {
	switch {
	case implGen == 0:
		return 0, true
	case implGen == l.implGen:
		return l.gen, true
	case implGen > l.implGen:
		b.nextGen++
		l.implGen, l.gen = implGen, b.nextGen
		return l.gen, true
	default:
		return 0, false
	}
}

func channelName(ch catalog.Channel) string {
	if ch.Login != "" {
		return ch.Login
	}
	return ch.StableID()
}

func (b *chatBridge) Join(ch catalog.Channel) tea.Cmd {
	l := b.lane(ch)
	b.mu.Lock()
	b.selected = l
	b.mu.Unlock()
	join := b.joins.Add(1)
	name := channelName(ch)
	plat := platform.Normalize(ch.Platform)
	return func() tea.Msg {
		b.joining.Lock()
		defer b.joining.Unlock()
		if b.joins.Load() != join {
			// A newer selection was made; its Join alone goes to a chat.
			return nil
		}
		if l == nil {
			return ui.ChatEventMsg{
				Platform: plat,
				Channel:  name,
				State:    ui.ChatStateError,
				ErrText:  "chat: no chat for platform " + plat,
				FromJoin: true,
			}
		}
		b.mu.Lock()
		prev := b.active
		b.active, b.channel = l, name
		b.mu.Unlock()
		if prev != nil && prev != l {
			prev.chat.Stop()
		}
		implGen, err := l.chat.Join(b.ctx, ch)
		if err != nil {
			return ui.ChatEventMsg{
				Platform:  l.platform,
				Channel:   name,
				State:     ui.ChatStateError,
				Anonymous: !l.chat.HasToken(),
				ErrText:   err.Error(),
				FromJoin:  true,
			}
		}
		b.mu.Lock()
		gen, _ := b.mapGenLocked(l, implGen)
		b.mu.Unlock()
		return ui.ChatEventMsg{
			Generation: gen,
			Platform:   l.platform,
			Channel:    name,
			State:      ui.ChatStateConnecting,
			Anonymous:  !l.chat.HasToken(),
			FromJoin:   true,
		}
	}
}

func (b *chatBridge) Reconnect() tea.Cmd {
	join := b.joins.Add(1)
	return func() tea.Msg {
		b.joining.Lock()
		defer b.joining.Unlock()
		if b.joins.Load() != join {
			return nil
		}
		b.mu.Lock()
		l, name := b.active, b.channel
		b.mu.Unlock()
		if l == nil {
			return ui.ChatEventMsg{
				State:    ui.ChatStateError,
				ErrText:  "chat: no active channel to reconnect",
				FromJoin: true,
			}
		}
		implGen, err := l.chat.Reconnect()
		if err != nil {
			return ui.ChatEventMsg{
				Platform:  l.platform,
				Channel:   name,
				State:     ui.ChatStateError,
				Anonymous: !l.chat.HasToken(),
				ErrText:   err.Error(),
				FromJoin:  true,
			}
		}
		b.mu.Lock()
		gen, _ := b.mapGenLocked(l, implGen)
		b.mu.Unlock()
		return ui.ChatEventMsg{
			Generation: gen,
			Platform:   l.platform,
			Channel:    name,
			State:      ui.ChatStateConnecting,
			Anonymous:  !l.chat.HasToken(),
			FromJoin:   true,
		}
	}
}

func (b *chatBridge) Stop() tea.Cmd {
	join := b.joins.Add(1)
	return func() tea.Msg {
		b.joining.Lock()
		defer b.joining.Unlock()
		if b.joins.Load() != join {
			// A Join came after: it stops the chat that was active by itself.
			return nil
		}
		b.mu.Lock()
		l := b.active
		b.active, b.channel = nil, ""
		var gen uint64
		if l != nil {
			gen = l.gen
		}
		b.mu.Unlock()
		msg := ui.ChatEventMsg{Generation: gen, State: ui.ChatStateDisconnected}
		if l != nil {
			l.chat.Stop()
			msg.Platform = l.platform
		}
		return msg
	}
}

func (b *chatBridge) Send(req ui.ChatSendRequest) tea.Cmd {
	return func() tea.Msg {
		b.mu.Lock()
		l, name := b.active, b.channel
		var implGen, gen uint64
		if l != nil {
			implGen, gen = l.implGen, l.gen
		}
		b.mu.Unlock()

		discard := func(reason catalog.ChatRejectReason) tea.Msg {
			upd := catalog.ChatSendStatusUpdate{Generation: req.Generation, Channel: req.Channel, Nonce: req.Nonce, Body: req.Body, Status: catalog.ChatDeliveryDiscarded, Reason: reason}
			msg := ui.ChatEventMsg{Generation: upd.Generation, Channel: upd.Channel, SendUpdates: []ui.ChatSendStatusUpdate{upd}}
			if l != nil {
				msg.Platform, msg.Anonymous = l.platform, !l.chat.HasToken()
			}
			return msg
		}
		if l == nil || req.Generation != gen {
			// The request was composed for a session that is no longer the
			// active one; no chat sees it. Same room, new session: reconnected.
			reason := catalog.ChatRejectChannelSwitched
			if l != nil && strings.EqualFold(req.Channel, name) {
				reason = catalog.ChatRejectReconnected
			}
			return discard(reason)
		}

		impl := req
		impl.Generation = implGen
		upd, err := l.chat.Send(impl)
		if err != nil && upd.Status == catalog.ChatDeliveryNone {
			return discard(catalog.ChatRejectConnectionLost)
		}
		if upd.Generation == implGen || upd.Generation == 0 {
			upd.Generation = gen
		}
		return ui.ChatEventMsg{
			Generation:  upd.Generation,
			Platform:    l.platform,
			Channel:     upd.Channel,
			Anonymous:   !l.chat.HasToken(),
			SendUpdates: []ui.ChatSendStatusUpdate{upd},
		}
	}
}

// translate turns an event of lane l into the message the UI gets, with the
// generations renumbered. It reports false for an event that must not reach the
// UI: the lane is not the active one, or the event is of a session already
// replaced.
func (b *chatBridge) translate(l *chatLane, ev catalog.ChatEvent) (ui.ChatEventMsg, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active != l {
		return ui.ChatEventMsg{}, false
	}
	gen, ok := b.mapGenLocked(l, ev.Generation)
	if !ok {
		return ui.ChatEventMsg{}, false
	}
	// remap renumbers the generation an item of the event carries.
	remap := func(g uint64) uint64 {
		if g == ev.Generation {
			return gen
		}
		return 0
	}
	msg := mapChatEvent(l.platform, ev)
	msg.Generation = gen
	if len(ev.Messages) > 0 {
		msg.Messages = make([]ui.ChatMessage, len(ev.Messages))
		for i, m := range ev.Messages {
			m.Generation = remap(m.Generation)
			msg.Messages[i] = m
		}
	}
	if len(ev.SendUpdates) > 0 {
		msg.SendUpdates = make([]ui.ChatSendStatusUpdate, len(ev.SendUpdates))
		for i, u := range ev.SendUpdates {
			u.Generation = remap(u.Generation)
			msg.SendUpdates[i] = u
		}
	}
	return msg, true
}

func mapChatEvent(plat string, ev catalog.ChatEvent) ui.ChatEventMsg {
	errText := ""
	if ev.Err != nil {
		errText = ev.Err.Error()
	}
	return ui.ChatEventMsg{
		Generation:  ev.Generation,
		Platform:    plat,
		Channel:     ev.Channel,
		State:       ui.ChatState(ev.State),
		Anonymous:   ev.Anonymous,
		Messages:    ev.Messages,
		SendUpdates: ev.SendUpdates,
		RoomState:   ev.RoomState,
		ClearChat:   ev.ClearChat,
		ClearUser:   ev.ClearUser,
		ClearMsgID:  ev.ClearMsgID,
		BanDuration: ev.BanDuration,
		ErrText:     errText,
		AuthFailed:  errors.Is(ev.Err, catalog.ErrChatAuthentication),
	}
}

// forward delivers the events of every chat to send, one consumer for all of
// them, until ctx ends. It returns when every chat's events are done.
func (b *chatBridge) forward(ctx context.Context, send func(tea.Msg)) {
	var wg sync.WaitGroup
	for _, l := range b.lanes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.forwardLane(ctx, l, send)
		}()
	}
	wg.Wait()
}

func (b *chatBridge) forwardLane(ctx context.Context, l *chatLane, send func(tea.Msg)) {
	events := l.chat.Events()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			msg, ok := b.translate(l, ev)
			if !ok {
				continue
			}
			done := make(chan struct{})
			go func() {
				send(msg)
				close(done)
			}()
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
	}
}
