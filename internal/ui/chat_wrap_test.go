package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const longChatBody = "essa jogada foi simplesmente absurda, ninguém esperava o clutch no último round " +
	"e agora a final vai para o mapa decisivo com a torcida inteira de pé"

// withChatMessages replaces the chat buffer with msgs (generation filled in).
func withChatMessages(t *testing.T, m Model, msgs ...ChatMessage) Model {
	t.Helper()
	m = withMessages(t, m, 0)
	for i := range msgs {
		msgs[i].Generation = m.chatPanel.gen
	}
	updated, _ := m.Update(ChatEventMsg{Generation: m.chatPanel.gen, Channel: "gaules", Messages: msgs})
	return updated.(Model)
}

// squash drops all whitespace so text broken across rows can be compared whole.
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// messageBlock returns the chat rows from the one starting with first up to the
// row before the next message (a row whose lead is not the continuation indent).
func messageBlock(t *testing.T, col []string, first string) []string {
	t.Helper()
	start := -1
	for i, row := range col {
		if strings.HasPrefix(strings.TrimSpace(row), first) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no chat row starts with %q:\n%s", first, strings.Join(col, "\n"))
	}
	block := []string{col[start]}
	for _, row := range col[start+1:] {
		if !strings.HasPrefix(row, strings.Repeat(" ", chatContinuationIndent)) || strings.HasPrefix(row, "  > ") {
			break
		}
		block = append(block, row)
	}
	return block
}

func assertRowsFit(t *testing.T, m Model) {
	t.Helper()
	listW, chatW := m.panelWidths()
	for i, row := range strings.Split(ansi.Strip(m.View()), "\n") {
		if w := ansi.StringWidth(row); w > m.width {
			t.Fatalf("row %d overflows %d cols (%d): %q", i, m.width, w, row)
		}
		if chatW == 0 || i == m.height-1 {
			continue
		}
		left, right, ok := strings.Cut(row, panelSeparator)
		if !ok {
			t.Fatalf("row %d lost the panel separator: %q", i, row)
		}
		if w := ansi.StringWidth(left); w != listW {
			t.Fatalf("row %d: separator at col %d, want %d: %q", i, w, listW, row)
		}
		if w := ansi.StringWidth(right); w != chatW {
			t.Fatalf("row %d: chat cell is %d cols, want %d: %q", i, w, chatW, row)
		}
	}
}

func TestLongChatMessageWrapsWholeAt80Columns(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 20)
			m = withChatMessages(t, m, ChatMessage{
				Kind: chat.MessageChat, ID: "long", User: "torcedor", DisplayName: "Torcedor", Body: longChatBody,
			})

			col := chatColumn(t, m)
			block := messageBlock(t, col, "Torcedor:")
			if width == 80 && len(block) < 2 {
				t.Fatalf("long message did not wrap at %d cols:\n%s", width, strings.Join(col, "\n"))
			}
			got := squash(strings.Join(block, " "))
			if want := squash("Torcedor: " + longChatBody); got != want {
				t.Fatalf("message not shown whole:\n got %q\nwant %q\n%s", got, want, strings.Join(col, "\n"))
			}
			if strings.Contains(strings.Join(block, ""), "…") {
				t.Fatalf("message was truncated with an ellipsis:\n%s", strings.Join(block, "\n"))
			}
			if last := col[len(col)-1]; !strings.HasPrefix(last, "  > ") {
				t.Fatalf("input line is not the last chat row: %q", last)
			}
			assertRowsFit(t, m)
		})
	}
}

func TestWrappedEmojiAndCJKKeepColumnsAligned(t *testing.T) {
	body := "🔥🔥🔥 こんにちは世界！これは非常に長い絵文字と漢字のメッセージです 🚀🚀🚀 1234567890 漢字漢字漢字漢字漢字漢字漢字漢字"
	for _, width := range []int{80, 100} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 16)
			m = withChatMessages(t, m, ChatMessage{
				Kind: chat.MessageChat, ID: "cjk", User: "unicodefan", DisplayName: "日本語ユーザー🎌", Body: body,
			})
			assertRowsFit(t, m)

			col := chatColumn(t, m)
			block := messageBlock(t, col, "日本語ユーザー🎌:")
			if len(block) < 2 {
				t.Fatalf("CJK message did not wrap:\n%s", strings.Join(col, "\n"))
			}
			if got, want := squash(strings.Join(block, " ")), squash("日本語ユーザー🎌: "+body); got != want {
				t.Fatalf("CJK message not shown whole:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestChatHeightBudgetKeepsInputAndStatus(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 14)
	msgs := make([]ChatMessage, 0, 40)
	for i := 0; i < 40; i++ {
		msgs = append(msgs, ChatMessage{
			Kind: chat.MessageChat, ID: fmt.Sprintf("l%d", i), User: "u", DisplayName: "User",
			Body: fmt.Sprintf("mensagem %02d %s", i, longChatBody),
		})
	}
	m = withChatMessages(t, m, msgs...)
	m = sendKeys(m, keyTab, keyI)
	m = sendKeys(m, runes("oi")...)

	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if len(rows) != 14 {
		t.Fatalf("View has %d rows, want 14", len(rows))
	}
	if got, want := strings.TrimSpace(rows[13]), strings.TrimSpace(ansi.Strip(m.StatusBar())); got != want || got == "" {
		t.Fatalf("status bar is not the last row: got %q want %q", got, want)
	}
	col := chatColumn(t, m)
	if !strings.Contains(col[0], "Chat — #gaules") {
		t.Fatalf("chat header pushed off the panel: %q", col[0])
	}
	last := col[len(col)-1]
	if !strings.HasPrefix(last, "  > ") || !strings.HasSuffix(last, "oi"+composeCursor) {
		t.Fatalf("input line is not the last chat row: %q\n%s", last, strings.Join(col, "\n"))
	}
	text := strings.Join(col, "\n")
	if !strings.Contains(text, "User: mensagem 39") {
		t.Fatalf("newest message not visible:\n%s", text)
	}
	if strings.Contains(text, "mensagem 00") {
		t.Fatalf("oldest message should have left the screen:\n%s", text)
	}
	if got := len(m.ChatMessages()); got != 40 {
		t.Fatalf("buffer lost messages: %d, want 40", got)
	}
	assertRowsFit(t, m)
}

func TestRejectedSendWrapsWithReasonInBothLanguages(t *testing.T) {
	cases := []struct {
		lang, reason string
	}{
		{i18n.LangEN, "[rejected: slow mode]"},
		{i18n.LangPTBR, "[recusada: modo lento]"},
	}
	for _, tc := range cases {
		for _, width := range []int{80, 100} {
			t.Run(fmt.Sprintf("%s/%d", tc.lang, width), func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				cat, err := i18n.New(tc.lang)
				if err != nil {
					t.Fatalf("i18n.New(%s): %v", tc.lang, err)
				}
				m.catalog = cat
				m = resize(m, width, 16)
				m = withChatMessages(t, m, ChatMessage{
					Kind: chat.MessageChat, ID: "own", User: "pedrosatin", DisplayName: "pedrosatin",
					Body: longChatBody, Delivery: chat.DeliveryRejected, RejectReason: chat.RejectSlowMode,
				})
				col := chatColumn(t, m)
				block := messageBlock(t, col, "pedrosatin:")
				got := squash(strings.Join(block, " "))
				if want := squash("pedrosatin: " + longChatBody + " " + tc.reason); got != want {
					t.Fatalf("rejection not shown whole:\n got %q\nwant %q\n%s", got, want, strings.Join(col, "\n"))
				}
				assertRowsFit(t, m)
			})
		}
	}
}

func TestNarrowChatNoticeReadableInBothLanguages(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, width := range []int{79, 60, 40} {
			t.Run(fmt.Sprintf("%s/%d", lang, width), func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				cat, err := i18n.New(lang)
				if err != nil {
					t.Fatalf("i18n.New(%s): %v", lang, err)
				}
				m.catalog = cat
				m = resize(m, width, 14)
				plain := ansi.Strip(m.View())
				if strings.Contains(plain, "│") || strings.Contains(plain, "Chat — #gaules") {
					t.Fatalf("chat panel shown below 80 cols:\n%s", plain)
				}
				rows := strings.Split(plain, "\n")
				start := -1
				for i, row := range rows {
					if strings.HasPrefix(row, "  ! ") {
						start = i
						break
					}
				}
				if start < 0 {
					t.Fatalf("narrow notice missing:\n%s", plain)
				}
				var notice []string
				for _, row := range rows[start:] {
					if len(notice) > 0 && !strings.HasPrefix(row, strings.Repeat(" ", chatContinuationIndent)) {
						break
					}
					if strings.TrimSpace(row) == "" {
						break
					}
					notice = append(notice, row)
				}
				got := squash(strings.TrimPrefix(strings.Join(notice, " "), "  ! "))
				if want := squash(cat.T("chat.hidden_narrow")); got != want {
					t.Fatalf("notice cut:\n got %q\nwant %q\n%s", got, want, plain)
				}
				for i, row := range rows {
					if w := ansi.StringWidth(row); w > width {
						t.Fatalf("row %d overflows %d cols: %q", i, width, row)
					}
				}
			})
		}
	}
}

func TestActionAndHighlightWrapKeepLeadOnFirstRow(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 20)
	m = withChatMessages(t, m,
		ChatMessage{Kind: chat.MessageChat, ID: "a", User: "acao", DisplayName: "Acao", Body: longChatBody, Action: true},
		ChatMessage{Kind: chat.MessageChat, ID: "h", User: "dest", DisplayName: "Dest", Body: longChatBody, Highlighted: true},
	)
	col := chatColumn(t, m)
	action := messageBlock(t, col, "* Acao")
	if len(action) < 2 || squash(strings.Join(action, " ")) != squash("* Acao "+longChatBody) {
		t.Fatalf("action message not wrapped whole:\n%s", strings.Join(col, "\n"))
	}
	hl := messageBlock(t, col, "★")
	if len(hl) < 2 || !strings.Contains(squash(strings.Join(hl, " ")), squash("Dest: "+longChatBody)) {
		t.Fatalf("highlighted message not wrapped whole:\n%s", strings.Join(col, "\n"))
	}
	assertRowsFit(t, m)
}

func TestWrapChatRowCarriesStylesAcrossRows(t *testing.T) {
	bold := "\x1b[1m"
	content := bold + "nome-bem-comprido-que-quebra" + "\x1b[0m" + ": texto"
	rows := wrapChatRow("  ", content, 16)
	if len(rows) < 2 {
		t.Fatalf("expected a wrap, got %q", rows)
	}
	for i, row := range rows {
		if w := ansi.StringWidth(row); w > 16 {
			t.Fatalf("row %d is %d cols wide: %q", i, w, row)
		}
	}
	if !strings.HasSuffix(rows[0], ansi.ResetStyle) {
		t.Fatalf("open style not closed at the end of the row: %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], strings.Repeat(" ", chatContinuationIndent)+bold) {
		t.Fatalf("style not reopened on the continuation row: %q", rows[1])
	}
	if got := wrapChatRow("  ", "curto", 0); len(got) != 1 || got[0] != "  curto" {
		t.Fatalf("unbounded width should not wrap: %q", got)
	}
}
