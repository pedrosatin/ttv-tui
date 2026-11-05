package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestComposeCounterShortLimitAndOver(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	cases := []struct {
		name  string
		draft string
		want  string
	}{
		{name: "short", draft: "oi", want: "2/500"},
		{name: "exact_limit", draft: strings.Repeat("a", twitchMessageRuneLimit), want: "500/500"},
		{name: "over_limit", draft: strings.Repeat("b", twitchMessageRuneLimit+20), want: "520/500"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = cat
			m = resize(m, 120, 16)
			m = sendKeys(m, keyTab, keyI)
			m.composeDraft = tc.draft

			col := chatColumn(t, m)
			hintRow := col[len(col)-2]
			if !strings.Contains(hintRow, tc.want) {
				t.Fatalf("hint row %q missing counter %q", hintRow, tc.want)
			}
			got := fmt.Sprintf(cat.T("chat.input.counter"), utf8.RuneCountInString(tc.draft), twitchMessageRuneLimit)
			if got != tc.want {
				t.Fatalf("counter label = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestComposeCounterCountsAccentsAndEmoji(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	// 4 letters with accents + space + 1 emoji = 6 runes (🎉 is one code point).
	draft := "ação 🎉"
	wantCount := utf8.RuneCountInString(draft)
	if wantCount != 6 {
		t.Fatalf("fixture rune count = %d, want 6", wantCount)
	}

	m, _, _ := setupTwoPanelModel(t)
	m.catalog = cat
	m = resize(m, 100, 16)
	m = sendKeys(m, keyTab, keyI)
	m = sendKeys(m, runes(draft)...)

	if m.ComposeDraft() != draft {
		t.Fatalf("ComposeDraft=%q want %q", m.ComposeDraft(), draft)
	}
	want := fmt.Sprintf(cat.T("chat.input.counter"), wantCount, twitchMessageRuneLimit)
	col := chatColumn(t, m)
	if !strings.Contains(col[len(col)-2], want) {
		t.Fatalf("hint row %q missing %q", col[len(col)-2], want)
	}
}

func TestComposeCursorVisibleWithLongDraftAt80And120(t *testing.T) {
	draft := strings.Repeat("x", 600)
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("%dcols", width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 20)
			m = sendKeys(m, keyTab, keyI)
			m.composeDraft = draft

			col := chatColumn(t, m)
			last := col[len(col)-1]
			if !strings.Contains(last, composeCursor) {
				t.Fatalf("cursor missing with 600-rune draft at %d cols: %q", width, last)
			}
			if !strings.HasSuffix(strings.TrimRight(last, " "), "x"+composeCursor) &&
				!strings.HasSuffix(strings.TrimRight(last, " "), composeCursor) {
				t.Fatalf("expected draft tail + cursor visible: %q", last)
			}
			if !strings.Contains(last, "…") {
				t.Fatalf("expected leading ellipsis for overflowing draft: %q", last)
			}
			hint := col[len(col)-2]
			if !strings.Contains(hint, "600/500") {
				t.Fatalf("hint row missing 600/500: %q", hint)
			}
			assertAllLinesFit(t, m.View(), width)
		})
	}
}

func TestComposeSendBlockedWhenOverLimit(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m, _, fc := setupTwoPanelModel(t)
	m.catalog = cat
	m = resize(m, 120, 18)
	m = sendKeys(m, keyTab, keyI)
	fc.sent = nil

	over := strings.Repeat("z", twitchMessageRuneLimit+1)
	m.composeDraft = over

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		t.Fatalf("Enter over limit must not return a send cmd")
	}
	if len(fc.sent) != 0 {
		t.Fatalf("send must be blocked, got %+v", fc.sent)
	}
	if m.ComposeDraft() != over {
		t.Fatalf("draft must be kept after blocked send, got %q", m.ComposeDraft())
	}
	if !m.Composing() {
		t.Fatal("must stay in compose mode after blocked send")
	}
	warn := cat.T("chat.warn.too_long")
	if m.ChatSendWarning() != warn {
		t.Fatalf("ChatSendWarning=%q want %q", m.ChatSendWarning(), warn)
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, warn) {
		t.Fatalf("view missing too-long warning:\n%s", plain)
	}
	if !strings.Contains(plain, "501/500") {
		t.Fatalf("view missing over-limit counter:\n%s", plain)
	}

	// Exact limit sends the full body — no silent truncation.
	exact := strings.Repeat("z", twitchMessageRuneLimit)
	m.composeDraft = exact
	m.clearResolvedComposeWarning()
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected send cmd at exact limit")
	}
	if len(fc.sent) != 1 || fc.sent[0].Body != exact {
		t.Fatalf("sent body mismatch: %+v", fc.sent)
	}
	if utf8.RuneCountInString(fc.sent[0].Body) != twitchMessageRuneLimit {
		t.Fatalf("sent body rune count = %d, want %d", utf8.RuneCountInString(fc.sent[0].Body), twitchMessageRuneLimit)
	}
	if m.ComposeDraft() != "" {
		t.Fatalf("draft should clear after successful send, got %q", m.ComposeDraft())
	}
}

func TestComposeFooterAndStatusStayVisibleAt80(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m, _, _ := setupTwoPanelModel(t)
	m.catalog = cat
	m = resize(m, 80, 14)
	m = withMessages(t, m, 40)
	m = sendKeys(m, keyTab, keyI)
	m.composeDraft = strings.Repeat("m", 600)

	view := m.View()
	plain := ansi.Strip(view)
	rows := strings.Split(plain, "\n")
	if len(rows) != 14 {
		t.Fatalf("view rows = %d, want height 14", len(rows))
	}
	assertAllLinesFit(t, view, 80)

	col := chatColumn(t, m)
	last := col[len(col)-1]
	hint := col[len(col)-2]
	if !strings.Contains(hint, "600/500") {
		t.Fatalf("counter missing near input: %q", hint)
	}
	if !strings.Contains(last, composeCursor) {
		t.Fatalf("input/cursor missing from footer: %q", last)
	}
	status := rows[len(rows)-1]
	if strings.TrimSpace(status) == "" {
		t.Fatal("status bar missing")
	}
	if !strings.Contains(last, "…") {
		t.Fatalf("long draft should window with ellipsis: %q", last)
	}
}

func TestFormatComposeHintWithCounterFitsNarrowColumn(t *testing.T) {
	hint := "Enter sends · Esc exits"
	for _, n := range []int{0, 12, 500, 600, 9999} {
		counter := fmt.Sprintf("%d/%d", n, twitchMessageRuneLimit)
		got := formatComposeHintWithCounter(hint, counter, 34)
		if w := ansi.StringWidth(got); w > 34 {
			t.Fatalf("n=%d width %d > 34: %q", n, w, got)
		}
		if !strings.Contains(got, counter) {
			t.Fatalf("n=%d missing counter in %q", n, got)
		}
	}
}

func TestCapturePTY80LongComposeCounter(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m, _, _ := setupTwoPanelModel(t)
	m.catalog = cat
	m = resize(m, 80, 16)
	m = sendKeys(m, keyTab, keyI)
	m.composeDraft = strings.Repeat("w", 600)

	view := m.View()
	printScreen(t, "Compose longo com contador N/500 em 80 colunas", view, 80)

	plain := ansi.Strip(view)
	if !strings.Contains(plain, "600/500") {
		t.Fatalf("PTY 80 capture missing counter 600/500:\n%s", plain)
	}
	if !strings.Contains(plain, composeCursor) {
		t.Fatalf("PTY 80 capture missing compose cursor:\n%s", plain)
	}
	col := chatColumn(t, m)
	last := col[len(col)-1]
	if !strings.Contains(last, "…") || !strings.Contains(last, composeCursor) {
		t.Fatalf("PTY 80 input line should show windowed draft + cursor: %q", last)
	}
}
