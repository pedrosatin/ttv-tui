package ui

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func scrollFixture(t *testing.T, lang string, n int) Model {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	fc := &fakeChat{hasToken: true, username: "tester"}
	m := New(cat, WithChat(fc))
	updated, cmd := m.Update(ChannelSelectedMsg{
		Channel: catalog.Channel{ID: "1", Login: "gaules", DisplayName: "Gaules"},
	})
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		State:      ChatStateConnected,
	})
	m = updated.(Model)
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	m.layout.focus = FocusChat
	if n > 0 {
		m = withMessages(t, m, n)
	}
	return m
}

func TestChatScroll_UpPausesAndSelects(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 20)
	if !m.ChatFollowLive() {
		t.Fatal("expected follow live at start")
	}
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != "msg 19" {
		t.Fatalf("live selection = %+v ok=%v, want msg 19", sel, ok)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.ChatFollowLive() {
		t.Fatal("↑/k must pause auto-scroll")
	}
	if m.HighlightedID() != "" && m.Focus() != FocusChat {
		t.Fatalf("focus left chat: %q", m.Focus())
	}
	sel, ok = m.SelectedChatMessage()
	if !ok || sel.Body != "msg 18" {
		t.Fatalf("after k selection = %+v ok=%v, want msg 18", sel, ok)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	sel, ok = m.SelectedChatMessage()
	if !ok || sel.Body != "msg 17" {
		t.Fatalf("after KeyUp selection = %+v ok=%v, want msg 17", sel, ok)
	}
}

func TestChatScroll_PendingNewNoticeAndEndResumes(t *testing.T) {
	m := scrollFixture(t, i18n.LangPTBR, 15)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	if m.ChatFollowLive() || m.ChatPendingNew() != 0 {
		t.Fatalf("after pause: follow=%v pending=%d", m.ChatFollowLive(), m.ChatPendingNew())
	}
	pausedSel, _ := m.SelectedChatMessage()

	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "n1", User: "a", DisplayName: "A", Body: "nova 1"},
			{Generation: 1, Kind: chat.MessageChat, ID: "n2", User: "b", DisplayName: "B", Body: "nova 2"},
			{Generation: 1, Kind: chat.MessageChat, ID: "n3", User: "c", DisplayName: "C", Body: "nova 3"},
		},
	})
	m = updated.(Model)
	if m.ChatFollowLive() {
		t.Fatal("new messages must not resume live while scrolled")
	}
	if m.ChatPendingNew() != 3 {
		t.Fatalf("pending=%d, want 3", m.ChatPendingNew())
	}
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != pausedSel.Body {
		t.Fatalf("selection moved while paused: got %+v, want body %q", sel, pausedSel.Body)
	}

	plain := ansi.Strip(m.View())
	wantNotice := fmt.Sprintf(m.t("chat.scroll.new"), 3)
	if !strings.Contains(plain, wantNotice) {
		t.Fatalf("view missing pending notice %q:\n%s", wantNotice, plain)
	}
	// Newest arrivals stay off-screen while paused.
	if strings.Contains(plain, "nova 3") {
		t.Fatalf("paused view must not show newest arrival:\n%s", plain)
	}
	cols := chatColumn(t, m)
	if len(cols) == 0 || !strings.Contains(cols[len(cols)-1], ">") {
		t.Fatalf("input line must stay at chat footer: last=%q", lastNonEmpty(cols))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if !m.ChatFollowLive() || m.ChatPendingNew() != 0 {
		t.Fatalf("End must resume live: follow=%v pending=%d", m.ChatFollowLive(), m.ChatPendingNew())
	}
	sel, ok = m.SelectedChatMessage()
	if !ok || sel.Body != "nova 3" {
		t.Fatalf("after End selection = %+v ok=%v, want nova 3", sel, ok)
	}
	plain = ansi.Strip(m.View())
	if strings.Contains(plain, wantNotice) {
		t.Fatalf("notice must clear after End:\n%s", plain)
	}
	if !strings.Contains(plain, "nova 3") {
		t.Fatalf("live view must show newest:\n%s", plain)
	}
}

func TestChatScroll_GResumesLikeEnd(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 10)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.ChatFollowLive() {
		t.Fatal("PgUp must pause")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(Model)
	if !m.ChatFollowLive() {
		t.Fatal("G must resume live")
	}
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != "msg 9" {
		t.Fatalf("after G selection = %+v, want msg 9", sel)
	}
}

func TestChatScroll_PageUpAndHome(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 40)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.ChatFollowLive() {
		t.Fatal("PgUp must pause")
	}
	sel, ok := m.SelectedChatMessage()
	if !ok {
		t.Fatal("expected selection after PgUp")
	}
	pageSel := sel.Body

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if m.ChatFollowLive() {
		t.Fatal("Home must stay paused")
	}
	sel, ok = m.SelectedChatMessage()
	if !ok {
		t.Fatal("expected selection after Home")
	}
	// Home lands on the oldest page; selection is at or near the start.
	if sel.Body == "msg 39" || sel.Body == pageSel && pageSel == "msg 39" {
		t.Fatalf("Home should leave the live bottom, got %q (page was %q)", sel.Body, pageSel)
	}
	// Oldest message must be reachable and visible after Home.
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "msg 0") {
		t.Fatalf("Home view should include oldest message:\n%s", plain)
	}
}

func TestChatScroll_DownTowardLive(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 8)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	sel, _ := m.SelectedChatMessage()
	if sel.Body != "msg 5" {
		t.Fatalf("after kk selection=%q, want msg 5", sel.Body)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	sel, _ = m.SelectedChatMessage()
	if sel.Body != "msg 6" {
		t.Fatalf("after j selection=%q, want msg 6", sel.Body)
	}
	// Walk back to the bottom: last j resumes live.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if !m.ChatFollowLive() {
		t.Fatal("reaching newest via ↓ must resume live")
	}
}

func TestChatScroll_InputPinnedAndBufferLimits(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 12)
	maxLines, maxBytes := m.ChatBufferLimits()
	if maxLines != chat.DefaultMaxLines || maxBytes != chat.DefaultMaxBytes {
		t.Fatalf("limits=%d/%d, want %d/%d", maxLines, maxBytes, chat.DefaultMaxLines, chat.DefaultMaxBytes)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	cols := chatColumn(t, m)
	if len(cols) == 0 {
		t.Fatal("empty chat column")
	}
	last := cols[len(cols)-1]
	if !strings.Contains(last, ">") {
		t.Fatalf("input must remain last chat row after Home: %q", last)
	}

	// Fill past the line limit while paused; buffer must stay capped and scroll
	// must not panic.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	batch := make([]ChatMessage, 0, chat.DefaultMaxLines+50)
	for i := 0; i < chat.DefaultMaxLines+50; i++ {
		batch = append(batch, ChatMessage{
			Generation: 1, Kind: chat.MessageChat,
			ID: fmt.Sprintf("flood-%d", i), User: "u", DisplayName: "U",
			Body: fmt.Sprintf("flood %d", i),
		})
	}
	updated, _ = m.Update(ChatEventMsg{Generation: 1, Channel: "gaules", Messages: batch})
	m = updated.(Model)
	if got := len(m.ChatMessages()); got > chat.DefaultMaxLines {
		t.Fatalf("buffer len=%d, want <= %d", got, chat.DefaultMaxLines)
	}
	if m.ChatFollowLive() {
		t.Fatal("flood while paused must keep followLive false")
	}
	if _, ok := m.SelectedChatMessage(); !ok {
		t.Fatal("selection must remain valid after eviction")
	}
}

func TestChatScroll_SelectionAtEachPosition(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 5)
	want := []string{"msg 4", "msg 3", "msg 2", "msg 1", "msg 0"}
	for i, body := range want {
		sel, ok := m.SelectedChatMessage()
		if !ok || sel.Body != body {
			t.Fatalf("step %d: selection=%q ok=%v, want %q", i, sel.Body, ok, body)
		}
		if i == len(want)-1 {
			break
		}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = updated.(Model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != "msg 4" {
		t.Fatalf("after End selection=%q, want msg 4", sel.Body)
	}
}

func TestChatScroll_PTYCapture(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 30)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	updated, _ = m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{
			{Generation: 1, Kind: chat.MessageChat, ID: "x1", DisplayName: "X", Body: "arrived while paused"},
			{Generation: 1, Kind: chat.MessageChat, ID: "x2", DisplayName: "Y", Body: "second arrival"},
		},
	})
	m = updated.(Model)

	plain := ansi.Strip(m.View())
	fmt.Printf("\n--- PTY CAPTURE CHAT SCROLL ---\n%s\n--- END CAPTURE ---\n", plain)
	t.Logf("PTY capture:\n%s", plain)

	if !strings.Contains(plain, "2 new ↓") {
		t.Fatalf("capture missing pending notice:\n%s", plain)
	}
	if !strings.Contains(plain, "▌") {
		t.Fatalf("capture missing selection marker:\n%s", plain)
	}
	cols := chatColumn(t, m)
	if !strings.Contains(cols[len(cols)-1], ">") {
		t.Fatalf("capture input not pinned: %q", cols[len(cols)-1])
	}
}

func lastNonEmpty(cols []string) string {
	for i := len(cols) - 1; i >= 0; i-- {
		if strings.TrimSpace(cols[i]) != "" {
			return cols[i]
		}
	}
	return ""
}
