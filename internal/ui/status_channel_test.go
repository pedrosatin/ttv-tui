package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestStatusBarKeepsLongChannelNameAt80And120(t *testing.T) {
	channels := []string{"peereira7", "landonorris0481"}
	langs := []string{i18n.LangPTBR, i18n.LangEN}
	widths := []int{80, 120}

	for _, lang := range langs {
		cat := mustCatalog(t, lang)
		for _, ch := range channels {
			for _, width := range widths {
				t.Run(fmt.Sprintf("category_chat/%s/%s/%d", lang, ch, width), func(t *testing.T) {
					m, _, _ := setupTwoPanelModel(t)
					m.catalog = cat
					m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
					m.player.windowNotice = false
					m.player.state = PlayerStateNone
					m.player.login = ""
					m.list.selectedChannel = nil
					m.chat.panel.channel = ch
					m.chat.panel.state = ChatStateConnected
					m.layout.focus = FocusList

					bar := ansi.Strip(m.StatusBar())
					assertStatusWidth(t, bar, width)
					want := fmt.Sprintf(m.t("chat.connected"), ch)
					if !strings.Contains(bar, want) {
						t.Fatalf("status bar at %d cols missing full chat channel %q:\n%q", width, want, bar)
					}
					// Observed bug was "Conectado em #peereir…" — name must stay whole.
					if strings.Contains(bar, "…") && !strings.Contains(bar, "#"+ch) {
						t.Fatalf("channel name truncated before secondary details:\n%q", bar)
					}
					assertAllLinesFit(t, m.View(), width)
				})

				t.Run(fmt.Sprintf("playing_chat/%s/%s/%d", lang, ch, width), func(t *testing.T) {
					m, _, _ := setupTwoPanelModel(t)
					m.catalog = cat
					m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
					m.player.windowNotice = false
					m.player.state = PlayerStatePlaying
					m.player.login = ch
					m.player.volume = 100
					m.list.selectedChannel = &twitch.Channel{Login: ch, DisplayName: ch, Viewers: twitch.IntPtr(1234)}
					m.list.selectedCategory = &twitch.Category{Name: "Counter-Strike", Viewers: twitch.IntPtr(250000)}
					m.chat.panel.channel = ch
					m.chat.panel.state = ChatStateConnected
					m.layout.focus = FocusList

					v := m.View()
					assertAllLinesFit(t, v, width)
					bar := ansi.Strip(m.StatusBar())
					assertStatusWidth(t, bar, width)
					if !strings.Contains(bar, ch) {
						t.Fatalf("playing status at %d cols missing channel %q:\n%q", width, ch, bar)
					}
					// Main player action stays whole (ticket 07).
					main := fmt.Sprintf(m.t("player.playing"), ch)
					if !strings.Contains(bar, main) {
						t.Fatalf("playing main message cut at %d cols:\n%q want %q", width, bar, main)
					}
					if width == 120 {
						// Wider terminals keep secondary detail (volume) when it fits.
						if !strings.Contains(bar, fmt.Sprintf(m.t("player.vol"), 100)) {
							t.Fatalf("120-col playing bar should keep volume:\n%q", bar)
						}
					}
				})
			}
		}
	}
}

func TestStatusBarPlayerPhasesWithLongChatChannel(t *testing.T) {
	ch := "landonorris0481"
	langs := []string{i18n.LangPTBR, i18n.LangEN}

	type phase struct {
		name  string
		apply func(t *testing.T, m Model) Model
		main  func(m Model) string
	}
	phases := []phase{
		{"starting", func(t *testing.T, m Model) Model {
			m.player.windowNotice = false
			m.player.state = PlayerStateStarting
			m.player.login = ch
			m.player.volume = 85
			return m
		}, func(m Model) string { return fmt.Sprintf(m.t("player.starting_channel"), ch) }},
		{"playing", func(t *testing.T, m Model) Model {
			m.player.windowNotice = false
			m.player.state = PlayerStatePlaying
			m.player.login = ch
			m.player.volume = 100
			return m
		}, func(m Model) string { return fmt.Sprintf(m.t("player.playing"), ch) }},
		{"break", func(t *testing.T, m Model) Model {
			m.player.windowNotice = false
			m.player.state = PlayerStateBreak
			m.player.login = ch
			m.player.volume = 85
			m.player.breakElapsed = 145 * time.Second
			return m
		}, func(m Model) string { return fmt.Sprintf(m.t("player.break"), 145) }},
		{"error", func(t *testing.T, m Model) Model {
			return failWith(t, m, 1, PlayerFailureNetwork, "error: Unable to open URL: https://gql.twitch.tv/gql (Connection refused)")
		}, func(m Model) string { return m.t("player.fail.network") }},
	}

	for _, lang := range langs {
		for _, p := range phases {
			t.Run(lang+"/"+p.name, func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				m.catalog = mustCatalog(t, lang)
				m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
				m.chat.panel.channel = ch
				m.chat.panel.state = ChatStateConnected
				m.list.selectedChannel = &twitch.Channel{Login: ch, DisplayName: ch, Viewers: twitch.IntPtr(1234)}
				m.layout.focus = FocusChat
				m = p.apply(t, m)

				bar := ansi.Strip(m.StatusBar())
				assertStatusWidth(t, bar, 80)
				main := p.main(m)
				if !strings.Contains(bar, main) {
					t.Fatalf("main message %q cut at 80 columns:\n%q", main, bar)
				}
				if !strings.Contains(bar, m.t("status.quit_hint")) {
					t.Fatalf("quit hint lost at 80 columns:\n%q", bar)
				}
				// Never show a mid-name chat stub (Conectado em #lando…).
				if strings.Contains(bar, "…") {
					if strings.Contains(bar, "Conectado em #") || strings.Contains(bar, "Connected to #") {
						if !strings.Contains(bar, "#"+ch) && !strings.Contains(bar, ch) {
							t.Fatalf("chat channel truncated mid-name:\n%q", bar)
						}
					}
				}
				assertAllLinesFit(t, m.View(), 80)
			})
		}
	}
}

func TestStatusBarLongChannelPTYCapture80(t *testing.T) {
	catPT := mustCatalog(t, i18n.LangPTBR)
	for _, ch := range []string{"peereira7", "landonorris0481"} {
		t.Run(ch, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = catPT
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 12})
			m.player.windowNotice = false
			m.player.state = PlayerStateNone
			m.player.login = ""
			m.list.selectedChannel = nil
			m.chat.panel.channel = ch
			m.chat.panel.state = ChatStateConnected
			m.layout.focus = FocusList
			printScreen(t, "Status com canal longo #"+ch+" em 80 colunas", m.View(), 80)

			bar := ansi.Strip(m.StatusBar())
			want := fmt.Sprintf(catPT.T("chat.connected"), ch)
			if !strings.Contains(bar, want) {
				t.Fatalf("PTY 80 capture missing full %q:\n%q", want, bar)
			}
		})
	}
}

func assertStatusWidth(t *testing.T, bar string, width int) {
	t.Helper()
	if w := ansi.StringWidth(bar); w != width {
		t.Fatalf("status bar width %d, want %d:\n%q", w, width, bar)
	}
}
