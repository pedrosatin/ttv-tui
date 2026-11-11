package app

import (
	"context"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/ui"
	"net"
	"testing"
)

func TestDeliveryBeforeJoin(t *testing.T) {
	m := chat.New(chat.Options{Token: "fixture", Username: "tester", Dial: func(context.Context, string) (net.Conn, error) {
		t.Error("unexpected dial")
		return nil, context.Canceled
	}})
	defer m.Close()
	bridge := newChatBridge(m)
	_ = bridge.Join("newchannel") // The command has been requested but has not run.
	msg := bridge.Send(ui.ChatSendRequest{Channel: "newchannel", Nonce: "pending", Body: "hello"})()
	ev, ok := msg.(ui.ChatEventMsg)
	if !ok || len(ev.SendUpdates) != 1 || ev.SendUpdates[0].Status == chat.DeliveryNone || ev.SendUpdates[0].Status == chat.DeliverySending {
		t.Fatalf("send before Join has no final status: %#v", msg)
	}
}
