package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

type fakeChat struct {
	joined      []string
	reconnected int
	stopped     int
	hasToken    bool
	username    string
	sent        []ChatSendRequest
}

func (f *fakeChat) HasToken() bool {
	return f.hasToken
}

func (f *fakeChat) Username() string {
	return f.username
}

func (f *fakeChat) Join(login string) tea.Cmd {
	f.joined = append(f.joined, login)
	gen := uint64(len(f.joined))
	return func() tea.Msg {
		return ChatEventMsg{
			Generation: gen,
			Channel:    login,
			State:      ChatStateConnecting,
			Anonymous:  !f.hasToken,
			FromJoin:   true,
		}
	}
}

func (f *fakeChat) Reconnect() tea.Cmd {
	f.reconnected++
	gen := uint64(len(f.joined) + f.reconnected)
	ch := ""
	if len(f.joined) > 0 {
		ch = f.joined[len(f.joined)-1]
	}
	return func() tea.Msg {
		return ChatEventMsg{
			Generation: gen,
			Channel:    ch,
			State:      ChatStateConnecting,
			Anonymous:  !f.hasToken,
			FromJoin:   true,
		}
	}
}

func (f *fakeChat) Stop() tea.Cmd {
	f.stopped++
	return func() tea.Msg {
		return ChatEventMsg{
			State: ChatStateDisconnected,
		}
	}
}

func (f *fakeChat) Send(req ChatSendRequest) tea.Cmd {
	f.sent = append(f.sent, req)
	return func() tea.Msg {
		return ChatEventMsg{
			Generation: req.Generation,
			Channel:    req.Channel,
			SendUpdates: []ChatSendStatusUpdate{
				{
					Generation: req.Generation,
					Channel:    req.Channel,
					Nonce:      req.Nonce,
					Body:       req.Body,
					Status:     chat.DeliverySending,
				},
			},
		}
	}
}

func TestChatPanel_LifecycleMessagesHighlightAndChannelSwitch(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	fc := &fakeChat{}
	m := New(catEN, WithChat(fc))

	// 1. Select first channel "gaules" -> starts chat connecting.
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	if m.ChatState() != ChatStateConnecting || m.ChatChannel() != "gaules" {
		t.Fatalf("ChatState=%q ChatChannel=%q", m.ChatState(), m.ChatChannel())
	}
	if cmd == nil {
		t.Fatal("expected Join cmd")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.ChatGeneration() != 1 {
		t.Fatalf("ChatGeneration=%d, want 1", m.ChatGeneration())
	}

	// 2. Receive connected state, roomstate, normal + highlighted messages.
	rs := ChatRoomState{Slow: 15, FollowersOnly: 10, SubsOnly: true}
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Anonymous:  true,
		RoomState:  &rs,
		Messages: []ChatMessage{
			{
				Generation:  1,
				Kind:        chat.MessageChat,
				ID:          "m1",
				User:        "viewer1",
				DisplayName: "ViewerOne",
				Color:       "#FF5500",
				Body:        "Salve chat!",
			},
			{
				Generation:  1,
				Kind:        chat.MessageChat,
				ID:          "m2",
				User:        "vipuser",
				DisplayName: "VIPUser",
				Color:       "#00FF7F",
				MsgID:       "highlighted-message",
				Highlighted: true,
				Body:        "Mensagem em destaque!",
			},
		},
	})
	m = updated.(Model)

	if m.ChatState() != ChatStateConnected {
		t.Fatalf("ChatState=%q, want %q", m.ChatState(), ChatStateConnected)
	}
	if len(m.ChatMessages()) != 2 {
		t.Fatalf("ChatMessages len=%d, want 2", len(m.ChatMessages()))
	}
	if m.ChatRoomState().Slow != 15 || !m.ChatRoomState().SubsOnly {
		t.Fatalf("ChatRoomState=%+v", m.ChatRoomState())
	}

	view := m.View()
	for _, want := range []string{
		"Chat — #gaules",
		"Connected to #gaules (anonymous)",
		"slow 15s",
		"followers-only",
		"subs-only",
		"ViewerOne",
		"Salve chat!",
		"[highlighted]",
		"VIPUser",
		"Mensagem em destaque!",
		"ttv-tui login --from-browser",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in View(), got:\n%s", want, view)
		}
	}

	// 3. Transition to Reconnecting -> shown in UI while preserving existing messages.
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateReconnecting,
	})
	m = updated.(Model)
	if m.ChatState() != ChatStateReconnecting {
		t.Fatalf("ChatState=%q, want reconnecting", m.ChatState())
	}
	if !strings.Contains(m.View(), "Reconnecting to #gaules...") {
		t.Fatalf("expected reconnecting text in View(), got:\n%s", m.View())
	}

	// 4. Switch to a second channel "alanzoka": clears previous messages and ignores stale generation 1 events.
	updated, cmd = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{ID: "2", Login: "alanzoka", DisplayName: "alanzoka"}})
	m = updated.(Model)
	if len(m.ChatMessages()) != 0 {
		t.Fatalf("expected chat buffer cleared on channel switch, got %d messages", len(m.ChatMessages()))
	}
	if m.ChatChannel() != "alanzoka" {
		t.Fatalf("ChatChannel=%q, want alanzoka", m.ChatChannel())
	}

	// Stale message from generation 1 arrives before or after Join handshake -> must be discarded.
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, DisplayName: "OldGenUser", Body: "leaked message"},
		},
	})
	m = updated.(Model)
	if len(m.ChatMessages()) != 0 {
		t.Fatalf("stale generation message leaked before handshake: %+v", m.ChatMessages())
	}

	// Complete Join handshake for generation 2.
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.ChatGeneration() != 2 {
		t.Fatalf("ChatGeneration=%d, want 2", m.ChatGeneration())
	}

	// Another late generation 1 message after handshake -> must still be discarded.
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, DisplayName: "OldGenUser2", Body: "second leaked message"},
		},
	})
	m = updated.(Model)
	if len(m.ChatMessages()) != 0 || m.ChatChannel() != "alanzoka" {
		t.Fatalf("stale generation leaked after handshake: %+v", m.ChatMessages())
	}

	// Generation 2 message is accepted.
	updated, _ = m.Update(ChatEventMsg{
		Generation: 2,
		Channel:    "alanzoka",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{Generation: 2, Kind: chat.MessageChat, DisplayName: "AlanFan", Body: "Oi Alan!"},
		},
	})
	m = updated.(Model)
	if len(m.ChatMessages()) != 1 || m.ChatMessages()[0].Body != "Oi Alan!" {
		t.Fatalf("ChatMessages=%+v, want Oi Alan!", m.ChatMessages())
	}
}

func TestChatCompose_KeysDraftPreservationEmptySendAndDeliveryLifecycle(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	fc := &fakeChat{
		hasToken: true,
		username: "pedrosatin",
	}
	m := New(catEN, WithChat(fc))

	// Join #gaules (generation 1).
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Anonymous:  false,
	})
	m = updated.(Model)

	// Press 'i' to enter compose mode.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	if !m.Composing() {
		t.Fatal("expected Composing() = true after pressing 'i'")
	}

	// Empty message on Enter must NOT send anything.
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil || len(fc.sent) != 0 || len(m.ChatMessages()) != 0 {
		t.Fatalf("empty Enter must not send: cmd=%v sent=%d msgs=%d", cmd, len(fc.sent), len(m.ChatMessages()))
	}

	// Global keys ('j', 'k', Space, '+', '-') become literal text in compose mode.
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'j'}},
		{Type: tea.KeyRunes, Runes: []rune{'k'}},
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune{'+'}},
		{Type: tea.KeyRunes, Runes: []rune{'-'}},
	} {
		updated, _ = m.Update(k)
		m = updated.(Model)
	}
	if m.ComposeDraft() != "jk +-" {
		t.Fatalf("ComposeDraft() = %q, want %q", m.ComposeDraft(), "jk +-")
	}

	// Esc exits compose mode while preserving the draft; switching panel/mode also preserves draft.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Composing() {
		t.Fatal("expected Composing() = false after Esc")
	}
	if m.ComposeDraft() != "jk +-" {
		t.Fatalf("draft lost after Esc: %q", m.ComposeDraft())
	}

	// Re-enter compose mode with 'i', press Tab (leave panel) -> draft still preserved.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.Composing() || m.ComposeDraft() != "jk +-" {
		t.Fatalf("after Tab: Composing=%v Draft=%q", m.Composing(), m.ComposeDraft())
	}

	// Re-enter compose mode and press Enter to send.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected Send cmd on Enter")
	}
	if m.ComposeDraft() != "" {
		t.Fatalf("expected draft cleared after Enter, got %q", m.ComposeDraft())
	}
	if len(fc.sent) != 1 || fc.sent[0].Body != "jk +-" || fc.sent[0].Channel != "gaules" || fc.sent[0].Generation != 1 {
		t.Fatalf("sent request mismatch: %+v", fc.sent)
	}

	// Message immediately appears as [sending] in the buffer and View.
	msgs := m.ChatMessages()
	if len(msgs) != 1 || msgs[0].Delivery != chat.DeliverySending {
		t.Fatalf("expected 1 sending message, got %+v", msgs)
	}
	if !strings.Contains(m.View(), "[sending]") {
		t.Fatalf("expected [sending] in View():\n%s", m.View())
	}

	// Server confirms delivery -> badge updates to [sent].
	nonce := fc.sent[0].Nonce
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		SendUpdates: []ChatSendStatusUpdate{
			{
				Generation: 1,
				Channel:    "gaules",
				Nonce:      nonce,
				Status:     chat.DeliveryConfirmed,
			},
		},
	})
	m = updated.(Model)
	if m.ChatMessages()[0].Delivery != chat.DeliveryConfirmed {
		t.Fatalf("expected DeliveryConfirmed, got %q", m.ChatMessages()[0].Delivery)
	}
	if !strings.Contains(m.View(), "[sent]") {
		t.Fatalf("expected [sent] in View():\n%s", m.View())
	}

	// Send a second message that gets rejected by slow mode NOTICE -> shows [rejected: slow mode].
	for _, r := range "msg2" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	_ = cmd()
	nonce2 := fc.sent[1].Nonce

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		SendUpdates: []ChatSendStatusUpdate{
			{
				Generation: 1,
				Channel:    "gaules",
				Nonce:      nonce2,
				Status:     chat.DeliveryRejected,
				Reason:     chat.RejectSlowMode,
			},
		},
	})
	m = updated.(Model)
	if !strings.Contains(m.View(), "[rejected: slow mode]") {
		t.Fatalf("expected [rejected: slow mode] in View():\n%s", m.View())
	}

	// Rate-limit discard informs the UI with a visible warning and marks the message [discarded: ...].
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		SendUpdates: []ChatSendStatusUpdate{
			{
				Generation: 1,
				Channel:    "gaules",
				Nonce:      nonce2,
				Status:     chat.DeliveryDiscarded,
				Reason:     chat.RejectRateLimited,
			},
		},
	})
	m = updated.(Model)
	if m.ChatSendWarning() == "" || !strings.Contains(m.View(), "Rate limit reached") {
		t.Fatalf("expected rate-limit warning in View():\n%s", m.View())
	}
}

func TestChatCompose_NoTokenBlocksSendWithLoginInstruction(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	fc := &fakeChat{hasToken: false}
	m := New(catEN, WithChat(fc))

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// Input line already shows the login instruction.
	if !strings.Contains(m.View(), "ttv-tui login --from-browser") {
		t.Fatalf("expected login instruction in View():\n%s", m.View())
	}

	// Pressing 'i' without a token does not enter compose mode and surfaces the login warning.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(Model)
	if m.Composing() {
		t.Fatal("expected Composing() = false when no token is configured")
	}
	if !strings.Contains(m.ChatSendWarning(), "ttv-tui login --from-browser") {
		t.Fatalf("expected login instruction in ChatSendWarning(), got %q", m.ChatSendWarning())
	}
}
