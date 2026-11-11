package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// TestExternalTextNeverReachesTerminalRaw feeds ESC sequences and line breaks
// through every Twitch field the frame shows. None of them may reach the
// terminal, and the frame keeps its height and width.
func TestExternalTextNeverReachesTerminalRaw(t *testing.T) {
	const (
		w, h  = 80, 24
		name  = "Evil\x1b[2JName\nX"
		login = "evil\x1b]0;pwn\x07login"
		title = "Ti\x1b[2Jtle\r\nnext"
		cat   = "Cat\x1b[2J\negory\t\u009b2J"
	)
	ch := twitch.Channel{
		ID: "1", Login: login, DisplayName: name, Title: twitch.StringPtr(title),
		CategoryName: twitch.StringPtr(cat), Viewers: twitch.IntPtr(10),
		Language:     twitch.StringPtr("pt\x1b[2J\nfoo"),
		Availability: twitch.AvailabilityLive,
	}
	category := twitch.Category{ID: "c", Name: cat, Viewers: twitch.IntPtr(10)}
	cases := []struct {
		name  string
		setup func(m *Model)
	}{
		{"selected channel", func(m *Model) {
			m.list.selectedChannel = &ch
		}},
		{"selected category", func(m *Model) {
			m.list.mode = ModeCategories
			m.list.categories = []twitch.Category{category}
			m.list.selectedChannel = nil
			m.list.selectedCategory = &category
		}},
		{"player", func(m *Model) {
			m.player.login = login
			m.player.state = PlayerStatePlaying
		}},
		{"language menu", func(m *Model) {
			m.list.prefs.menuOpen = true
		}},
		{"language filter hides all", func(m *Model) {
			m.list.prefs.languages = []string{"pt\x1b[2J\nfoo"}
			m.list.prefs.minViewers = 1000
		}},
		{"empty chat", func(m *Model) {
			m.chat.panel.channel = login
			m.chat.panel.state = ChatStateConnected
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangEN))
			m.list.mode = ModeChannels
			m.list.selectedCategory = &category
			m.list.channels = []twitch.Channel{ch}
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: w, Height: h})
			tc.setup(&m)
			rows := strings.Split(m.View(), "\n")
			if len(rows) != h {
				t.Errorf("frame has %d rows, want %d", len(rows), h)
			}
			for i, row := range rows {
				for _, bad := range []string{"\x1b[2J", "\x1b]", "\x07", "\r", "\t", "\u009b"} {
					if strings.Contains(row, bad) {
						t.Errorf("row %d carries %q from Twitch text: %q", i, bad, ansi.Strip(row))
					}
				}
				if got := ansi.StringWidth(row); got > w {
					t.Errorf("row %d is %d cells wide, want <= %d: %q", i, got, w, ansi.Strip(row))
				}
			}
		})
	}
}
