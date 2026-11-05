package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestAuthorBadges(t *testing.T) {
	cases := []struct{ tags, want string }{
		{"moderator/1", "[MOD] "}, {"broadcaster/1", "[STR] "}, {"vip/1", "[VIP] "},
		{"", ""}, {"subscriber/12,premium/1", ""}, {"moderator-fake/1", ""},
		{"vip/1,broadcaster/1,moderator/1", "[MOD] "},
		{"moderator/1,broadcaster/1,vip/1", "[MOD] "},
		{"vip/1,broadcaster/1", "[STR] "}, {"broadcaster/1,vip/1", "[STR] "},
		{"vip/1,moderator/1", "[MOD] "},
	}
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m := joinChatFixture(t, lang)
		for _, tc := range cases {
			t.Run(lang+"/"+tc.tags, func(t *testing.T) {
				ev := chat.ParseLine("@badges=" + tc.tags + ";display-name=Nick :nick!nick@host PRIVMSG #gaules :oi")
				msg := ChatMessage{Kind: chat.MessageChat, User: ev.User, DisplayName: ev.DisplayName, Body: ev.Body, Badges: ev.Badges}
				lead, text := m.formatChatMessageParts(msg, false)
				if got, want := ansi.Strip(lead+text), "  "+tc.want+"Nick: oi"; got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			})
		}
	}
}

func TestAuthorBadgeDistinctFromModerationEvent(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m := joinChatFixture(t, lang)
		author := ChatMessage{Kind: chat.MessageChat, User: "Mod", Body: "oi", Badges: []string{"moderator/1"}}
		lead, text := m.formatChatMessageParts(author, false)
		authorRow := ansi.Strip(lead + text)
		secs := 120
		for _, event := range []ChatMessage{
			m.moderationClearChatMessage(ChatEventMsg{ClearUser: "troll", BanDuration: &secs}),
			m.moderationClearChatMessage(ChatEventMsg{ClearUser: "troll"}),
			m.moderationClearChatMessage(ChatEventMsg{}),
			m.moderationClearMsgMessage(ChatEventMsg{ClearUser: "troll"}),
		} {
			event.Badges = author.Badges // Even tagged system messages must stay events.
			lead, text = m.formatChatMessageParts(event, false)
			eventRow := ansi.Strip(lead + text)
			if authorRow == eventRow || !strings.Contains(authorRow, "[MOD] Mod:") || strings.Contains(authorRow, "[mod]") || !strings.Contains(eventRow, "[mod]") || strings.Contains(eventRow, "[MOD]") {
				t.Fatalf("author %q vs event %q", authorRow, eventRow)
			}
		}
	}
}

func TestAuthorBadgeMessageVariants(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	m.chatPanel.username = "viewer"
	for _, tc := range []struct {
		name, body        string
		action, highlight bool
	}{
		{"normal", "oi", false, false}, {"action", "acena", true, false},
		{"highlight", "oi", false, true}, {"mention", "@viewer oi", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := ChatMessage{Kind: chat.MessageChat, User: "Nick", Body: tc.body, Badges: []string{"moderator/1"}, Action: tc.action, Highlighted: tc.highlight}
			for _, selected := range []bool{false, true} {
				lead, text := m.formatChatMessageParts(msg, selected)
				if !strings.Contains(ansi.Strip(lead+text), "[MOD] Nick") {
					t.Fatalf("badge lost: %q", lead+text)
				}
			}
		})
	}
}

func TestAuthorBadgeWidthAt80Columns(t *testing.T) {
	for _, split := range []bool{false, true} {
		m, _, _ := setupTwoPanelModel(t)
		m = resize(m, 80, 24)
		if !split {
			m.showList, m.showChat = false, true
		}
		_, width := m.panelWidths()
		for _, extra := range []int{0, 1, 6, 90} {
			// Without the badge this fills the column exactly; adding a badge must
			// remove secondary information before wrapping the nick or body.
			body := strings.Repeat("x", width-2-len("Nick: ")+extra)
			msg := ChatMessage{Kind: chat.MessageChat, User: "Nick", Body: body, Badges: []string{"moderator/1"}}
			rows := m.chatMessageRows([]ChatMessage{msg}, 100, width, -1)
			plain := ansi.Strip(strings.Join(rows, "\n"))
			if strings.Contains(plain, "[MOD]") {
				t.Fatalf("badge displaced content at width %d: %q", width, plain)
			}
			if !strings.HasPrefix(plain, "  Nick:") || squash(plain) != squash("Nick: "+body) {
				t.Fatalf("content lost: %q", plain)
			}
			if extra == 0 && len(rows) != 1 {
				t.Fatalf("badge forced a wrap: %q", rows)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("overflow: %q", row)
				}
			}
			m = withChatMessages(t, m, msg)
			assertAllLinesFit(t, ansi.Strip(m.View()), 80)
		}
	}
}

func TestAuthorBadgeNormalizedWidth(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	msg := ChatMessage{Kind: chat.MessageChat, User: "Nick👍🏽", Body: "oi 🫶🏻 👨‍👩‍👧‍👦 café 漢字", Badges: []string{"moderator/1"}}
	m = withChatMessages(t, m, msg)
	view := ansi.Strip(m.View())
	if strings.ContainsAny(view, "\u200d\ufe0f🏽🏻") || !strings.Contains(view, "Nick👍: oi") {
		t.Fatalf("normalization regressed: %s", view)
	}
	assertAllLinesFit(t, view, 80)
	if m.ChatMessages()[0].Body != msg.Body {
		t.Fatal("source mutated")
	}
}

func TestAuthorBadgesPTYEvidence(t *testing.T) {
	m := joinChatFixture(t, i18n.LangPTBR)
	m = resize(m, 80, 18)
	msgs := []ChatMessage{
		{Kind: chat.MessageChat, User: "Equipe", Body: "Sem spam, pessoal", Badges: []string{"moderator/1"}},
		{Kind: chat.MessageChat, User: "Canal", Body: "Bem-vindos!", Badges: []string{"broadcaster/1"}},
		{Kind: chat.MessageChat, User: "Vip", Body: "Boa live!", Badges: []string{"vip/1"}},
		{Kind: chat.MessageChat, User: "Pessoa", Body: "oi"},
		m.moderationClearChatMessage(ChatEventMsg{ClearUser: "troll"}),
	}
	m = withChatMessages(t, m, msgs...)
	view := ansi.Strip(m.View())
	for _, badge := range []string{"[MOD]", "[STR]", "[VIP]", "[mod]"} {
		if !strings.Contains(view, badge) {
			t.Fatalf("missing %s: %s", badge, view)
		}
	}
	assertAllLinesFit(t, view, 80)
	if os.Getenv("TTV_BADGES_PTY") != "1" {
		return
	}
	screen := []string{"Badges de autor | PT-BR | PTY 80x24", ""}
	// Three actual formatter outputs, adjacent, without semantic colour.
	for _, body := range []string{"oi", "orientacao"} {
		columns := make([]string, 3)
		for i, badge := range []string{"moderator/1", "broadcaster/1", "vip/1"} {
			lead, text := m.formatChatMessageParts(ChatMessage{Kind: chat.MessageChat, User: "Nick", Body: body, Badges: []string{badge}}, false)
			columns[i] = fmt.Sprintf("%-26s", ansi.Strip(lead+text))
		}
		screen = append(screen, strings.Join(columns, " "))
	}
	screen = append(screen, "", "Autor [MOD] / evento [mod]; abaixo: View() em 80 colunas")
	screen = append(screen, strings.Split(view, "\n")...)
	if len(screen) != 24 {
		t.Fatalf("capture has %d rows", len(screen))
	}
	fmt.Print("\x1b[2J\x1b[H" + strings.Join(screen, "\r\n"))
}
