package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestReliableChatFailureStateAndDraft(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		m, cat := rcLive(t, lang, &fakeChat{hasToken: true, username: "tester"}, 3)
		m.width = 160
		reason := "heartbeat timeout"
		m.chatPanel.buf.Append(ChatMessage{Generation: 1, Kind: chat.MessageChat, Nonce: "pending", Body: "retry me", Self: true, Delivery: chat.DeliverySending})
		m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateReconnecting, ErrText: reason})
		m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", SendUpdates: []ChatSendStatusUpdate{{Generation: 1, Channel: "gaules", Nonce: "pending", Body: "retry me", Status: chat.DeliveryDiscarded, Reason: chat.RejectTimeout}}})
		for _, text := range []string{m.chatStatusSummary(), ansi.Strip(m.StatusBar()), ansi.Strip(strings.Join(m.renderChatLines(12, 160), "\n"))} {
			if strings.Count(text, reason) != 1 || strings.Contains(text, cat.T("chat.state.connected")) {
				t.Fatalf("contradictory status: %s", text)
			}
		}
		for _, msg := range m.ChatMessages() {
			if msg.Delivery == chat.DeliverySending {
				t.Fatal("still sending after timeout")
			}
		}
		if m.composeDraft != "retry me" {
			t.Fatal("lost failed draft")
		}
		m.composeDraft = "new draft"
		m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", SendUpdates: []ChatSendStatusUpdate{{Channel: "gaules", Body: "old draft", Status: chat.DeliveryDiscarded, Reason: chat.RejectConnectionLost}}})
		if m.composeDraft != "new draft" {
			t.Fatal("overwrote newer draft")
		}
		m = rcConnected(t, m, 1)
		if rcGapLines(m, cat) != 1 {
			t.Fatal("missing automatic reconnect gap")
		}
	}
}
