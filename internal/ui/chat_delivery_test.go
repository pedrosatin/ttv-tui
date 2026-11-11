package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"testing"
)

func TestDeliveryComposeBeforeJoin(t *testing.T) {
	m, _ := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 1)
	_ = m.startChatChannel("newchannel")
	m.chat.composeDraft = "hello"
	updated, cmd := m.submitComposeMessage()
	m = updated.(Model)
	if cmd != nil {
		t.Error("send scheduled while Join is pending")
	}
	if m.ComposeDraft() != "hello" {
		t.Error("draft lost before Join")
	}
	for _, msg := range m.ChatMessages() {
		if msg.Delivery == chat.DeliverySending {
			t.Fatal("message pending before Join")
		}
	}
}
