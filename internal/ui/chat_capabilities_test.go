package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestChatReadOnlyPlatform_DisplaysMessagesAndDisablesInput(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	caps := catalog.ReadOnlyChatCapabilities
	fc := &fakeChat{
		hasToken: true,
		username: "tester",
		caps:     &caps,
	}

	m := New(catEN, WithChat(fc))
	m = resize(m, 120, 24)

	// Start channel on a read-only platform.
	if cmd := m.startChatChannel(catalog.Channel{Login: "streamer"}); cmd != nil {
		updated, _ := m.Update(cmd())
		m = updated.(Model)
	}

	if m.ChatCapabilities().CanSend {
		t.Fatal("expected CanSend to be false for read-only platform")
	}

	// Deliver messages to the chat panel.
	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "streamer",
		State:      ChatStateConnected,
		Messages: []catalog.ChatMessage{
			{
				Generation:  1,
				Kind:        catalog.ChatMessageChat,
				ReceivedAt:  time.Now(),
				User:        "viewer1",
				DisplayName: "Viewer1",
				Body:        "hello from read only platform",
			},
		},
	})
	m = updated.(Model)

	msgs := m.ChatMessages()
	if len(msgs) != 1 || msgs[0].Body != "hello from read only platform" {
		t.Fatalf("unexpected chat messages: %+v", msgs)
	}

	// Verify footer shows "Unavailable for this platform" instead of input / ready prompt.
	inputLine := m.renderChatInputLine(80)
	wantHint := catEN.T("chat.input.unsupported")
	if !strings.Contains(inputLine, wantHint) {
		t.Fatalf("input line %q does not contain %q", inputLine, wantHint)
	}

	// Attempting to enter compose mode via enterCompose or submit must fail.
	mUpdated, cmd := m.enterCompose()
	m = mUpdated.(Model)
	if m.Composing() {
		t.Fatal("enterCompose must not activate composing on read-only platform")
	}
	if cmd != nil {
		t.Fatal("enterCompose returned unexpected cmd")
	}

	m.chat.composeDraft = "should not send"
	mUpdated, sendCmd := m.submitComposeMessage()
	m = mUpdated.(Model)
	if sendCmd != nil {
		t.Fatal("submitComposeMessage returned non-nil cmd on read-only platform")
	}
	if len(fc.sent) != 0 {
		t.Fatalf("read-only chat sent a message: %+v", fc.sent)
	}
}

func TestChatCapabilities_SlowModeTagOmittedWhenUnsupported(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	capsNoSlow := catalog.ChatCapabilities{
		CanSend:          false,
		SupportsSlowMode: false,
	}
	fc := &fakeChat{caps: &capsNoSlow}
	m := New(catEN, WithChat(fc))
	// Capabilities arrive with the Join of a channel.
	m.startChatChannel(catalog.Channel{Login: "ch"})
	m.chat.panel.roomState = ChatRoomState{Slow: 30}

	summary := m.chatRoomModesSummary()
	if strings.Contains(summary, "slow") || strings.Contains(summary, "30") {
		t.Fatalf("slow mode summary shown when capability is unsupported: %q", summary)
	}

	// Enable slow mode capability.
	m.chat.panel.caps.SupportsSlowMode = true
	summary = m.chatRoomModesSummary()
	if !strings.Contains(summary, "slow 30s") {
		t.Fatalf("slow mode summary missing when capability is supported: %q", summary)
	}
}
