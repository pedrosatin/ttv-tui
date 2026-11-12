package ui

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const reconnectGapKey = "chat.reconnected_gap"

// rcLive is a chat fixture joined to #gaules (generation 1, connected) with n
// messages in the buffer, each with the IRC id Twitch would send.
func rcLive(t *testing.T, lang string, fc *fakeChat, n int) (Model, *i18n.Catalog) {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithChat(fc))
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	if cmd != nil {
		m = rcSend(t, m, cmd())
	}
	m = rcSend(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateConnected})
	m.layout.focus = FocusChat
	return rcDeliver(t, m, 1, 0, n), cat
}

func rcSend(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// rcBatch builds messages [from, to) as the session of generation gen emits them.
func rcBatch(gen uint64, from, to int) []ChatMessage {
	out := make([]ChatMessage, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, ChatMessage{
			Generation:  gen,
			Kind:        chat.MessageChat,
			ID:          fmt.Sprintf("id-%d", i),
			User:        "bob",
			DisplayName: "Bob",
			Body:        fmt.Sprintf("msg %d", i),
		})
	}
	return out
}

func rcDeliver(t *testing.T, m Model, gen uint64, from, to int) Model {
	t.Helper()
	return rcSend(t, m, ChatEventMsg{Generation: gen, Channel: "gaules", State: ChatStateConnected, Messages: rcBatch(gen, from, to)})
}

// rcCtrlT presses Ctrl+T and delivers the bridge's reply, as the runtime does.
func rcCtrlT(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+T returned no reconnect command")
	}
	return rcSend(t, m, cmd())
}

func rcConnected(t *testing.T, m Model, gen uint64) Model {
	t.Helper()
	return rcSend(t, m, ChatEventMsg{Generation: gen, Channel: "gaules", State: ChatStateConnected})
}

func rcGapLines(m Model, cat *i18n.Catalog) int {
	n := 0
	for _, msg := range m.ChatMessages() {
		if msg.Kind == chat.MessageSystem && msg.Body == cat.T(reconnectGapKey) {
			n++
		}
	}
	return n
}

// rcBodies lists the chat bodies in buffer order, leaving system lines out.
func rcBodies(m Model) []string {
	var out []string
	for _, msg := range m.ChatMessages() {
		if msg.Kind == chat.MessageChat {
			out = append(out, msg.Body)
		}
	}
	return out
}

func rcAssertHistory(t *testing.T, m Model, want int) {
	t.Helper()
	got := rcBodies(m)
	if len(got) != want {
		t.Fatalf("history has %d messages, want %d: %v", len(got), want, got)
	}
	for i, body := range got {
		if body != fmt.Sprintf("msg %d", i) {
			t.Fatalf("history[%d] = %q, want msg %d", i, body, i)
		}
	}
}

func TestCtrlT_KeepsHistoryAndMarksTheGap(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, cat := rcLive(t, lang, &fakeChat{hasToken: true, username: "tester"}, 50)

			m = rcCtrlT(t, m)
			rcAssertHistory(t, m, 50)
			if m.ChatGeneration() != 2 {
				t.Fatalf("ChatGeneration = %d, want the new session 2", m.ChatGeneration())
			}
			if got := rcGapLines(m, cat); got != 0 {
				t.Fatalf("gap line before the new session connected: %d", got)
			}

			m = rcConnected(t, m, 2)
			rcAssertHistory(t, m, 50)
			if got := rcGapLines(m, cat); got != 1 {
				t.Fatalf("gap lines = %d, want 1: %+v", got, m.ChatMessages())
			}
			msgs := m.ChatMessages()
			if last := msgs[len(msgs)-1]; last.Body != cat.T(reconnectGapKey) {
				t.Fatalf("last line = %+v, want the gap line after the history", last)
			}

			view := squash(strings.Join(chatColumn(t, m), "\n"))
			if !strings.Contains(view, squash(cat.T(reconnectGapKey))) {
				t.Fatalf("gap line not on screen:\n%s", m.View())
			}
			if !strings.Contains(view, "msg49") {
				t.Fatalf("history not on screen:\n%s", m.View())
			}
			if strings.Contains(view, squash(fmt.Sprintf(cat.T("chat.empty"), "#gaules"))) {
				t.Fatalf("empty chat text on screen:\n%s", m.View())
			}

			// What arrives afterwards lands below the gap line.
			m = rcDeliver(t, m, 2, 50, 51)
			msgs = m.ChatMessages()
			if msgs[len(msgs)-2].Body != cat.T(reconnectGapKey) || msgs[len(msgs)-1].Body != "msg 50" {
				t.Fatalf("new message out of place: %+v", msgs[len(msgs)-2:])
			}
		})
	}
}

// The session goroutine and the bridge reply race: the new session's Connected
// can reach the UI before the reply to Ctrl+T.
func TestCtrlT_SessionEventBeforeBridgeReply(t *testing.T) {
	m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	m = rcConnected(t, m, 2)
	m = rcSend(t, m, cmd())

	rcAssertHistory(t, m, 50)
	if got := rcGapLines(m, cat); got != 1 {
		t.Fatalf("gap lines = %d, want 1", got)
	}
}

func TestCtrlT_TwiceQuicklyKeepsHistoryWithOneGapLine(t *testing.T) {
	m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)

	updated, first := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	updated, second := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	m = rcSend(t, m, first())
	m = rcConnected(t, m, 2)
	m = rcSend(t, m, second())
	m = rcConnected(t, m, 3)

	rcAssertHistory(t, m, 50)
	if m.ChatGeneration() != 3 {
		t.Fatalf("ChatGeneration = %d, want 3", m.ChatGeneration())
	}
	if got := rcGapLines(m, cat); got != 1 {
		t.Fatalf("gap lines = %d, want 1: nothing arrived between the reconnects", got)
	}
}

func TestAutoReconnect_KeepsHistoryAndMarksTheGap(t *testing.T) {
	m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)

	// Two failed attempts before the session comes back, same generation.
	for range 2 {
		m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateReconnecting, ErrText: "read: EOF"})
	}
	rcAssertHistory(t, m, 50)
	if got := rcGapLines(m, cat); got != 0 {
		t.Fatalf("gap line while still offline: %d", got)
	}

	m = rcConnected(t, m, 1)
	rcAssertHistory(t, m, 50)
	if got := rcGapLines(m, cat); got != 1 {
		t.Fatalf("gap lines = %d, want 1", got)
	}

	// Later batches of the healthy session mark nothing.
	m = rcDeliver(t, m, 1, 50, 52)
	if got := rcGapLines(m, cat); got != 1 {
		t.Fatalf("gap lines = %d after normal traffic, want 1", got)
	}
}

// The first connection failing is not a gap: nothing was on screen to lose track of.
func TestReconnect_EmptyChatGetsNoGapLine(t *testing.T) {
	m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 0)

	m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateReconnecting})
	m = rcConnected(t, m, 1)
	m = rcCtrlT(t, m)
	m = rcConnected(t, m, 2)

	if got := len(m.ChatMessages()); got != 0 {
		t.Fatalf("buffer = %+v, want empty", m.ChatMessages())
	}
	if !strings.Contains(squash(m.View()), squash(fmt.Sprintf(cat.T("chat.empty"), "#gaules"))) {
		t.Fatalf("empty chat text missing:\n%s", m.View())
	}
}

func TestCtrlT_KeepsTheDraft(t *testing.T) {
	m, _ := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)
	m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	for _, r := range "rascunho" {
		m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = rcCtrlT(t, m)
	m = rcConnected(t, m, 2)
	m = rcSend(t, m, ChatEventMsg{Generation: 2, Channel: "gaules", State: ChatStateReconnecting})
	m = rcConnected(t, m, 2)

	if m.ComposeDraft() != "rascunho" {
		t.Fatalf("draft = %q, want rascunho", m.ComposeDraft())
	}
	if !m.chat.composing {
		t.Fatal("reconnecting left compose mode")
	}
	rcAssertHistory(t, m, 50)
}

func TestReconnect_ServerResendDoesNotDuplicate(t *testing.T) {
	reconnect := map[string]func(*testing.T, Model) (Model, uint64){
		"ctrl_t": func(t *testing.T, m Model) (Model, uint64) {
			return rcConnected(t, rcCtrlT(t, m), 2), 2
		},
		"automatic": func(t *testing.T, m Model) (Model, uint64) {
			m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateReconnecting})
			return rcConnected(t, m, 1), 1
		},
	}
	for name, fn := range reconnect {
		t.Run(name, func(t *testing.T) {
			m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)
			m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateConnected, ClearMsgID: "id-7"})

			m, gen := fn(t, m)
			// The replay overlaps the history and brings one message that is new.
			m = rcDeliver(t, m, gen, 0, 51)

			rcAssertHistory(t, m, 51)
			if got := rcGapLines(m, cat); got != 1 {
				t.Fatalf("gap lines = %d, want 1", got)
			}
			for _, msg := range m.ChatMessages() {
				if msg.ID == "id-7" && !msg.Deleted {
					t.Fatal("replay brought a moderated message back as live")
				}
			}
		})
	}
}

// A full buffer is the hard case: the gap line evicts the oldest message, and
// the replay of that message must not start rotating the history.
func TestReconnect_BufferLimitHoldsWithReplay(t *testing.T) {
	m, cat := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, chat.DefaultMaxLines+20)
	maxLines, _ := m.ChatBufferLimits()
	if got := len(m.ChatMessages()); got != maxLines {
		t.Fatalf("buffer = %d, want it full at %d", got, maxLines)
	}

	m = rcConnected(t, rcCtrlT(t, m), 2)
	if got := len(m.ChatMessages()); got != maxLines {
		t.Fatalf("buffer = %d after the gap line, want %d", got, maxLines)
	}
	if got := rcGapLines(m, cat); got != 1 {
		t.Fatalf("gap lines = %d, want 1", got)
	}

	m = rcDeliver(t, m, 2, 0, chat.DefaultMaxLines+20)
	msgs := m.ChatMessages()
	if len(msgs) != maxLines {
		t.Fatalf("buffer = %d after the replay, want %d", len(msgs), maxLines)
	}
	if last := msgs[len(msgs)-1]; last.Body != cat.T(reconnectGapKey) {
		t.Fatalf("replay moved past the gap line: last = %+v", last)
	}
	seen := make(map[string]bool, len(msgs))
	for _, msg := range msgs {
		if msg.ID != "" && seen[msg.ID] {
			t.Fatalf("duplicated id %s", msg.ID)
		}
		seen[msg.ID] = true
	}

	m = rcDeliver(t, m, 2, chat.DefaultMaxLines+20, chat.DefaultMaxLines+21)
	if got := len(m.ChatMessages()); got != maxLines {
		t.Fatalf("buffer = %d, want the limit %d after a new message", got, maxLines)
	}
}

// failingReconnect answers Ctrl+T the way the bridge does when Manager.Reconnect
// fails: no generation at all.
type failingReconnect struct{ *fakeChat }

func (failingReconnect) Reconnect() tea.Cmd {
	return func() tea.Msg {
		return ChatEventMsg{State: ChatStateError, ErrText: "chat: no active channel to reconnect", FromJoin: true}
	}
}

func TestCtrlT_FailureKeepsHistoryAndSession(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithChat(failingReconnect{&fakeChat{hasToken: true, username: "tester"}}))
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "gaules"}})
	m = updated.(Model)
	m = rcSend(t, m, cmd())
	m = rcDeliver(t, m, 1, 0, 50)

	m = rcCtrlT(t, m)

	rcAssertHistory(t, m, 50)
	if m.ChatState() != ChatStateError {
		t.Fatalf("ChatState = %q, want the error shown", m.ChatState())
	}
	if m.ChatGeneration() != 1 {
		t.Fatalf("ChatGeneration = %d, want the live session 1", m.ChatGeneration())
	}
	if got := rcGapLines(m, cat); got != 0 {
		t.Fatalf("gap line without a reconnect: %d", got)
	}
}

func TestCtrlT_KeepsTheScrollPosition(t *testing.T) {
	m, _ := rcLive(t, i18n.LangEN, &fakeChat{hasToken: true, username: "tester"}, 50)
	for range 5 {
		m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	}
	before, ok := m.SelectedChatMessage()
	if !ok || m.ChatFollowLive() {
		t.Fatalf("fixture not paused: %+v live=%v", before, m.ChatFollowLive())
	}

	m = rcConnected(t, rcCtrlT(t, m), 2)

	if m.ChatFollowLive() {
		t.Fatal("reconnect dropped the reader back to live")
	}
	after, ok := m.SelectedChatMessage()
	if !ok || after.ID != before.ID {
		t.Fatalf("selection moved from %+v to %+v", before, after)
	}
}

// Preserving history must stop at the channel: switching still starts empty.
func TestChannelSwitch_StillClearsHistory(t *testing.T) {
	fc := &fakeChat{hasToken: true, username: "tester"}
	m, cat := rcLive(t, i18n.LangEN, fc, 50)

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "2", Login: "alanzoka"}})
	m = updated.(Model)
	m = rcSend(t, m, cmd())
	m = rcSend(t, m, ChatEventMsg{Generation: 2, Channel: "alanzoka", State: ChatStateConnected})

	if got := m.ChatMessages(); len(got) != 0 {
		t.Fatalf("history of #gaules leaked into #alanzoka: %+v", got)
	}
	if got := rcGapLines(m, cat); got != 0 {
		t.Fatalf("gap line on a channel switch: %d", got)
	}
}

// A session pending a gap line that the user leaves must not mark the next channel.
func TestChannelSwitch_DropsThePendingGapLine(t *testing.T) {
	fc := &fakeChat{hasToken: true, username: "tester"}
	m, cat := rcLive(t, i18n.LangEN, fc, 50)
	m = rcSend(t, m, ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateReconnecting})

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: catalog.Channel{ID: "2", Login: "alanzoka"}})
	m = updated.(Model)
	m = rcSend(t, m, cmd())
	m = rcSend(t, m, ChatEventMsg{
		Generation: 2, Channel: "alanzoka", State: ChatStateConnected,
		Messages: []ChatMessage{{Generation: 2, Kind: chat.MessageChat, ID: "a1", DisplayName: "Fan", Body: "oi"}},
	})

	if got := rcGapLines(m, cat); got != 0 {
		t.Fatalf("gap line carried over to the new channel: %d", got)
	}
}

func TestReconnectGapLine_ParityAcrossLanguages(t *testing.T) {
	en, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pt, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	for _, key := range []string{reconnectGapKey, "chat.reject.reconnected", "chat.warn.reconnected"} {
		if !en.Has(key) || !pt.Has(key) {
			t.Fatalf("%s missing: en=%v pt-BR=%v", key, en.Has(key), pt.Has(key))
		}
		if en.T(key) == pt.T(key) {
			t.Fatalf("%s is not translated: %q", key, en.T(key))
		}
	}
}

// A message caught by Ctrl+T stays in the history now, so its label must say
// what happened: the channel did not change.
func TestCtrlT_PendingMessageIsNotBlamedOnAChannelChange(t *testing.T) {
	m, cat := rcLive(t, i18n.LangPTBR, &fakeChat{hasToken: true, username: "tester"}, 3)
	m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	for _, r := range "oi" {
		m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = rcSend(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	var nonce string
	for _, msg := range m.ChatMessages() {
		if msg.Self {
			nonce = msg.Nonce
		}
	}
	if nonce == "" {
		t.Fatalf("no outgoing message in %+v", m.ChatMessages())
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	m = rcSend(t, m, ChatEventMsg{
		Generation: 2, Channel: "gaules", State: ChatStateConnecting,
		SendUpdates: []ChatSendStatusUpdate{{
			Generation: 2, Channel: "gaules", Nonce: nonce, Body: "oi",
			Status: chat.DeliveryDiscarded, Reason: chat.RejectReconnected,
		}},
	})

	if got, want := m.ChatSendWarning(), cat.T("chat.warn.reconnected"); got != want {
		t.Fatalf("warning = %q, want %q", got, want)
	}
	view := squash(strings.Join(chatColumn(t, m), "\n"))
	if !strings.Contains(view, squash(cat.T("chat.reject.reconnected"))) {
		t.Fatalf("discarded message lost its reason:\n%s", m.View())
	}
	if strings.Contains(view, squash(cat.T("chat.reject.channel_switched"))) {
		t.Fatalf("reconnect blamed on a channel change:\n%s", m.View())
	}
}
