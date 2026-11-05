package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestComposedEmojiListAlignment(t *testing.T) {
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := New(nil)
			m.width, m.height, m.mode, m.loading = width, 24, ModeChannels, false
			m.showList, m.showChat = true, false
			title := "🫶🏻 " + strings.Repeat("live ", 30)
			m.channels = []twitch.Channel{
				{ID: "1", Login: "first", DisplayName: "First", Title: &title, Viewers: twitch.IntPtr(12)},
				{ID: "2", Login: "second", DisplayName: "Second", Viewers: twitch.IntPtr(8)},
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
			line := text + strings.Repeat("x", width)
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
	m.width, m.height, m.loading = 80, 24, false
	m.categories = []twitch.Category{{ID: "cat", Name: "Categoria👨‍👩‍👧‍👦"}}
	if !strings.Contains(ansi.Strip(m.View()), "Categoria👨") || strings.Contains(m.View(), "\u200d") {
		t.Fatal("category not normalized")
	}
	if m.categories[0].Name != "Categoria👨‍👩‍👧‍👦" {
		t.Fatal("source mutated")
	}
	m.mode = ModeSearch
	m.searchQuery = "Busca🫶🏻"
	m.searchCategories = m.categories
	view := m.View()
	if strings.ContainsAny(view, "🏻\u200d") || !strings.Contains(view, "Busca🫶") {
		t.Fatal("search not normalized")
	}
	m.channelFilterMenu = true
	m.channels = []twitch.Channel{{ID: "1", Login: "user", Language: twitch.StringPtr("pt👍🏽")}}
	if strings.Contains(m.View(), "🏽") {
		t.Fatal("language menu not normalized")
	}
	// Copy and outgoing message data retain the original Unicode sequence.
	if sanitizeChatText("🫶🏻 👨‍👩‍👧‍👦") != "🫶🏻 👨‍👩‍👧‍👦" {
		t.Fatal("non-display sanitizer changed")
	}
}
