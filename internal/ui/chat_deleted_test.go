package ui

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func offense(id, body string) ChatMessage {
	return ChatMessage{Generation: 1, Kind: chat.MessageChat, ID: id, User: "xingador", DisplayName: "Xingador", Body: body}
}

func moderate(t *testing.T, m Model, ev ChatEventMsg) Model {
	t.Helper()
	ev.Generation, ev.Channel = 1, "gaules"
	updated, _ := m.Update(ev)
	return updated.(Model)
}

func timeoutEvent(user string, secs int) ChatEventMsg {
	return ChatEventMsg{ClearChat: true, ClearUser: user, BanDuration: &secs}
}

func rowOf(m Model, msg ChatMessage) string {
	lead, text := m.formatChatMessageParts(msg, false)
	return ansi.Strip(lead + text)
}

func TestDeletedMessage_StaysWithMarkerAndText(t *testing.T) {
	cases := []struct {
		name  string
		event ChatEventMsg
		mod   string
	}{
		{"delete", ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"}, "[mod] Mensagem de xingador apagada"},
		{"timeout", timeoutEvent("xingador", 600), "[mod] xingador silenciado por 10m"},
		{"ban", ChatEventMsg{ClearChat: true, ClearUser: "xingador"}, "[mod] xingador foi banido permanentemente"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := deliver(t, joinChatFixture(t, i18n.LangPTBR), offense("x1", "você é um lixo"))
			m = moderate(t, m, tc.event)

			msgs := m.ChatMessages()
			if len(msgs) != 2 {
				t.Fatalf("messages = %+v, want the original and the [mod] line", msgs)
			}
			if !msgs[0].Deleted || msgs[0].Body != "você é um lixo" {
				t.Fatalf("original = %+v, want it Deleted with the text intact", msgs[0])
			}
			if got, want := rowOf(m, msgs[0]), "  [apagada] Xingador: você é um lixo"; got != want {
				t.Fatalf("row = %q, want %q", got, want)
			}

			// The event is a line of its own and never carries the Deleted mark.
			if msgs[1].Deleted || msgs[1].Kind != chat.MessageSystem {
				t.Fatalf("event = %+v, want an unmarked system line", msgs[1])
			}
			view := ansi.Strip(m.View())
			for _, want := range []string{"[apagada] Xingador: você é um lixo", tc.mod} {
				if !strings.Contains(view, want) {
					t.Fatalf("View() missing %q:\n%s", want, view)
				}
			}
			if strings.Index(view, "[apagada]") > strings.Index(view, "[mod]") {
				t.Fatalf("the event must come after the message it refers to:\n%s", view)
			}
		})
	}
}

func TestDeletedMessage_MarkerFollowsTheLanguage(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), offense("x1", "you are trash"))
	m = moderate(t, m, ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"})
	if got, want := rowOf(m, m.ChatMessages()[0]), "  [deleted] Xingador: you are trash"; got != want {
		t.Fatalf("row = %q, want %q", got, want)
	}
}

func TestDeletedMessage_PurgeMarksEveryMessageOfTheTarget(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangPTBR),
		offense("x1", "primeira"),
		ChatMessage{Generation: 1, Kind: chat.MessageChat, ID: "a1", User: "alice", DisplayName: "Alice", Body: "oi"},
		offense("x2", "segunda"),
		offense("x3", "terceira"),
	)
	m = moderate(t, m, timeoutEvent("xingador", 60))

	msgs := m.ChatMessages()
	if len(msgs) != 5 {
		t.Fatalf("messages = %+v", msgs)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"[apagada] Xingador: primeira", "[apagada] Xingador: segunda", "[apagada] Xingador: terceira"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
	if msgs[1].Deleted || strings.Contains(view, "[apagada] Alice") {
		t.Fatalf("purge reached another user: %+v\n%s", msgs[1], view)
	}
	if n := strings.Count(view, "[mod]"); n != 1 {
		t.Fatalf("purge wrote %d [mod] lines, want 1:\n%s", n, view)
	}
}

// The text is the evidence, so y hands it over exactly as it would before
// moderation: author and body, with no marker of the panel.
func TestDeletedMessage_CopyKeepsTheOriginalText(t *testing.T) {
	forceOSCClipboardEnv(t)
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := deliver(t, joinChatFixture(t, i18n.LangPTBR), offense("x1", "você é um lixo"))
	m = moderate(t, m, timeoutEvent("xingador", 600))
	m.layout.focus = FocusChat

	// While following live the selection is the [mod] line; one step up is the
	// message the moderator wants.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if sel, ok := m.SelectedChatMessage(); !ok || !sel.Deleted {
		t.Fatalf("selection = %+v, %v; want the erased message", sel, ok)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("y on an erased message copied nothing")
	}
	m = flushCopy(t, m, cmd)

	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := expectedContextCopy(m, "Xingador: você é um lixo"); got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
	if !strings.Contains(m.CopyNotice(), "você é um lixo") {
		t.Fatalf("notice = %q", m.CopyNotice())
	}
}

var foregroundSGR = regexp.MustCompile(`\x1b\[[0-9;]*(3[0-7]|9[0-7])[;m]`)

func TestDeletedMessage_MarkerSurvivesNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	resetOrientationColorsForTest()
	t.Cleanup(func() { forceOrientationColorsForTest(true) })

	m := deliver(t, joinChatFixture(t, i18n.LangPTBR), offense("x1", "você é um lixo"))
	m = moderate(t, m, ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"})

	lead, text := m.formatChatMessageParts(m.ChatMessages()[0], false)
	if foregroundSGR.MatchString(text) {
		t.Fatalf("NO_COLOR row carries a foreground color: %q", text)
	}
	if got, want := ansi.Strip(lead+text), "  [apagada] Xingador: você é um lixo"; got != want {
		t.Fatalf("row = %q, want %q", got, want)
	}
}

// Every shape a row can take has its own branch in the formatter; none of them
// may lose the marker.
func TestDeletedMessage_MarkerOnEveryRowShape(t *testing.T) {
	base := offense("x1", "texto")
	action, highlighted, mention, self := base, base, base, base
	action.Action = true
	highlighted.Highlighted = true
	mention.Body = "ei @eu texto"
	self.Self, self.Delivery = true, chat.DeliveryRejected

	cases := []struct {
		name string
		msg  ChatMessage
		want string
	}{
		{"action", action, "  * [apagada] Xingador texto"},
		{"highlighted", highlighted, "★ [apagada] [destaque] Xingador: texto"},
		{"mention", mention, "★ [apagada] [menção] Xingador: ei @eu texto"},
		{"rejected self", self, "  [apagada] Xingador: texto"},
		{"resub line", ChatMessage{Generation: 1, Kind: chat.MessageSystem, ID: "x1", User: "xingador", MsgID: "resub", Body: "Xingador resubscribed — texto"}, "  [apagada] [sistema] Xingador resubscribed — texto"},
		{"raid line", ChatMessage{Generation: 1, Kind: chat.MessageSystem, ID: "x1", User: "xingador", MsgID: "raid", Body: "10 raiders"}, "★ [apagada] [raid] 10 raiders"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := joinChatFixture(t, i18n.LangPTBR)
			m.chat.panel.username = "eu"
			m = deliver(t, m, tc.msg)
			m = moderate(t, m, ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"})
			if got := rowOf(m, m.ChatMessages()[0]); !strings.HasPrefix(got, tc.want) {
				t.Fatalf("row = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

func TestDeletedMessage_LongTextWrapsCompleteAtEightyColumns(t *testing.T) {
	m := joinChatFixture(t, i18n.LangPTBR)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	long := strings.TrimSpace(strings.Repeat("ofensa comprida ", 12))
	m = deliver(t, updated.(Model), offense("x1", long))
	m = moderate(t, m, ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"})

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[apagada] Xingador: ofensa") {
		t.Fatalf("marker must open the message at 80 columns:\n%s", view)
	}
	if got := strings.Count(view, "ofensa"); got != 12 {
		t.Fatalf("found %d of 12 words, the text must not be cut:\n%s", got, view)
	}
	for _, row := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(row); w > 80 {
			t.Fatalf("row %q is %d cells wide", row, w)
		}
	}
}

// A full clear is not moderation of one person: the screen empties, as it does
// on Twitch, and what remains is a single line saying so.
func TestDeletedMessage_FullClearEmptiesAndLeavesOneLine(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangPTBR), offense("x1", "primeira"), offense("x2", "segunda"))
	m = moderate(t, m, ChatEventMsg{ClearChat: true})

	msgs := m.ChatMessages()
	if len(msgs) != 1 || msgs[0].Kind != chat.MessageSystem || msgs[0].Deleted {
		t.Fatalf("messages after a full clear = %+v, want one system line", msgs)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[mod] Chat limpo por um moderador") {
		t.Fatalf("clear event missing:\n%s", view)
	}
	if strings.Contains(view, "primeira") || strings.Contains(view, "[apagada]") {
		t.Fatalf("a full clear must not keep the text:\n%s", view)
	}
}

// Marking does not change indexes, so a moderator reading history keeps the
// row under the highlight when a purge arrives.
func TestDeletedMessage_PurgeKeepsTheScrollAnchor(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN),
		offense("x1", "um"),
		ChatMessage{Generation: 1, Kind: chat.MessageChat, ID: "a1", User: "alice", DisplayName: "Alice", Body: "dois"},
		offense("x2", "tres"),
		ChatMessage{Generation: 1, Kind: chat.MessageChat, ID: "a2", User: "alice", DisplayName: "Alice", Body: "quatro"},
	)
	m.layout.focus = FocusChat
	for range 2 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(Model)
	}
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.ID != "a1" {
		t.Fatalf("selection before the purge = %+v", sel)
	}

	m = moderate(t, m, timeoutEvent("xingador", 60))

	sel, ok = m.SelectedChatMessage()
	if !ok || sel.ID != "a1" {
		t.Fatalf("selection after the purge = %+v, want a1", sel)
	}
	if m.ChatFollowLive() {
		t.Fatal("purge must not resume live")
	}
	if got := m.ChatPendingNew(); got != 1 {
		t.Fatalf("pending = %d, want 1: only the [mod] line is new", got)
	}
}

func TestDeletedMessage_LeavesTheBufferAtItsLimit(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), offense("x1", "evidence"))
	m = moderate(t, m, ChatEventMsg{ClearUser: "xingador", ClearMsgID: "x1"})

	limit := m.chat.panel.buf.MaxLines()
	for i := range limit {
		m = deliver(t, m, ChatMessage{Generation: 1, Kind: chat.MessageChat, ID: fmt.Sprint("n", i), User: "alice", DisplayName: "Alice", Body: "hi"})
	}

	msgs := m.ChatMessages()
	if len(msgs) != limit {
		t.Fatalf("Len = %d, want the limit %d", len(msgs), limit)
	}
	for _, msg := range msgs {
		if msg.Deleted || msg.Body == "evidence" {
			t.Fatalf("erased message outlived the buffer limit: %+v", msg)
		}
	}
}
