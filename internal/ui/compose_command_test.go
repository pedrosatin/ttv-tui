package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const commandWarningKey = "chat.warn.command_blocked"

// composeTyped opens the message input and types draft key by key, so the "/"
// goes through the same path as a real keystroke instead of being assigned.
func composeTyped(t *testing.T, lang string, width int, draft string) (Model, *fakeChat, *i18n.Catalog) {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m, _, fc := setupTwoPanelModel(t)
	m.catalog = cat
	m = resize(m, width, 24)
	m = sendKeys(m, keyTab, keyI)
	fc.sent = nil
	for _, r := range draft {
		if r == ' ' {
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.ComposeDraft() != draft {
		t.Fatalf("typed draft = %q, want %q", m.ComposeDraft(), draft)
	}
	return m, fc, cat
}

func TestComposeBlocksSlashCommandAndKeepsDraft(t *testing.T) {
	cases := []struct {
		name  string
		draft string
	}{
		{name: "ban", draft: "/ban Evasor spam"},
		{name: "short", draft: "/ban x"},
		{name: "leading_spaces", draft: "  /ban x"},
		{name: "prefix_of_me", draft: "/mean x"},
		{name: "bare_slash", draft: "/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, fc, cat := composeTyped(t, i18n.LangEN, 120, tc.draft)
			before := m.chat.panel.buf.Len()

			m, cmd := pressEnter(t, m)

			if cmd != nil {
				t.Fatal("blocked command must not return a send cmd")
			}
			if len(fc.sent) != 0 {
				t.Fatalf("blocked command reached the sender: %+v", fc.sent)
			}
			if got := m.chat.panel.buf.Len(); got != before {
				t.Fatalf("blocked command echoed locally: buffer %d -> %d", before, got)
			}
			if m.ComposeDraft() != tc.draft {
				t.Fatalf("draft = %q, want it kept as %q", m.ComposeDraft(), tc.draft)
			}
			if !m.Composing() {
				t.Fatal("must stay in compose mode so the draft can be fixed")
			}
			if want := cat.T(commandWarningKey); m.ChatSendWarning() != want {
				t.Fatalf("ChatSendWarning = %q, want %q", m.ChatSendWarning(), want)
			}
		})
	}
}

func TestComposeBlockedCommandBeatsTooLongWarning(t *testing.T) {
	m, fc, cat := composeTyped(t, i18n.LangEN, 120, "/ban x")
	m.chat.composeDraft = "/ban " + strings.Repeat("x", twitchMessageRuneLimit)

	m, _ = pressEnter(t, m)

	if len(fc.sent) != 0 {
		t.Fatalf("blocked command reached the sender: %+v", fc.sent)
	}
	if want := cat.T(commandWarningKey); m.ChatSendWarning() != want {
		t.Fatalf("shortening would not help: warning = %q, want %q", m.ChatSendWarning(), want)
	}
}

func TestComposeSendsMeAndMidMessageSlash(t *testing.T) {
	cases := []struct {
		name  string
		draft string
		body  string
	}{
		{name: "me", draft: "/me oi", body: "/me oi"},
		{name: "me_double_space", draft: "/me  texto", body: "/me  texto"},
		{name: "me_leading_space", draft: "  /me oi", body: "/me oi"},
		{name: "fraction", draft: "veja 1/2", body: "veja 1/2"},
		{name: "url_mid_message", draft: "veja http://x/y", body: "veja http://x/y"},
		{name: "url_alone", draft: "http://x/y", body: "http://x/y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, fc, _ := composeTyped(t, i18n.LangEN, 120, tc.draft)

			m, cmd := pressEnter(t, m)

			if cmd == nil {
				t.Fatal("expected a send cmd")
			}
			if len(fc.sent) != 1 || fc.sent[0].Body != tc.body {
				t.Fatalf("sent = %+v, want one message with body %q", fc.sent, tc.body)
			}
			if m.ComposeDraft() != "" {
				t.Fatalf("draft should clear after sending, got %q", m.ComposeDraft())
			}
			if m.ChatSendWarning() != "" {
				t.Fatalf("unexpected warning: %q", m.ChatSendWarning())
			}
		})
	}
}

func TestComposeCommandWarningVisibleAt80InBothLanguages(t *testing.T) {
	squeeze := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, cat := composeTyped(t, lang, 80, "/ban x")

			m, _ = pressEnter(t, m)

			warn := cat.T(commandWarningKey)
			if warn == commandWarningKey {
				t.Fatalf("catalog %s has no %s", lang, commandWarningKey)
			}
			col := chatColumn(t, m)
			if !strings.Contains(squeeze(strings.Join(col, " ")), squeeze(warn)) {
				t.Fatalf("warning %q not fully visible:\n%s", warn, strings.Join(col, "\n"))
			}
			if last := col[len(col)-1]; !strings.Contains(last, "/ban x") {
				t.Fatalf("input line lost the draft: %q", last)
			}
			assertAllLinesFit(t, m.View(), 80)
		})
	}
}

func TestComposeCommandWarningClearsOnceDraftIsFixed(t *testing.T) {
	m, fc, _ := composeTyped(t, i18n.LangEN, 120, "/ban x")
	m, _ = pressEnter(t, m)

	// Still a command after one edit: the warning stays true.
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.ChatSendWarning() == "" {
		t.Fatalf("warning cleared while the draft %q is still a command", m.ComposeDraft())
	}

	m.chat.composeDraft = "/m"
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if m.ChatSendWarning() != "" {
		t.Fatalf("warning outlived the fix: %q (draft %q)", m.ChatSendWarning(), m.ComposeDraft())
	}

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if _, cmd := pressEnter(t, m); cmd == nil {
		t.Fatal("fixed draft must send")
	}
	if len(fc.sent) != 1 || fc.sent[0].Body != "/me o" {
		t.Fatalf("sent = %+v", fc.sent)
	}
}

func TestSlashOutsideComposeIsNotTheChatGuard(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	fc.sent = nil
	m.chat.composeDraft = ""

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if m.Composing() {
		t.Fatal("slash outside compose must not open the message input")
	}
	if !m.Filtering() {
		t.Fatal("slash outside compose must still open the local filter")
	}
	m = sendKeys(m, runes("ban")...)
	m, _ = pressEnter(t, m)

	if len(fc.sent) != 0 {
		t.Fatalf("filter input reached the sender: %+v", fc.sent)
	}
	if m.ChatSendWarning() != "" {
		t.Fatalf("filter must not raise the chat warning: %q", m.ChatSendWarning())
	}
}
