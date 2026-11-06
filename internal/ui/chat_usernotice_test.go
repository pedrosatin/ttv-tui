package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func userNotice(msgID, name, systemMsg, body string) ChatMessage {
	return ChatMessage{
		Generation:  1,
		Kind:        chat.MessageSystem,
		UserNotice:  true,
		ID:          "n-" + msgID,
		User:        strings.ToLower(name),
		DisplayName: name,
		MsgID:       msgID,
		SystemMsg:   systemMsg,
		Body:        body,
	}
}

func deliver(t *testing.T, m Model, items ...ChatMessage) Model {
	t.Helper()
	updated, _ := m.Update(ChatEventMsg{Generation: 1, Channel: "gaules", Messages: items})
	return updated.(Model)
}

// renderedRow returns the row the panel builds for the newest message, without
// styles: what a colorblind reader or NO_COLOR terminal gets.
func renderedRow(m Model) string {
	msgs := m.ChatMessages()
	lead, text := m.formatChatMessageParts(msgs[len(msgs)-1], false)
	return ansi.Strip(lead + text)
}

func TestUserNotice_KindsRenderAsSystemLines(t *testing.T) {
	cases := []struct {
		name      string
		msgID     string
		systemMsg string
		body      string
		wantBody  string
	}{
		{"sub", "sub", "Nova subscribed at Tier 1.", "", "Nova subscribed at Tier 1."},
		{"subgift", "subgift", "Santa gifted a Tier 1 sub to Kid!", "", "Santa gifted a Tier 1 sub to Kid!"},
		{"submysterygift", "submysterygift", "Santa is gifting 5 Tier 1 Subs to Gaules's community!", "", "Santa is gifting 5 Tier 1 Subs to Gaules's community!"},
		{"raid", "raid", "1234 raiders from Raider have joined!", "", "1234 raiders from Raider have joined!"},
		{"announcement with system-msg", "announcement", "Equipe avisa", "Sorteio as 20h", "Equipe avisa — Sorteio as 20h"},
		{"unknown kind", "viewermilestone", "Fan watched 5 consecutive streams!", "boa", "Fan watched 5 consecutive streams! — boa"},
	}
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, tc := range cases {
			t.Run(lang+"/"+tc.name, func(t *testing.T) {
				m := deliver(t, joinChatFixture(t, lang), userNotice(tc.msgID, "Autor", tc.systemMsg, tc.body))
				msgs := m.ChatMessages()
				if len(msgs) != 1 {
					t.Fatalf("got %d messages: %+v", len(msgs), msgs)
				}
				// Twitch text is content: it stays verbatim in every UI language.
				if msgs[0].Kind != chat.MessageSystem || msgs[0].Body != tc.wantBody {
					t.Fatalf("message = %+v, want system line %q", msgs[0], tc.wantBody)
				}
				view := strings.Join(strings.Fields(ansi.Strip(m.View())), " ")
				if !strings.Contains(view, tc.wantBody) {
					t.Fatalf("View() missing %q:\n%s", tc.wantBody, m.View())
				}
			})
		}
	}
}

func TestUserNotice_SystemLineHasMarkerAndNoAuthor(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m := joinChatFixture(t, lang)
		notice := userNotice("announcement", "ModOne", "", "Sorteio as 20h")
		notice.Badges = []string{"moderator/1"}
		notice.Color = "#FF0000"
		row := renderedRow(deliver(t, m, notice))
		if !strings.HasPrefix(row, "  "+m.t("chat.system_prefix")+" ") {
			t.Fatalf("row %q does not start with the system marker", row)
		}
		if strings.Contains(row, m.t("chat.author.mod")) {
			t.Fatalf("row %q carries an author badge, want a system line", row)
		}

		sub := userNotice("sub", "Nova", "Nova subscribed at Tier 1.", "")
		sub.Badges = []string{"moderator/1"}
		if got, want := renderedRow(deliver(t, m, sub)), "  "+m.t("chat.system_prefix")+" Nova subscribed at Tier 1."; got != want {
			t.Fatalf("row = %q, want %q", got, want)
		}
	}
}

func TestUserNotice_RaidIsDistinctWithoutColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m := joinChatFixture(t, lang)
			raid := renderedRow(deliver(t, m, userNotice("raid", "Raider", "1234 raiders from Raider have joined!", "")))
			if want := "★ [raid] 1234 raiders from Raider have joined!"; raid != want {
				t.Fatalf("raid row = %q, want %q", raid, want)
			}

			// The same text without the raid msg-id must not borrow the marker.
			for _, other := range []string{"sub", "unraid", "viewermilestone", ""} {
				row := renderedRow(deliver(t, m, userNotice(other, "Raider", "1234 raiders from Raider have joined!", "")))
				if strings.Contains(row, "[raid]") || strings.Contains(row, "★") {
					t.Fatalf("msg-id %q row %q carries the raid marker", other, row)
				}
			}
		})
	}
}

func TestUserNotice_RaidLeadKeepsSelectionMarker(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), userNotice("raid", "Raider", "1234 raiders!", ""))
	m.focus = FocusChat
	msgs := m.ChatMessages()
	lead, text := m.formatChatMessageParts(msgs[0], true)
	if got, want := ansi.Strip(lead+text), "▌★[raid] 1234 raiders!"; got != want {
		t.Fatalf("selected raid row = %q, want %q", got, want)
	}
}

func TestUserNotice_RaidSurvivesEightyColumns(t *testing.T) {
	m := joinChatFixture(t, i18n.LangPTBR)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = deliver(t, updated.(Model), userNotice("raid", "Raider", "1234 raiders from Raider have joined the channel just now!", ""))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "★ [raid] ") {
		t.Fatalf("raid marker missing at 80 columns:\n%s", view)
	}
	for _, row := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(row); w > 80 {
			t.Fatalf("row %q is %d cells wide", row, w)
		}
	}
}

func TestUserNotice_EmptySystemMsgUsesLocalFallback(t *testing.T) {
	cases := []struct {
		msgID string
		body  string
		en    string
		ptBR  string
	}{
		{"sub", "", "Autor subscribed", "Autor se inscreveu"},
		{"subgift", "", "Subscription gift from Autor", "Presente de inscrição de Autor"},
		{"submysterygift", "", "Subscription gift from Autor", "Presente de inscrição de Autor"},
		{"raid", "", "Raid from Autor", "Raid de Autor"},
		{"announcement", "", "Announcement from Autor", "Anúncio de Autor"},
		{"announcement", "Sorteio as 20h", "Announcement from Autor: Sorteio as 20h", "Anúncio de Autor: Sorteio as 20h"},
		{"resub", "", "Autor resubscribed", "Autor renovou a inscrição"},
		{"viewermilestone", "", "Channel event (viewermilestone)", "Evento do canal (viewermilestone)"},
		{"", "", "Channel event", "Evento do canal"},
		{"", "oi", "Channel event: oi", "Evento do canal: oi"},
	}
	for _, tc := range cases {
		for lang, want := range map[string]string{i18n.LangEN: tc.en, i18n.LangPTBR: tc.ptBR} {
			for _, systemMsg := range []string{"", "   "} {
				t.Run(lang+"/"+tc.msgID+"/"+tc.body, func(t *testing.T) {
					m := deliver(t, joinChatFixture(t, lang), userNotice(tc.msgID, "Autor", systemMsg, tc.body))
					if got := m.ChatMessages()[0].Body; got != want {
						t.Fatalf("Body = %q, want %q", got, want)
					}
				})
			}
		}
	}
}

func TestUserNotice_FallbackWithoutAnyName(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	notice := userNotice("raid", "", "", "")
	if got, want := deliver(t, m, notice).ChatMessages()[0].Body, "Raid from ?"; got != want {
		t.Fatalf("Body = %q, want %q", got, want)
	}
}

func TestUserNotice_ResubAppearsOnce(t *testing.T) {
	cases := []struct {
		name      string
		systemMsg string
		months    int
		body      string
		want      string
	}{
		{"twitch text with message", "CoolSub subscribed for 15 months!", 15, "Valeu pela live", "CoolSub subscribed for 15 months! — Valeu pela live"},
		{"twitch text alone", "CoolSub subscribed for 15 months!", 15, "", "CoolSub subscribed for 15 months!"},
		{"local fallback with message", "", 15, "Valeu pela live", "CoolSub resubscribed (15 months): Valeu pela live"},
		{"local fallback alone", "", 15, "", "CoolSub resubscribed (15 months)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notice := userNotice("resub", "CoolSub", tc.systemMsg, tc.body)
			notice.Months = tc.months
			m := deliver(t, joinChatFixture(t, i18n.LangEN), notice)
			msgs := m.ChatMessages()
			if len(msgs) != 1 || msgs[0].Body != tc.want {
				t.Fatalf("messages = %+v, want one line %q", msgs, tc.want)
			}
			view := strings.Join(strings.Fields(ansi.Strip(m.View())), " ")
			if got := strings.Count(view, "CoolSub"); got != 1 {
				t.Fatalf("CoolSub appears %d times in the view:\n%s", got, m.View())
			}
		})
	}
}

func TestUserNotice_NormalMessageStaysChat(t *testing.T) {
	m := joinChatFixture(t, i18n.LangEN)
	cases := []struct {
		name string
		msg  ChatMessage
		want string
	}{
		{"plain", ChatMessage{Kind: chat.MessageChat, User: "alice", DisplayName: "Alice", Body: "oi"}, "  Alice: oi"},
		{"typed marker", ChatMessage{Kind: chat.MessageChat, User: "alice", DisplayName: "Alice", Body: "[raid] 1234 raiders"}, "  Alice: [raid] 1234 raiders"},
		{"msg-id on PRIVMSG", ChatMessage{Kind: chat.MessageChat, User: "alice", DisplayName: "Alice", MsgID: "raid", Body: "oi"}, "  Alice: oi"},
		{"badge survives", ChatMessage{Kind: chat.MessageChat, User: "alice", DisplayName: "Alice", Badges: []string{"moderator/1"}, Body: "oi"}, "  [MOD] Alice: oi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.msg.Generation = 1
			got := deliver(t, m, tc.msg)
			msgs := got.ChatMessages()
			if last := msgs[len(msgs)-1]; last.Kind != chat.MessageChat || last.Body != tc.msg.Body {
				t.Fatalf("message rewritten: %+v", last)
			}
			if row := renderedRow(got); row != tc.want {
				t.Fatalf("row = %q, want %q", row, tc.want)
			}
		})
	}
}

// A system line that did not come from USERNOTICE keeps its body: only marked
// messages go through the renderer.
func TestUserNotice_UnmarkedSystemLineIsNotRewritten(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), ChatMessage{Generation: 1, Kind: chat.MessageSystem, Body: "gaules timed out for 10s"})
	if got := m.ChatMessages()[0].Body; got != "gaules timed out for 10s" {
		t.Fatalf("Body = %q", got)
	}
}

func TestUserNotice_ControlSequencesNeverReachTheScreen(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), userNotice("raid", "Raider", "1234\x1b[2J raiders\n\x1b]0;pwn\x07joined", "\x1b[31mtexto"))
	row := renderedRow(m)
	if strings.ContainsAny(row, "\x1b\n\x07") {
		t.Fatalf("row kept control bytes: %q", row)
	}
	if want := "★ [raid] 1234 raiders joined — texto"; row != want {
		t.Fatalf("row = %q, want %q", row, want)
	}
}

func TestUserNotice_CopyCarriesTextWithoutMarker(t *testing.T) {
	m := deliver(t, joinChatFixture(t, i18n.LangEN), userNotice("raid", "Raider", "1234 raiders from Raider have joined!", ""))
	if got, want := formatChatCopyText(m.ChatMessages()[0]), "1234 raiders from Raider have joined!"; got != want {
		t.Fatalf("copy = %q, want %q", got, want)
	}
}

// Every key the fallback reads must exist in both catalogs with the same verbs,
// and be translated unless the audit lists it as identical on purpose.
func TestUserNotice_LocalMessagesHaveLanguageParity(t *testing.T) {
	keys := []string{
		"chat.raid_prefix",
		"chat.usernotice.sub",
		"chat.usernotice.resub",
		"chat.usernotice.resub_months",
		"chat.usernotice.gift",
		"chat.usernotice.raid",
		"chat.usernotice.announcement",
		"chat.usernotice.event",
		"chat.usernotice.event_id",
		"chat.usernotice.with_text",
	}
	verbs := regexp.MustCompile(`%[a-z]`)
	en, ptBR := mustCatalog(t, i18n.LangEN), mustCatalog(t, i18n.LangPTBR)
	for _, key := range keys {
		enValue, ptValue := en.T(key), ptBR.T(key)
		if enValue == key || ptValue == key || enValue == "" || ptValue == "" {
			t.Fatalf("%s: en=%q pt-BR=%q", key, enValue, ptValue)
		}
		if a, b := verbs.FindAllString(enValue, -1), verbs.FindAllString(ptValue, -1); strings.Join(a, " ") != strings.Join(b, " ") {
			t.Fatalf("%s verbs differ: en=%v pt-BR=%v", key, a, b)
		}
	}
}
