package ui

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// chatColumn returns the plain text of the chat panel's cells, one per body row
// (the status bar is excluded), trimmed of trailing padding.
func chatColumn(t *testing.T, m Model) []string {
	t.Helper()
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if len(rows) != m.height {
		t.Fatalf("View has %d rows, want %d", len(rows), m.height)
	}
	out := make([]string, 0, len(rows)-1)
	for i, row := range rows[:len(rows)-1] {
		_, right, ok := strings.Cut(row, panelSeparator)
		if !ok {
			t.Fatalf("row %d has no panel separator: %q", i, row)
		}
		out = append(out, strings.TrimRight(right, " "))
	}
	return out
}

func withMessages(t *testing.T, m Model, n int) Model {
	t.Helper()
	m.chat.panel.buf = chat.NewBuffer(chat.DefaultMaxLines, chat.DefaultMaxBytes)
	m.chat.panel.buf.SetGeneration(m.chat.panel.gen)
	if n == 0 {
		return m
	}
	msgs := make([]ChatMessage, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, ChatMessage{
			Generation: m.chat.panel.gen, Kind: chat.MessageChat,
			ID: fmt.Sprintf("m%d", i), User: "u", DisplayName: "User", Body: fmt.Sprintf("msg %d", i),
		})
	}
	updated, _ := m.Update(ChatEventMsg{Generation: m.chat.panel.gen, Channel: "gaules", Messages: msgs})
	return updated.(Model)
}

func sendKeys(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		updated, _ := m.Update(k)
		m = updated.(Model)
	}
	return m
}

func runes(s string) []tea.KeyMsg {
	out := make([]tea.KeyMsg, 0, len(s))
	for _, r := range s {
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

func resize(m Model, w, h int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

var (
	keyEsc = tea.KeyMsg{Type: tea.KeyEsc}
	keyTab = tea.KeyMsg{Type: tea.KeyTab}
	keyI   = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}
)

func TestComposeCursorOnlyWhileComposing(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 14)

	if strings.Contains(ansi.Strip(m.View()), composeCursor) {
		t.Fatalf("cursor drawn before composing:\n%s", ansi.Strip(m.View()))
	}

	m = sendKeys(m, keyTab, keyI)
	m = sendKeys(m, runes("oi chat")...)
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "oi chat"+composeCursor) {
		t.Fatalf("composing view lacks draft with cursor:\n%s", plain)
	}
	if !strings.Contains(plain, "Enter sends · Esc exits") {
		t.Fatalf("composing view lacks compose hint:\n%s", plain)
	}

	// First Esc leaves composition (focus stays on chat), second returns to the list.
	for step, want := range []Focus{FocusChat, FocusList} {
		m = sendKeys(m, keyEsc)
		if m.Focus() != want || m.Composing() {
			t.Fatalf("Esc #%d: Focus=%q Composing=%v", step+1, m.Focus(), m.Composing())
		}
		plain = ansi.Strip(m.View())
		if strings.Contains(plain, composeCursor) {
			t.Fatalf("Esc #%d: cursor still drawn outside composition:\n%s", step+1, plain)
		}
		if !strings.Contains(plain, "Draft (i resumes): oi chat") {
			t.Fatalf("Esc #%d: draft not marked as draft:\n%s", step+1, plain)
		}
		if strings.Contains(plain, "Enter sends") {
			t.Fatalf("Esc #%d: compose hint shown outside composition:\n%s", step+1, plain)
		}
	}

	// i from the list resumes the same draft.
	m = sendKeys(m, keyI)
	if !m.Composing() || !strings.Contains(ansi.Strip(m.View()), "oi chat"+composeCursor) {
		t.Fatalf("i did not resume draft:\n%s", ansi.Strip(m.View()))
	}
}

func TestChatInputPinnedToPanelBottom(t *testing.T) {
	for _, n := range []int{0, 3, 50} {
		for _, width := range []int{80, 120} {
			for _, composing := range []bool{false, true} {
				name := fmt.Sprintf("msgs=%d/width=%d/composing=%v", n, width, composing)
				t.Run(name, func(t *testing.T) {
					m, _, _ := setupTwoPanelModel(t)
					m = resize(m, width, 20)
					m = withMessages(t, m, n)
					m = sendKeys(m, keyTab, keyI)
					m = sendKeys(m, runes("rascunho")...)
					if !composing {
						m = sendKeys(m, keyEsc)
					}

					col := chatColumn(t, m)
					last := col[len(col)-1]
					if !strings.HasPrefix(last, "  > ") || !strings.Contains(last, "rascunho") {
						t.Fatalf("last chat row is not the input line: %q\n%s", last, strings.Join(col, "\n"))
					}
					if composing && !strings.HasSuffix(last, composeCursor) {
						t.Fatalf("input line lacks cursor at the end: %q", last)
					}
					for i, row := range col[:len(col)-1] {
						if strings.HasPrefix(row, "  > ") {
							t.Fatalf("input line duplicated at row %d: %q", i, row)
						}
					}
					if n == 50 && !strings.Contains(strings.Join(col, "\n"), "msg 49") {
						t.Fatalf("newest message not visible:\n%s", strings.Join(col, "\n"))
					}
				})
			}
		}
	}
}

func TestDraftAndInputPositionSurviveTabResizeAndChannelSwitch(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 16)
	m = sendKeys(m, keyTab, keyI)
	m = sendKeys(m, runes("guardado")...)

	assertBottom := func(stage string) {
		t.Helper()
		if m.ComposeDraft() != "guardado" {
			t.Fatalf("%s: draft=%q", stage, m.ComposeDraft())
		}
		col := chatColumn(t, m)
		if last := col[len(col)-1]; !strings.Contains(last, "guardado") {
			t.Fatalf("%s: last chat row %q is not the input:\n%s", stage, last, strings.Join(col, "\n"))
		}
	}

	m = sendKeys(m, keyTab) // leaves composition, focus to list
	if m.Composing() || m.Focus() != FocusList {
		t.Fatalf("Tab: Composing=%v Focus=%q", m.Composing(), m.Focus())
	}
	assertBottom("after Tab")

	m = resize(m, 79, 16)
	if strings.Contains(ansi.Strip(m.View()), composeCursor) {
		t.Fatalf("cursor drawn with chat hidden:\n%s", ansi.Strip(m.View()))
	}
	m = resize(m, 80, 24)
	assertBottom("after resize 79→80x24")
	m = resize(m, 120, 12)
	assertBottom("after resize 120x12")

	m = sendKeys(m, keyTab, keyI)
	m = resize(m, 100, 18)
	if !m.Composing() {
		t.Fatalf("resize ended composition")
	}
	assertBottom("composing after resize")

	// Channel switch through the list: Tab to the list, j to alanzoka, Enter.
	m = sendKeys(m, keyTab, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ = m.Update(msg)
			m = updated.(Model)
		}
	}
	if m.ChatChannel() != "alanzoka" {
		t.Fatalf("chat channel = %q, want alanzoka", m.ChatChannel())
	}
	if m.ComposeDraft() != "" {
		t.Fatalf("after channel switch: draft=%q, want empty", m.ComposeDraft())
	}

	// Switch back to gaules: draft is restored.
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ = m.Update(msg)
			m = updated.(Model)
		}
	}
	if m.ChatChannel() != "gaules" {
		t.Fatalf("chat channel = %q, want gaules", m.ChatChannel())
	}
	assertBottom("after returning to channel")
}

func TestComposeHintAndCursorFitAt80Columns(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			cat, err := i18n.New(lang)
			if err != nil {
				t.Fatalf("i18n.New(%s): %v", lang, err)
			}
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = cat
			m = resize(m, 80, 14)
			m = sendKeys(m, keyTab, keyI)
			draft := "uma mensagem bem comprida que passa da largura do painel"
			m = sendKeys(m, runes(draft)...)

			hint := cat.T("chat.input.compose_hint")
			draftLabel := strings.TrimSuffix(cat.T("chat.input.draft"), "%s")
			if hint == "chat.input.compose_hint" || draftLabel == "chat.input.draft" {
				t.Fatalf("missing catalog keys for %s", lang)
			}
			counter := fmt.Sprintf(cat.T("chat.input.counter"), utf8.RuneCountInString(draft), twitchMessageRuneLimit)

			col := chatColumn(t, m)
			gotHint := col[len(col)-2]
			if !strings.Contains(gotHint, hint) {
				t.Fatalf("hint row = %q, want to contain %q", gotHint, hint)
			}
			if !strings.HasSuffix(strings.TrimRight(gotHint, " "), counter) {
				t.Fatalf("hint row = %q, want counter suffix %q", gotHint, counter)
			}
			if w := ansi.StringWidth(gotHint); w > 34 {
				// chat column at 80 terminal cols is 34 cells; padOrTruncate may pad, so strip.
				if w = ansi.StringWidth(strings.TrimRight(gotHint, " ")); w > 34 {
					t.Fatalf("hint+counter width %d exceeds chat column 34: %q", w, gotHint)
				}
			}
			last := col[len(col)-1]
			if !strings.HasSuffix(last, "do painel"+composeCursor) {
				t.Fatalf("input line does not keep the tail and cursor visible: %q", last)
			}

			m = sendKeys(m, keyEsc)
			col = chatColumn(t, m)
			last = col[len(col)-1]
			if strings.Contains(last, composeCursor) || !strings.Contains(last, draftLabel) {
				t.Fatalf("inactive draft row = %q, want label %q and no cursor", last, draftLabel)
			}
		})
	}
}

func TestDraftTailKeepsCursorInsideWidth(t *testing.T) {
	prefix := "  > Send to #x: "
	for _, width := range []int{0, 10, 20, 34} {
		got := prefix + draftTail("ação 漢字 emoji 🎉 fim", prefix, width) + composeCursor
		if width > 0 && ansi.StringWidth(got) > width && ansi.StringWidth(prefix)+1 <= width {
			t.Fatalf("width=%d: %q is %d cells", width, got, ansi.StringWidth(got))
		}
	}
}
