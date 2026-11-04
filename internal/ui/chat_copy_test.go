package ui

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func decodeOSC52Payload(seq string) (string, error) {
	if !strings.HasPrefix(seq, osc52Prefix) {
		return "", errOSC52Shape
	}
	rest := strings.TrimPrefix(seq, osc52Prefix)
	rest = strings.TrimSuffix(rest, osc52BEL)
	rest = strings.TrimSuffix(rest, string([]byte{0x1b, '\\'}))
	raw, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

var errOSC52Shape = errString("osc52 shape")

type errString string

func (e errString) Error() string { return string(e) }

// flushCopyCmds runs OSC write cmds from a copy batch and ignores the clear
// tick (which would sleep for copyNoticeTTL).
func flushCopyCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		runCmdWithTimeout(t, cmd)
		return
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		runCmdWithTimeout(t, c)
	}
}

func runCmdWithTimeout(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = cmd()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Millisecond):
		// tea.Tick blocks until TTL; leave it.
	}
}

func TestFormatChatCopyText_AuthorAndBody(t *testing.T) {
	got := formatChatCopyText(ChatMessage{
		Kind:        chat.MessageChat,
		DisplayName: "Gaules",
		User:        "gaules",
		Body:        "salve tribo",
	})
	if got != "Gaules: salve tribo" {
		t.Fatalf("got %q, want %q", got, "Gaules: salve tribo")
	}
}

func TestFormatChatCopyText_SystemIsBodyOnly(t *testing.T) {
	got := formatChatCopyText(ChatMessage{
		Kind: chat.MessageSystem,
		Body: "Chat cleared by a moderator",
	})
	if got != "Chat cleared by a moderator" {
		t.Fatalf("system copy = %q", got)
	}
	got = formatChatCopyText(ChatMessage{
		Kind: chat.MessageNotice,
		Body: "This room is now in slow mode",
	})
	if got != "This room is now in slow mode" {
		t.Fatalf("notice copy = %q", got)
	}
}

func TestOSC52PayloadDecodesToAuthorText(t *testing.T) {
	plain := formatChatCopyText(ChatMessage{
		Kind:        chat.MessageChat,
		DisplayName: "autor",
		Body:        "texto",
	})
	if plain != "autor: texto" {
		t.Fatalf("plaintext = %q", plain)
	}
	seq := osc52Sequence(plain)
	if !strings.HasPrefix(seq, osc52Prefix) || !strings.HasSuffix(seq, osc52BEL) {
		t.Fatalf("bad OSC shape: %q", seq)
	}
	got, err := decodeOSC52Payload(seq)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "autor: texto" {
		t.Fatalf("decoded = %q, want autor: texto", got)
	}
}

func TestCopyDoesNotIncludeLayoutJunk(t *testing.T) {
	m := scrollFixture(t, i18n.LangEN, 5)
	msg, ok := m.SelectedChatMessage()
	if !ok {
		t.Fatal("expected selection")
	}
	plain := formatChatCopyText(msg)
	for _, junk := range []string{"│", "▌", "  ", "\n", "\t"} {
		if junk == "  " {
			// author: text has no double-space padding from the list column
			if strings.Contains(plain, "  ") {
				t.Fatalf("payload has layout padding: %q", plain)
			}
			continue
		}
		if strings.Contains(plain, junk) {
			t.Fatalf("payload %q contains layout junk %q", plain, junk)
		}
	}
	view := m.View()
	if !strings.Contains(view, panelSeparator) && !strings.Contains(ansi.Strip(view), "│") {
		// Side-by-side layout should show the separator in the full view; the
		// copy payload must not.
		t.Log("view may be single-panel in fixture; payload check still stands")
	}
	if strings.Contains(plain, "│") || strings.Contains(plain, "▌") {
		t.Fatalf("payload leaked selection/layout markers: %q", plain)
	}
	if plain != "User: msg 4" {
		t.Fatalf("payload = %q, want User: msg 4", plain)
	}
}

func TestCopyY_LiveCopiesLastMessageAndWritesOSC(t *testing.T) {
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 3)
	if !m.ChatFollowLive() {
		t.Fatal("expected live follow")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected OSC write + clear tick cmds")
	}
	flushCopyCmds(t, cmd)

	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode OSC from stderr writer: %v (raw=%q)", err, buf.String())
	}
	if got != "User: msg 2" {
		t.Fatalf("copied %q, want last live message User: msg 2", got)
	}
	wantNotice := "Copied: User — msg 2"
	if m.CopyNotice() != wantNotice {
		t.Fatalf("notice = %q, want %q", m.CopyNotice(), wantNotice)
	}
	if !strings.Contains(ansi.Strip(m.View()), wantNotice) {
		t.Fatalf("view missing copy notice:\n%s", ansi.Strip(m.View()))
	}
}

func TestCopyY_ScrolledCopiesSelection(t *testing.T) {
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 10)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(Model)
	sel, ok := m.SelectedChatMessage()
	if !ok || sel.Body != "msg 7" {
		t.Fatalf("selection = %+v", sel)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	flushCopyCmds(t, cmd)
	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "User: msg 7" {
		t.Fatalf("copied %q, want selected User: msg 7", got)
	}
	if !strings.Contains(m.CopyNotice(), "msg 7") {
		t.Fatalf("notice = %q", m.CopyNotice())
	}
}

func TestCopyY_SystemMessage(t *testing.T) {
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 0)
	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Channel:    "gaules",
		Messages: []ChatMessage{{
			Generation: 1,
			Kind:       chat.MessageSystem,
			ID:         "sys1",
			Body:       "gaules timed out for 10s",
		}},
	})
	m = updated.(Model)
	m.focus = FocusChat

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	flushCopyCmds(t, cmd)
	got, err := decodeOSC52Payload(buf.String())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "gaules timed out for 10s" {
		t.Fatalf("system payload = %q", got)
	}
	if !strings.HasPrefix(m.CopyNotice(), "Copied: ") {
		t.Fatalf("notice = %q", m.CopyNotice())
	}
	if strings.Contains(got, "[mod]") || strings.Contains(got, "▌") {
		t.Fatalf("system payload has UI chrome: %q", got)
	}
}

func TestCopyNoticeExpiresWithInjectedClock(t *testing.T) {
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	now := base
	m := scrollFixture(t, i18n.LangPTBR, 2)
	m.nowFunc = func() time.Time { return now }

	prev := osc52Out
	osc52Out = ioDiscard{}
	t.Cleanup(func() { osc52Out = prev })

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	flushCopyCmds(t, cmd)
	if m.CopyNotice() == "" {
		t.Fatal("expected copy notice")
	}
	want := "Copiado: User — msg 1"
	if m.CopyNotice() != want {
		t.Fatalf("pt-BR notice = %q, want %q", m.CopyNotice(), want)
	}
	gen := m.copyNoticeGen

	// Clear tick before TTL must keep the notice when the clock is frozen.
	updated, _ = m.Update(copyNoticeClearMsg{gen: gen})
	m = updated.(Model)
	if m.CopyNotice() == "" {
		t.Fatal("notice cleared before injected clock reached TTL")
	}

	now = base.Add(copyNoticeTTL)
	updated, _ = m.Update(copyNoticeClearMsg{gen: gen})
	m = updated.(Model)
	if m.CopyNotice() != "" {
		t.Fatalf("notice still visible after TTL: %q", m.CopyNotice())
	}
}

func TestCopyY_IgnoredOnListFocus(t *testing.T) {
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 2)
	m.focus = FocusList
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd != nil {
		flushCopyCmds(t, cmd)
	}
	if buf.Len() != 0 {
		t.Fatalf("list focus must not copy, wrote %q", buf.String())
	}
	if m.CopyNotice() != "" {
		t.Fatalf("unexpected notice %q", m.CopyNotice())
	}
}

func TestCopyI18nBothLanguages(t *testing.T) {
	cases := []struct {
		lang string
		want string
	}{
		{i18n.LangEN, "Copied: User — msg 0"},
		{i18n.LangPTBR, "Copiado: User — msg 0"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			prev := osc52Out
			osc52Out = ioDiscard{}
			t.Cleanup(func() { osc52Out = prev })

			m := scrollFixture(t, tc.lang, 1)
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			m = updated.(Model)
			flushCopyCmds(t, cmd)
			if m.CopyNotice() != tc.want {
				t.Fatalf("notice = %q, want %q", m.CopyNotice(), tc.want)
			}
			if m.t("chat.copy.done") == "chat.copy.done" || m.t("chat.copy.done_text") == "chat.copy.done_text" {
				t.Fatal("missing i18n keys")
			}
		})
	}
}

func TestCopyNoticeVisibleForPTYCapture(t *testing.T) {
	prev := osc52Out
	osc52Out = ioDiscard{}
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangPTBR, 1)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	flushCopyCmds(t, cmd)
	view := ansi.Strip(m.View())
	t.Logf("PTY capture (ansi-stripped View after y):\n%s", view)
	if !strings.Contains(view, "Copiado: User — msg 0") {
		t.Fatalf("missing copy notice in view")
	}
}

func TestCopyEmptyBufferNoop(t *testing.T) {
	var buf bytes.Buffer
	prev := osc52Out
	osc52Out = &buf
	t.Cleanup(func() { osc52Out = prev })

	m := scrollFixture(t, i18n.LangEN, 0)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)
	if cmd != nil {
		flushCopyCmds(t, cmd)
	}
	if buf.Len() != 0 || m.CopyNotice() != "" {
		t.Fatalf("empty buffer must not copy: buf=%q notice=%q", buf.String(), m.CopyNotice())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
