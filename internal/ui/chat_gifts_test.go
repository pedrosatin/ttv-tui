package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func giftNotice(id, community, recipient string) ChatMessage {
	item := userNotice("subgift", "Santa", "Santa gifted a sub to "+recipient, "")
	item.ID, item.CommunityGiftID, item.GiftRecipient = id, community, recipient
	return item
}

func TestCommunityGiftsFifty(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	header := userNotice("submysterygift", "Santa", "Santa gifted 50 subs to the community", "")
	header.CommunityGiftID, header.GiftCount = "community", 50
	m = deliver(t, m, header)
	for i := 0; i < 50; i++ {
		m = deliver(t, m, giftNotice(fmt.Sprint(i), "community", fmt.Sprintf("Recipient%d", i)))
	}
	if got := len(m.ChatMessages()); got > 2 {
		t.Fatalf("50 gifts produced %d lines, want <= 2", got)
	}
	if got := m.ChatMessages()[0]; got.GiftCount != 50 || !strings.Contains(got.Body, "Santa gifted 50 subs") {
		t.Fatalf("summary = %+v", got)
	}
}

func TestCommunityGiftsUpdateInPlace(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), giftNotice("a", "community", "Alice"))
	if got := m.ChatMessages()[0].Body; !strings.Contains(got, "Santa") || !strings.Contains(got, "Alice") {
		t.Fatal(got)
	}
	m = deliver(t, m, ChatMessage{Kind: chat.MessageChat, ID: "chat", Body: "between"})
	m = deliver(t, m, giftNotice("b", "community", "Bob"))
	m = deliver(t, m, giftNotice("b", "community", "Bob"))
	selected, ok := m.SelectedChatMessage()
	if !ok || selected.Body != "between" {
		t.Fatalf("selected = %+v", selected)
	}
	msgs := m.ChatMessages()
	if len(msgs) != 2 || msgs[0].GiftCount != 2 || msgs[1].ID != "chat" {
		t.Fatalf("updated history = %+v", msgs)
	}
	m = deliver(t, m, giftNotice("c", "", "Carol"), giftNotice("d", "", "Dave"))
	msgs = m.ChatMessages()
	if len(msgs) != 4 || !strings.Contains(msgs[2].Body, "Carol") || !strings.Contains(msgs[3].Body, "Dave") {
		t.Fatalf("isolated gifts = %+v", msgs)
	}
	m.chat.panel.buf.ClearMsg(1, "b")
	m = deliver(t, m, giftNotice("e", "community", "Eve"))
	if !m.ChatMessages()[0].Deleted || !strings.Contains(rowOf(m, m.ChatMessages()[0]), "[deleted]") {
		t.Fatal("group lost deleted marker")
	}
}

func TestCommunityGiftsBufferBound(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	m.chat.panel.buf = chat.NewBuffer(3, 1024)
	for i := 0; i < 200; i++ {
		m = deliver(t, m, giftNotice(fmt.Sprint(i), fmt.Sprint(i/50), "Recipient"))
		if m.chat.panel.buf.Len() > 3 || m.chat.panel.buf.Bytes() > 1024 {
			t.Fatal("buffer exceeded limits")
		}
	}
	var size int
	for _, msg := range m.chat.panel.buf.Messages() {
		size += msg.ByteSize()
	}
	if size != m.chat.panel.buf.Bytes() {
		t.Fatalf("bytes: calculated %d accounted %d", size, m.chat.panel.buf.Bytes())
	}
}

func TestCommunityGiftsLateHeaderAndTwoHundred(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	for i := 0; i < 200; i++ {
		m = deliver(t, m, giftNotice(fmt.Sprint(i), "community", "Recipient"))
	}
	header := userNotice("submysterygift", "Santa", "", " ")
	header.CommunityGiftID, header.GiftCount = "community", 200
	m = deliver(t, m, header)
	if msgs := m.ChatMessages(); len(msgs) != 1 || msgs[0].GiftCount != 200 {
		t.Fatalf("summary = %+v", msgs)
	}
	m = deliver(t, m, giftNotice("extra", "community", "Last"))
	if got := m.ChatMessages()[0].GiftCount; got != 201 {
		t.Fatalf("count = %d", got)
	}
}

func TestIsolatedGiftRecipientFallback(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		item := giftNotice("single", "", "Alice")
		item.SystemMsg = ""
		m := deliver(t, joinChatFixture(t, lang), item)
		if body := m.ChatMessages()[0].Body; !strings.Contains(body, "Santa") || !strings.Contains(body, "Alice") {
			t.Fatal(body)
		}
	}
}

type giftsPTYModel struct{ Model }

func (m giftsPTYModel) Init() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return tea.Quit() })
}
func TestCommunityGiftsPTY(t *testing.T) {
	if os.Getenv("TTV_GIFTS_PTY") != "1" {
		t.Skip("opt-in PTY capture")
	}
	m := joinChatFixture(t, i18n.LangEN)
	m.width, m.height = 80, 24
	m.layout.showList, m.layout.showChat = false, true
	for i := 0; i < 50; i++ {
		m = deliver(t, m, giftNotice(fmt.Sprint(i), "community", fmt.Sprintf("Recipient%d", i)))
	}
	if _, err := tea.NewProgram(giftsPTYModel{m}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
	t.Logf("50 gifts; buffer lines=%d; total=%d", len(m.ChatMessages()), m.ChatMessages()[0].GiftCount)
}
