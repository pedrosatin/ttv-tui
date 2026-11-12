package ui

import (
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

func TestComposedEmojiListAlignment(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := New(nil)
			m.width, m.height, m.list.mode, m.list.loading = width, 24, ModeChannels, false
			m.layout.showList, m.layout.showChat = true, false
			title := "🫶🏻 " + strings.Repeat("live ", 30)
			m.list.channels = []catalog.Channel{
				{ID: "1", Login: "first", DisplayName: "First", Title: &title, Viewers: catalog.IntPtr(12)},
				{ID: "2", Login: "second", DisplayName: "Second", Viewers: catalog.IntPtr(8)},
			}
			view := ansi.Strip(m.View())
			rows := strings.Split(view, "\n")
			if len(rows) != 24 {
				t.Fatalf("rows=%d, want 24", len(rows))
			}
			for i, row := range rows {
				normalized := strings.ReplaceAll(row, "🏻", "")
				if strings.Contains(row, "🏻") {
					t.Errorf("row %d retains skin modifier: %q", i, row)
				}
				if ansi.StringWidth(normalized) > width || (hasWideRune(row) && ansi.StringWidth(normalized) >= width) {
					t.Errorf("row %d overflows", i)
				}
				if ansi.StringWidth(row) != ansi.StringWidth(normalized) {
					t.Errorf("row %d measured before normalization", i)
				}
			}
			if !strings.Contains(rows[1], "First") || !strings.Contains(rows[2], "Second") {
				t.Fatalf("list order/alignment lost:\n%s", view)
			}
			if width == 80 && os.Getenv("TTV_WIDTH_PTY") == "1" {
				fmt.Print("\x1b[2J\x1b[H" + strings.ReplaceAll(view, "\n", "\r\n"))
			}
		})
	}
}

func TestComposedEmojiChat(t *testing.T) {
	for _, width := range []int{80, 120} {
		m, _, _ := setupTwoPanelModel(t)
		m = resize(m, width, 24)
		m = withChatMessages(t, m, ChatMessage{Kind: chat.MessageChat, ID: "emoji", User: "user", DisplayName: "Camila👍🏽", Body: "oi 🫶🏻 👨‍👩‍👧‍👦 ❤️ café 漢字"})
		view := ansi.Strip(m.View())
		if strings.ContainsAny(view, "\u200d\ufe0f🏽🏻") {
			t.Fatalf("composed emoji reached screen:\n%s", view)
		}
		if !strings.Contains(view, "Camila👍: oi 🫶 👨 ❤ café 漢字") {
			t.Fatalf("normalized chat missing:\n%s", view)
		}
		if len(strings.Split(view, "\n")) != 24 {
			t.Fatal("unexpected row count")
		}
		assertAllLinesFit(t, view, width)
	}
}

func TestNormalizeScreenText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"🫶🏻", "🫶"}, {"👍🏽", "👍"}, {"👨‍👩‍👧‍👦", "👨"}, {"❤️", "❤"},
		{"❤︎", "❤"}, {"👩🏿‍💻!", "👩!"}, {"👍🏻👍🏼👍🏽👍🏾👍🏿", "👍👍👍👍👍"},
		{"漢字 日本語 中文", "漢字 日本語 中文"}, {"café ação", "café ação"}, {"plain text", "plain text"},
		{"", ""}, {"x\u200d", "x"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeScreenText(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWideScreenLineHasSpareCell(t *testing.T) {
	for _, width := range []int{80, 120} {
		for _, text := range []string{"🫶", "漢"} {
			line := padOrTruncateVisual(text, width)
			got := fitScreenLines(line, width)
			if ansi.StringWidth(got) != width-1 {
				t.Fatalf("got %d cells, want %d", ansi.StringWidth(got), width-1)
			}
		}
		ascii := strings.Repeat("x", width)
		if fitScreenLines(ascii, width) != ascii {
			t.Fatal("ASCII changed")
		}
	}
}

func TestScreenInputsNormalizeWithoutMutatingSource(t *testing.T) {
	m := New(nil)
	m.width, m.height, m.list.loading = 80, 24, false
	m.list.categories = []catalog.Category{{ID: "cat", Name: "Categoria👨‍👩‍👧‍👦"}}
	if !strings.Contains(ansi.Strip(m.View()), "Categoria👨") || strings.Contains(m.View(), "\u200d") {
		t.Fatal("category not normalized")
	}
	if m.list.categories[0].Name != "Categoria👨‍👩‍👧‍👦" {
		t.Fatal("source mutated")
	}
	m.list.mode = ModeSearch
	m.list.search.query = "Busca🫶🏻"
	m.list.search.categories = m.list.categories
	view := m.View()
	if strings.ContainsAny(view, "🏻\u200d") || !strings.Contains(view, "Busca🫶") {
		t.Fatal("search not normalized")
	}
	m.list.prefs.menuOpen = true
	m.list.channels = []catalog.Channel{{ID: "1", Login: "user", Language: catalog.StringPtr("pt👍🏽")}}
	if strings.Contains(m.View(), "🏽") {
		t.Fatal("language menu not normalized")
	}
	// Copy and outgoing message data retain the original Unicode sequence.
	if sanitizeChatText("🫶🏻 👨‍👩‍👧‍👦") != "🫶🏻 👨‍👩‍👧‍👦" {
		t.Fatal("non-display sanitizer changed")
	}
}

func TestEmojiInListDoesNotTruncateChatURL(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	title := "🔥 " + strings.Repeat("x", 20)
	m.list.channels = []catalog.Channel{
		{ID: "1", Login: "streamer", DisplayName: "streamer", Title: &title, Viewers: catalog.IntPtr(10)},
	}
	// With chatW = 34, lead "  " (2 cells) and nick "u: " (3 cells), a 29-cell URL fills the chat column.
	url := "https://example.com/123456789"
	m = withChatMessages(t, m, ChatMessage{
		Kind:        chat.MessageChat,
		ID:          "m1",
		User:        "u",
		DisplayName: "u",
		Body:        url,
	})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, url) {
		t.Fatalf("chat URL truncated in split view with emoji in list:\n%s", view)
	}
}

func TestSinhalaWithZWJPreservesText(t *testing.T) {
	sinhala := "\u0dc1\u0dca\u200d\u0dbb\u0dd3" // "ශ්‍රී" with U+200D
	got := normalizeScreenText(sinhala)
	if !strings.Contains(got, "ර") {
		t.Fatalf("normalizeScreenText(%q) = %q, want consonant ර (U+0DBB) preserved", sinhala, got)
	}
}

func TestPanelSeparatorColumnAlignedWithWideRunesInList(t *testing.T) {
	for _, width := range []int{80, 100} {
		t.Run(fmt.Sprintf("%dx24", width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 24)
			title1 := "🔥 " + strings.Repeat("x", 20)
			title2 := "日本語 " + strings.Repeat("x", 20)
			m.list.channels = []catalog.Channel{
				{ID: "1", Login: "streamer1", DisplayName: "streamer1", Title: &title1, Viewers: catalog.IntPtr(10)},
				{ID: "2", Login: "streamer2", DisplayName: "streamer2", Title: &title2, Viewers: catalog.IntPtr(20)},
				{ID: "3", Login: "streamer3", DisplayName: "streamer3", Viewers: catalog.IntPtr(5)},
			}
			url := "https://example.com/123456789"
			m = withChatMessages(t, m, ChatMessage{
				Kind:        chat.MessageChat,
				ID:          "m1",
				User:        "u",
				DisplayName: "u",
				Body:        url,
			})
			view := ansi.Strip(m.View())
			rows := strings.Split(view, "\n")
			sepCol := -1
			for i, row := range rows {
				idx := strings.Index(row, "│")
				if idx == -1 {
					continue
				}
				col := ansi.StringWidth(row[:idx])
				if sepCol == -1 {
					sepCol = col
				} else if col != sepCol {
					t.Fatalf("row %d has separator at col %d, want col %d:\n%s", i, col, sepCol, view)
				}
			}
			if sepCol == -1 {
				t.Fatal("no separator found in view")
			}
		})
	}
}

// A filter typed with emoji does not go through fitListRow. Its row still
// leaves the spare cell when the chat side ends in blank space, and the
// separator keeps its column.
func TestFilterRowWithWideRunesKeepsSpareCell(t *testing.T) {
	for _, width := range []int{80, 100} {
		t.Run(fmt.Sprintf("%dx24", width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 24)
			m.list.filter = strings.Repeat("🔥", 40)
			rows := strings.Split(ansi.Strip(m.View()), "\n")
			found := false
			sepCol := -1
			for i, row := range rows {
				if idx := strings.Index(row, "│"); idx != -1 {
					col := ansi.StringWidth(row[:idx])
					if sepCol == -1 {
						sepCol = col
					} else if col != sepCol {
						t.Fatalf("row %d has separator at col %d, want %d", i, col, sepCol)
					}
				}
				if !strings.Contains(row, "🔥") {
					continue
				}
				found = true
				if got := ansi.StringWidth(row); got >= width {
					t.Errorf("row %d with emoji filter is %d cells wide, want < %d: %q", i, got, width, row)
				}
			}
			if !found {
				t.Fatal("filter row not rendered")
			}
		})
	}
}
