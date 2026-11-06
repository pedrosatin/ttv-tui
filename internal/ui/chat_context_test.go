package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestContextFirstMessage(t *testing.T) {
	m := scrollFixture(t, i18n.LangPTBR, 0)
	for _, msg := range []ChatMessage{
		{Kind: chat.MessageChat, FirstMsg: true, User: "Nick", Body: "oi"},
		{Kind: chat.MessageChat, FirstMsg: true, User: "Nick", Body: "oi", Action: true},
		{Kind: chat.MessageChat, FirstMsg: true, User: "Nick", Body: "oi", Highlighted: true},
	} {
		lead, content := m.formatChatMessageParts(msg, false)
		if !strings.Contains(ansi.Strip(lead+content), "[novo]") {
			t.Errorf("missing first-msg marker: %q", lead+content)
		}
	}
}

func TestContextSanitization(t *testing.T) {
	m := scrollFixture(t, i18n.LangPTBR, 0)
	bad := "\u202a\u202b\u202c\u202d\u202e\u034f\U000e0000\U000e0001\U000e007f"
	good := "😀 漢字 café a\u0301"
	msg := ChatMessage{Kind: chat.MessageChat, DisplayName: bad + good, Body: bad + good}
	lead, content := m.formatChatMessageParts(msg, false)
	for _, text := range []string{ansi.Strip(lead + content), formatChatCopyText(msg)} {
		if strings.ContainsAny(text, bad) {
			t.Errorf("unsafe output: %q", text)
		}
		if !strings.Contains(text, good) {
			t.Errorf("Unicode lost: %q", text)
		}
	}
}

func TestContextCopyPayload(t *testing.T) {
	m := scrollFixture(t, i18n.LangPTBR, 0)
	received := time.Date(2026, 11, 5, 14, 23, 0, 0, time.Local)
	m.nowFunc = func() time.Time { return received }
	m = withChatMessages(t, m, ChatMessage{Kind: chat.MessageChat, User: "login", DisplayName: "Nick", Body: "olá 😀 漢字", ReceivedAt: received})
	forceOSCClipboardEnv(t)
	var out bytes.Buffer
	old := osc52Out
	osc52Out = &out
	t.Cleanup(func() { osc52Out = old })
	m.nowFunc = func() time.Time { return received.Add(24 * time.Hour) }
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	_ = flushCopy(t, updated.(Model), cmd)
	got, err := decodeOSC52Payload(out.String())
	if err != nil {
		t.Fatal(err)
	}
	want := "canal #gaules · 2026-11-05 14:23 · Nick: olá 😀 漢字"
	if got != want {
		t.Fatalf("payload = %q, want %q", got, want)
	}
}

func TestContextYOnList(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m.focus = FocusList
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !strings.Contains(strings.ToLower(ansi.Strip(m.View())), "focus the chat") {
		t.Fatalf("missing copy warning: %s", ansi.Strip(m.View()))
	}
}

func TestContextBlockedSlashInstruction(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		m, _, _ := composeTyped(t, lang, 120, "/r/golang")
		m, _ = pressEnter(t, m)
		instruction := "remove the leading slash"
		if lang == i18n.LangPTBR {
			instruction = "remova a barra inicial"
		}
		if !strings.Contains(strings.ToLower(m.ChatSendWarning()), instruction) {
			t.Errorf("%s: %s", lang, m.ChatSendWarning())
		}
	}
}

// contextPTYModel avoids catalogue I/O while exercising the real Tea renderer
// and Update loop with a received-message fixture and a scripted y key.
type contextPTYModel struct{ Model }

func (m contextPTYModel) Init() tea.Cmd {
	return tea.Sequence(
		tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}} }),
		tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return tea.QuitMsg{} }),
	)
}
func (m contextPTYModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	m.Model = next.(Model)
	return m, cmd
}
func TestContextPTY(t *testing.T) {
	m := scrollFixture(t, i18n.LangPTBR, 0)
	received := time.Date(2026, 11, 5, 14, 23, 0, 0, time.Local)
	m.nowFunc = func() time.Time { return received }
	m = withChatMessages(t, m, ChatMessage{Kind: chat.MessageChat, FirstMsg: true, ReceivedAt: received, DisplayName: "N\u202eick", Body: "Olá 😀 漢字 café\u034f\U000e0000"})
	forceOSCClipboardEnv(t)
	var clipboard bytes.Buffer
	prev := osc52Out
	osc52Out = &clipboard
	t.Cleanup(func() { osc52Out = prev })
	output := io.Discard
	if os.Getenv("TTV_CONTEXT_PTY") == "1" {
		output = os.Stdout
	}
	program := tea.NewProgram(contextPTYModel{m}, tea.WithInput(nil), tea.WithOutput(output), tea.WithoutSignalHandler())
	final, err := program.Run()
	if err != nil {
		t.Fatal(err)
	}
	result := final.(contextPTYModel)
	payload, err := decodeOSC52Payload(clipboard.String())
	if err != nil {
		t.Fatal(err)
	}
	want := "canal #gaules · 2026-11-05 14:23 · Nick: Olá 😀 漢字 café"
	if payload != want {
		t.Fatalf("payload=%q, want %q", payload, want)
	}
	if !strings.Contains(ansi.Strip(result.View()), "[novo] Nick: Olá 😀 漢字 café") {
		t.Fatal("new message missing in rendered screen")
	}
	if _, err := fmt.Fprintln(output, "COPY:", payload); err != nil {
		t.Fatal(err)
	}
	t.Logf("clipboard OSC52 decoded: %s", payload)
}
