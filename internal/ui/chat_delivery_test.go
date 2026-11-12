package ui

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
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
		if msg.Delivery == catalog.ChatDeliverySending {
			t.Fatal("message pending before Join")
		}
	}
}
