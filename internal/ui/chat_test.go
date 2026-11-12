package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

type fakeChat struct {
	joined []string
	// joinedChannels holds the channels exactly as Join received them.
	joinedChannels []catalog.Channel
	// platforms declares the chats served besides Twitch, by platform id. A
	// platform that is neither Twitch nor listed has no chat.
	platforms   map[string]catalog.ChatCapabilities
	reconnected int
	stopped     int
	hasToken    bool
	username    string
	caps        *catalog.ChatCapabilities
	sent        []ChatSendRequest
}

func (f *fakeChat) Capabilities(ch catalog.Channel) (catalog.ChatCapabilities, bool) {
	plat := platform.Normalize(ch.Platform)
	if caps, ok := f.platforms[plat]; ok {
		return caps, true
	}
	if plat != platform.Twitch {
		return catalog.ChatCapabilities{}, false
	}
	if f.caps != nil {
		return *f.caps, true
	}
	return catalog.TwitchChatCapabilities, true
}

func (f *fakeChat) HasToken() bool {
	return f.hasToken
}

func (f *fakeChat) Username() string {
	return f.username
}

func (f *fakeChat) Join(ch catalog.Channel) tea.Cmd {
	login := chatChannelName(ch)
	f.joined = append(f.joined, login)
	f.joinedChannels = append(f.joinedChannels, ch)
	gen := uint64(len(f.joined))
	plat := platform.Normalize(ch.Platform)
	return func() tea.Msg {
		return ChatEventMsg{
			Generation: gen,
			Platform:   plat,
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
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
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
	rs := ChatRoomState{Slow: 15, FollowersOnly: 10, SubsOnly: true, EmoteOnly: true, R9K: true}
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
	if m.ChatRoomState().Slow != 15 || !m.ChatRoomState().SubsOnly || !m.ChatRoomState().EmoteOnly || !m.ChatRoomState().R9K {
		t.Fatalf("ChatRoomState=%+v", m.ChatRoomState())
	}

	view := m.View()
	for _, want := range []string{
		"Chat — #gaules",
		"Connected to #gaules (anonymous)",
		"slow 15s",
		"followers-only",
		"subs-only",
		"emote-only",
		"unique-chat",
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

	catPT, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New(pt-BR): %v", err)
	}
	mPT := m
	mPT.catalog = catPT
	viewPT := mPT.View()
	for _, want := range []string{
		"lento 15s",
		"só seguidores",
		"só inscritos",
		"só emotes",
		"chat único",
	} {
		if !strings.Contains(viewPT, want) {
			t.Fatalf("expected %q in pt-BR View(), got:\n%s", want, viewPT)
		}
	}

	// In 80 columns with 5 modes active, the layout must not break.
	m80 := resize(m, 80, 20)
	assertAllLinesFit(t, m80.View(), 80)

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
	updated, cmd = m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "2", Login: "alanzoka", DisplayName: "alanzoka"}})
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
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
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

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
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

func joinChatFixture(t *testing.T, lang string) Model {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New(%s): %v", lang, err)
	}
	fc := &fakeChat{}
	m := New(cat, WithChat(fc))
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Anonymous:  true,
	})
	return updated.(Model)
}

func TestChatModeration_ClearChatTimeoutBanAndClearMsg(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)

	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "msg-keep", User: "alice", DisplayName: "Alice", Body: "hello"},
			{Generation: 1, Kind: chat.MessageChat, ID: "msg-gone", User: "baduser", DisplayName: "BadUser", Body: "spam"},
			{Generation: 1, Kind: chat.MessageChat, ID: "msg-other", User: "bob", DisplayName: "Bob", Body: "noise"},
		},
	})
	m = updated.(Model)

	timeout := 600
	updated, _ = m.Update(ChatEventMsg{
		Generation:  1,
		Channel:     "gaules",
		ClearChat:   true,
		ClearUser:   "baduser",
		BanDuration: &timeout,
	})
	m = updated.(Model)

	msgs := m.ChatMessages()
	for _, msg := range msgs {
		if strings.EqualFold(msg.User, "baduser") && msg.Kind == chat.MessageChat && !msg.Deleted {
			t.Fatalf("baduser chat message not marked: %+v", msg)
		}
	}
	foundTimeout := false
	for _, msg := range msgs {
		if msg.Kind == chat.MessageSystem && strings.Contains(msg.Body, "baduser timed out for 10m") {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		t.Fatalf("missing timeout system line in %+v", msgs)
	}
	if !strings.Contains(m.View(), "baduser timed out for 10m") {
		t.Fatalf("timeout system line missing from View():\n%s", m.View())
	}

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		ClearChat:  true,
		ClearUser:  "bob",
	})
	m = updated.(Model)
	foundBan := false
	for _, msg := range m.ChatMessages() {
		if msg.Kind == chat.MessageSystem && strings.Contains(msg.Body, "bob was permanently banned") {
			foundBan = true
			break
		}
	}
	if !foundBan {
		t.Fatalf("missing permanent ban system line in %+v", m.ChatMessages())
	}

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "to-delete", User: "carol", DisplayName: "Carol", Body: "bye"},
		},
	})
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		ClearUser:  "carol",
		ClearMsgID: "to-delete",
	})
	m = updated.(Model)
	for _, msg := range m.ChatMessages() {
		if msg.ID == "to-delete" && !msg.Deleted {
			t.Fatalf("deleted message not marked: %+v", msg)
		}
	}
	foundDeleted := false
	for _, msg := range m.ChatMessages() {
		if msg.Kind == chat.MessageSystem && strings.Contains(msg.Body, "Message from carol deleted") {
			foundDeleted = true
			break
		}
	}
	if !foundDeleted {
		t.Fatalf("missing clearmsg system line in %+v", m.ChatMessages())
	}

	// Message already outside the buffer must not break; still leave a system line.
	before := len(m.ChatMessages())
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		ClearUser:  "ghost",
		ClearMsgID: "already-gone",
	})
	m = updated.(Model)
	if len(m.ChatMessages()) != before+1 {
		t.Fatalf("expected one system line after missing ClearMsg, Len %d -> %d", before, len(m.ChatMessages()))
	}
	last := m.ChatMessages()[len(m.ChatMessages())-1]
	if last.Kind != chat.MessageSystem || !strings.Contains(last.Body, "Message from ghost deleted") {
		t.Fatalf("unexpected last message after missing ClearMsg: %+v", last)
	}
}

func TestChatModeration_I18nENAndPTBR(t *testing.T) {
	cases := []struct {
		lang         string
		timeoutBody  string
		banBody      string
		deletedBody  string
		systemPrefix string
	}{
		{
			lang:         i18n.LangEN,
			timeoutBody:  "spammer timed out for 30s",
			banBody:      "spammer was permanently banned",
			deletedBody:  "Message from spammer deleted",
			systemPrefix: "[mod]",
		},
		{
			lang:         i18n.LangPTBR,
			timeoutBody:  "spammer silenciado por 30s",
			banBody:      "spammer foi banido permanentemente",
			deletedBody:  "Mensagem de spammer apagada",
			systemPrefix: "[mod]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			m := joinChatFixture(t, tc.lang)
			secs := 30
			updated, _ := m.Update(ChatEventMsg{
				Generation:  1,
				Channel:     "gaules",
				ClearChat:   true,
				ClearUser:   "spammer",
				BanDuration: &secs,
			})
			m = updated.(Model)
			updated, _ = m.Update(ChatEventMsg{
				Generation: 1,
				Channel:    "gaules",
				ClearChat:  true,
				ClearUser:  "spammer",
			})
			m = updated.(Model)
			updated, _ = m.Update(ChatEventMsg{
				Generation: 1,
				Channel:    "gaules",
				ClearUser:  "spammer",
				ClearMsgID: "x",
			})
			m = updated.(Model)

			view := m.View()
			for _, want := range []string{tc.timeoutBody, tc.banBody, tc.deletedBody, tc.systemPrefix} {
				if !strings.Contains(view, want) {
					t.Fatalf("View() missing %q:\n%s", want, view)
				}
			}
			bodies := map[string]bool{}
			for _, msg := range m.ChatMessages() {
				if msg.Kind == chat.MessageSystem {
					bodies[msg.Body] = true
				}
			}
			for _, want := range []string{tc.timeoutBody, tc.banBody, tc.deletedBody} {
				if !bodies[want] {
					t.Fatalf("system bodies=%v missing %q", bodies, want)
				}
			}
		})
	}
}

// TestChatModeration_PTYEvidence logs the rendered chat panel with timeout and
// delete system lines so a PTY/script capture can show both markers.
func TestChatModeration_PTYEvidence(t *testing.T) {
	m := joinChatFixture(t, i18n.LangPTBR)
	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "m1", User: "alice", DisplayName: "Alice", Body: "oi"},
			{Generation: 1, Kind: chat.MessageChat, ID: "m2", User: "troll", DisplayName: "Troll", Body: "spam"},
		},
	})
	m = updated.(Model)

	secs := 120
	updated, _ = m.Update(ChatEventMsg{
		Generation:  1,
		Channel:     "gaules",
		ClearChat:   true,
		ClearUser:   "troll",
		BanDuration: &secs,
	})
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "m3", User: "bob", DisplayName: "Bob", Body: "depois"},
		},
	})
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		ClearUser:  "bob",
		ClearMsgID: "m3",
	})
	m = updated.(Model)

	view := m.View()
	t.Logf("\n================================================================================\nCAPTURA PTY: linhas de sistema CLEARCHAT/CLEARMSG\n--------------------------------------------------------------------------------\n%s\n================================================================================", view)

	if !strings.Contains(view, "troll silenciado por 2m") {
		t.Fatalf("PTY capture missing timeout line:\n%s", view)
	}
	if !strings.Contains(view, "Mensagem de bob apagada") {
		t.Fatalf("PTY capture missing delete line:\n%s", view)
	}
	if !strings.Contains(view, "[mod]") {
		t.Fatalf("PTY capture missing [mod] prefix:\n%s", view)
	}
}

func TestChatUserNotice_ResubWithAndWithoutText(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)

	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "m1", User: "alice", DisplayName: "Alice", Body: "hello"},
			{
				Generation:  1,
				Kind:        chat.MessageSystem,
				ID:          "resub-1",
				User:        "coolsub",
				DisplayName: "CoolSub",
				MsgID:       "resub",
				UserNotice:  true,
				Months:      15,
				Body:        "Great stream -- keep it up!",
			},
			{
				Generation:  1,
				Kind:        chat.MessageSystem,
				ID:          "resub-2",
				User:        "tww2",
				DisplayName: "TWW2",
				MsgID:       "resub",
				UserNotice:  true,
				Months:      6,
				Body:        "",
			},
			{
				Generation:  1,
				Kind:        chat.MessageSystem,
				ID:          "raid-1",
				User:        "raider",
				DisplayName: "Raider",
				MsgID:       "raid",
				UserNotice:  true,
				SystemMsg:   "5 raiders from Raider have joined!",
			},
		},
	})
	m = updated.(Model)

	msgs := m.ChatMessages()
	if len(msgs) != 4 {
		t.Fatalf("Len=%d, want 4 (chat + 2 localized resub + raid): %+v", len(msgs), msgs)
	}
	if msgs[1].Kind != chat.MessageSystem || msgs[1].Body != "CoolSub resubscribed (15 months): Great stream -- keep it up!" {
		t.Fatalf("resub with text = %+v", msgs[1])
	}
	if msgs[2].Kind != chat.MessageSystem || msgs[2].Body != "TWW2 resubscribed (6 months)" {
		t.Fatalf("resub without text = %+v", msgs[2])
	}
	if msgs[3].Body != "5 raiders from Raider have joined!" {
		t.Fatalf("raid = %+v", msgs[3])
	}

	view := m.View()
	for _, want := range []string{
		"CoolSub resubscribed (15 months)",
		"Great stream -- keep it",
		"up!",
		"TWW2 resubscribed (6 months)",
		"[system]",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View missing %q:\n%s", want, view)
		}
	}
}

func TestChatUserNotice_I18nENAndPTBR(t *testing.T) {
	cases := []struct {
		lang         string
		withText     string
		withoutText  string
		systemPrefix string
	}{
		{
			lang:         i18n.LangEN,
			withText:     "CoolSub resubscribed (15 months): Great stream!",
			withoutText:  "TWW2 resubscribed (6 months)",
			systemPrefix: "[system]",
		},
		{
			lang:         i18n.LangPTBR,
			withText:     "CoolSub renovou a inscrição (15 meses): Great stream!",
			withoutText:  "TWW2 renovou a inscrição (6 meses)",
			systemPrefix: "[sistema]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			m := joinChatFixture(t, tc.lang)
			updated, _ := m.Update(ChatEventMsg{
				Generation: 1,
				Channel:    "gaules",
				Messages: []ChatMessage{
					{
						Generation:  1,
						Kind:        chat.MessageSystem,
						MsgID:       "resub",
						UserNotice:  true,
						User:        "coolsub",
						DisplayName: "CoolSub",
						Months:      15,
						Body:        "Great stream!",
					},
					{
						Generation:  1,
						Kind:        chat.MessageSystem,
						MsgID:       "resub",
						UserNotice:  true,
						User:        "tww2",
						DisplayName: "TWW2",
						Months:      6,
					},
				},
			})
			m = updated.(Model)

			view := strings.Join(strings.Fields(m.View()), " ")
			for _, want := range []string{tc.withText, tc.withoutText, tc.systemPrefix} {
				if !strings.Contains(view, want) {
					t.Fatalf("View() missing %q:\n%s", want, m.View())
				}
			}
			bodies := map[string]bool{}
			for _, msg := range m.ChatMessages() {
				if msg.Kind == chat.MessageSystem {
					bodies[msg.Body] = true
				}
			}
			for _, want := range []string{tc.withText, tc.withoutText} {
				if !bodies[want] {
					t.Fatalf("system bodies=%v missing %q", bodies, want)
				}
			}
		})
	}
}

// TestChatUserNotice_PTYEvidence logs the rendered chat panel with resub system
// lines so a PTY/script capture can show the markers.
func TestChatUserNotice_PTYEvidence(t *testing.T) {
	m := joinChatFixture(t, i18n.LangPTBR)
	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "m1", User: "alice", DisplayName: "Alice", Body: "oi"},
			{
				Generation:  1,
				Kind:        chat.MessageSystem,
				ID:          "resub-1",
				User:        "coolsub",
				DisplayName: "CoolSub",
				MsgID:       "resub",
				UserNotice:  true,
				Months:      15,
				Body:        "Valeu pela live!",
			},
			{
				Generation:  1,
				Kind:        chat.MessageSystem,
				ID:          "resub-2",
				User:        "tww2",
				DisplayName: "TWW2",
				MsgID:       "resub",
				UserNotice:  true,
				Months:      6,
			},
		},
	})
	m = updated.(Model)

	view := m.View()
	t.Logf("\n================================================================================\nCAPTURA PTY: linhas de sistema USERNOTICE resub\n--------------------------------------------------------------------------------\n%s\n================================================================================", view)

	if !strings.Contains(view, "CoolSub renovou a inscrição (15 meses): Valeu pela live!") {
		t.Fatalf("PTY capture missing resub with text:\n%s", view)
	}
	if !strings.Contains(view, "TWW2 renovou a inscrição (6 meses)") {
		t.Fatalf("PTY capture missing resub without text:\n%s", view)
	}
	if !strings.Contains(view, "[sistema]") {
		t.Fatalf("PTY capture missing [sistema] prefix:\n%s", view)
	}
}
