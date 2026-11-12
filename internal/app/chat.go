package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// chatBridge adapts chat.Manager to ui.Chat and ui.ChatSender without the UI
// importing network/TLS logic.
type chatBridge struct {
	mgr *chat.Manager
	// joins counts the Joins asked for. Join runs in Update, in the order of the
	// selections; its Cmd runs in a goroutine that may not keep that order.
	joins atomic.Uint64
	// joining serializes the Join Cmds, so a Join that is no longer the latest
	// cannot reach the chat after the latest one did.
	joining sync.Mutex
}

func newChatBridge(mgr *chat.Manager) *chatBridge {
	return &chatBridge{mgr: mgr}
}

func (b *chatBridge) Capabilities() catalog.ChatCapabilities {
	if b.mgr == nil {
		return catalog.ChatCapabilities{}
	}
	return b.mgr.Capabilities()
}

func (b *chatBridge) HasToken() bool {
	if b.mgr == nil {
		return false
	}
	return b.mgr.HasToken()
}

func (b *chatBridge) Username() string {
	if b.mgr == nil {
		return ""
	}
	return b.mgr.Username()
}

func (b *chatBridge) Join(login string) tea.Cmd {
	mgr := b.mgr
	join := b.joins.Add(1)
	return func() tea.Msg {
		b.joining.Lock()
		defer b.joining.Unlock()
		if b.joins.Load() != join {
			// A newer selection was made; its Join alone goes to the chat.
			return nil
		}
		gen, err := mgr.Join(login)
		if err != nil {
			return ui.ChatEventMsg{
				Generation: gen,
				Channel:    login,
				State:      ui.ChatStateError,
				Anonymous:  !mgr.HasToken(),
				ErrText:    err.Error(),
				FromJoin:   true,
			}
		}
		return ui.ChatEventMsg{
			Generation: gen,
			Channel:    mgr.Channel(),
			State:      ui.ChatStateConnecting,
			Anonymous:  !mgr.HasToken(),
			FromJoin:   true,
		}
	}
}

func (b *chatBridge) Reconnect() tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		ch := mgr.Channel()
		gen, err := mgr.Reconnect()
		if err != nil {
			return ui.ChatEventMsg{
				Generation: gen,
				Channel:    ch,
				State:      ui.ChatStateError,
				Anonymous:  !mgr.HasToken(),
				ErrText:    err.Error(),
				FromJoin:   true,
			}
		}
		return ui.ChatEventMsg{
			Generation: gen,
			Channel:    mgr.Channel(),
			State:      ui.ChatStateConnecting,
			Anonymous:  !mgr.HasToken(),
			FromJoin:   true,
		}
	}
}

func (b *chatBridge) Stop() tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		gen := mgr.Generation()
		mgr.Stop()
		return ui.ChatEventMsg{
			Generation: gen,
			State:      ui.ChatStateDisconnected,
		}
	}
}

func (b *chatBridge) Send(req ui.ChatSendRequest) tea.Cmd {
	mgr := b.mgr
	return func() tea.Msg {
		upd, err := mgr.Send(req)
		if err != nil && upd.Status == chat.DeliveryNone {
			upd = chat.SendStatusUpdate{Generation: req.Generation, Channel: req.Channel, Nonce: req.Nonce, Body: req.Body, Status: chat.DeliveryDiscarded, Reason: chat.RejectConnectionLost}
		}
		return ui.ChatEventMsg{
			Generation:  upd.Generation,
			Channel:     upd.Channel,
			Anonymous:   !mgr.HasToken(),
			SendUpdates: []ui.ChatSendStatusUpdate{upd},
		}
	}
}

func mapChatEvent(ev chat.SessionEvent) ui.ChatEventMsg {
	errText := ""
	if ev.Err != nil {
		errText = ev.Err.Error()
	}
	return ui.ChatEventMsg{
		Generation:  ev.Generation,
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
		AuthFailed:  errors.Is(ev.Err, chat.ErrAuthentication),
	}
}

func forwardChatEvents(ctx context.Context, p *tea.Program, mgr *chat.Manager) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-mgr.Events():
			if !ok {
				return
			}
			msg := mapChatEvent(ev)
			ch := make(chan struct{})
			go func() {
				p.Send(msg)
				close(ch)
			}()
			select {
			case <-ch:
			case <-ctx.Done():
				return
			}
		}
	}
}
