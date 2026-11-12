package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// testOnly is a platform whose chat is read-only, registered in the fake chat.
const testOnly = "testonly"

func newPlatformsChat() *fakeChat {
	return &fakeChat{
		hasToken:  true,
		username:  "tester",
		platforms: map[string]catalog.ChatCapabilities{testOnly: catalog.ReadOnlyChatCapabilities},
	}
}

// selectChannel selects ch as the UI does on Enter and delivers the Join result.
func selectChannel(t *testing.T, m Model, ch catalog.Channel) Model {
	t.Helper()
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: ch})
	return drainChat(t, updated.(Model), cmd)
}

func drainChat(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = drainChat(t, m, c)
		}
	case ChatEventMsg:
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

func chatBodies(m Model) []string {
	var out []string
	for _, msg := range m.ChatMessages() {
		out = append(out, msg.Body)
	}
	return out
}

func chatLine(gen uint64, plat, channel, body string) ChatEventMsg {
	return ChatEventMsg{
		Generation: gen,
		Platform:   plat,
		Channel:    channel,
		State:      ChatStateConnected,
		Messages: []ChatMessage{{
			Generation: gen, Kind: catalog.ChatMessageChat, ReceivedAt: time.Now(),
			User: "viewer", DisplayName: "Viewer", Body: body,
		}},
	}
}

// Twitch -> read-only platform -> Twitch. An event of the first Twitch session
// that shows up late carries a generation that is larger than the panel's when
// the platforms number their own sessions; it must not reach the other
// platform's panel, and the read-only platform's own messages must.
func TestChatPlatformSwitch_LateTwitchEventStaysOutOfOtherPlatformPanel(t *testing.T) {
	fc := newPlatformsChat()
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))
	m = resize(m, 120, 24)

	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "foo"})
	updated, _ := m.Update(chatLine(1, platform.Twitch, "foo", "twitch before"))
	m = updated.(Model)

	m = selectChannel(t, m, catalog.Channel{Platform: testOnly, Login: "foo", ID: "42", URL: "https://example.test/foo"})
	if m.ChatGeneration() != 2 {
		t.Fatalf("generation after joining the read-only chat = %d, want 2", m.ChatGeneration())
	}

	updated, _ = m.Update(chatLine(7, platform.Twitch, "foo", "late twitch line"))
	m = updated.(Model)
	updated, _ = m.Update(chatLine(2, testOnly, "foo", "read-only line"))
	m = updated.(Model)

	got := chatBodies(m)
	if len(got) != 1 || got[0] != "read-only line" {
		t.Fatalf("panel of the read-only chat holds %q, want only its own line", got)
	}
	if m.ChatGeneration() != 2 {
		t.Fatalf("late Twitch event moved the generation to %d", m.ChatGeneration())
	}

	// Back to Twitch: the Twitch chat is the one that shows again.
	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "foo"})
	updated, _ = m.Update(chatLine(3, platform.Twitch, "foo", "twitch again"))
	m = updated.(Model)
	updated, _ = m.Update(chatLine(2, testOnly, "foo", "late read-only line"))
	m = updated.(Model)
	got = chatBodies(m)
	if len(got) != 1 || got[0] != "twitch again" {
		t.Fatalf("panel after returning to Twitch holds %q, want only the Twitch line", got)
	}
}

// The capabilities come with the channel: the input says it cannot send on the
// read-only platform and is back to normal on Twitch.
func TestChatCapabilitiesFollowTheChannel(t *testing.T) {
	cat := mustCatalog(t, i18n.LangEN)
	fc := newPlatformsChat()
	m := New(cat, WithChat(fc))
	m = resize(m, 120, 24)

	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "foo"})
	if !m.ChatCapabilities().CanSend {
		t.Fatal("Twitch chat must allow sending")
	}
	if line := m.renderChatInputLine(80); strings.Contains(line, cat.T("chat.input.unsupported")) {
		t.Fatalf("Twitch input line says it cannot send: %q", line)
	}

	m = selectChannel(t, m, catalog.Channel{Platform: testOnly, Login: "foo", URL: "https://example.test/foo"})
	if m.ChatCapabilities().CanSend {
		t.Fatal("read-only chat must not allow sending")
	}
	if m.chat.panel.unsupported {
		t.Fatal("a platform with a read-only chat has a chat: the panel is not 'unsupported'")
	}
	if line := m.renderChatInputLine(80); !strings.Contains(line, cat.T("chat.input.unsupported")) {
		t.Fatalf("read-only input line %q does not say it cannot send", line)
	}
	if fc.stopped != 0 {
		t.Fatalf("a read-only chat must be joined, not stopped (stopped %d)", fc.stopped)
	}

	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "bar"})
	if !m.ChatCapabilities().CanSend {
		t.Fatal("capabilities did not go back to Twitch's")
	}
	if line := m.renderChatInputLine(80); strings.Contains(line, cat.T("chat.input.unsupported")) {
		t.Fatalf("Twitch input line still says it cannot send: %q", line)
	}
}

// A platform with no chat at all gets the "unsupported" panel and the chat that
// was active is stopped.
func TestChatPlatformWithoutChatIsUnsupported(t *testing.T) {
	fc := newPlatformsChat()
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))
	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "foo"})
	m = selectChannel(t, m, catalog.Channel{Platform: platform.YouTube, ID: "AbC", URL: "https://youtube.com/watch?v=AbC"})
	if !m.chat.panel.unsupported {
		t.Fatal("a platform with no chat must show the unsupported panel")
	}
	if fc.stopped != 1 {
		t.Fatalf("stopped %d chats, want 1", fc.stopped)
	}
}

// The draft of Twitch "foo" and the one of another platform's "foo" are two
// drafts.
func TestChatDraftsDoNotCollideAcrossPlatforms(t *testing.T) {
	fc := newPlatformsChat()
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))
	twitchFoo := catalog.Channel{Platform: platform.Twitch, Login: "foo"}
	otherFoo := catalog.Channel{Platform: testOnly, Login: "foo", URL: "https://example.test/foo"}

	m = selectChannel(t, m, twitchFoo)
	m.chat.composeDraft = "draft of twitch"
	m = selectChannel(t, m, otherFoo)
	if m.chat.composeDraft != "" {
		t.Fatalf("draft of Twitch foo leaked into %s foo: %q", testOnly, m.chat.composeDraft)
	}
	m.chat.composeDraft = "draft of other"
	m = selectChannel(t, m, twitchFoo)
	if m.chat.composeDraft != "draft of twitch" {
		t.Fatalf("draft of Twitch foo = %q", m.chat.composeDraft)
	}
	m = selectChannel(t, m, otherFoo)
	if m.chat.composeDraft != "draft of other" {
		t.Fatalf("draft of %s foo = %q", testOnly, m.chat.composeDraft)
	}
}

// An identifier that is not a Twitch login reaches Join as the channel, with
// its case intact, and names the panel as it is.
func TestChatJoinKeepsIdentifierCase(t *testing.T) {
	fc := newPlatformsChat()
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))

	video := catalog.Channel{Platform: testOnly, ID: "dQw4w9WgXcQ", Login: "dQw4w9WgXcQ", URL: "https://example.test/watch?v=dQw4w9WgXcQ"}
	m = selectChannel(t, m, video)

	if len(fc.joinedChannels) != 1 || fc.joinedChannels[0] != video {
		t.Fatalf("Join got %+v, want the channel untouched: %+v", fc.joinedChannels, video)
	}
	if m.ChatChannel() != "dQw4w9WgXcQ" {
		t.Fatalf("panel channel = %q, want the identifier as is", m.ChatChannel())
	}

	// Twitch keeps normalizing the login for its own panel.
	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "GauLes"})
	if m.ChatChannel() != "gaules" {
		t.Fatalf("Twitch panel channel = %q, want gaules", m.ChatChannel())
	}
}

// Two YouTube identifiers that differ only in case are two rooms: a new session
// of the same video keeps the history, one of the other video starts clean.
func TestChatYouTubeRoomsAreCaseSensitive(t *testing.T) {
	video := catalog.Channel{Platform: platform.YouTube, ID: "AbCdEf", URL: "https://youtube.com/watch?v=AbCdEf"}
	setup := func() Model {
		fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{platform.YouTube: catalog.ReadOnlyChatCapabilities}}
		m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))
		m = selectChannel(t, m, video)
		updated, _ := m.Update(chatLine(1, platform.YouTube, "AbCdEf", "first"))
		return updated.(Model)
	}

	m := setup()
	updated, _ := m.Update(chatLine(2, platform.YouTube, "AbCdEf", "after reconnect"))
	m = updated.(Model)
	if got := chatBodies(m); len(got) == 0 || got[0] != "first" {
		t.Fatalf("a new session of the same video lost the history: %q", got)
	}

	m = setup()
	updated, _ = m.Update(chatLine(2, platform.YouTube, "abcdef", "other video"))
	m = updated.(Model)
	if got := chatBodies(m); len(got) != 1 || got[0] != "other video" {
		t.Fatalf("a session of abcdef kept the history of AbCdEf: %q", got)
	}
}

// MaxMessageLength 0 means no limit, not the Twitch default.
func TestChatZeroMessageLimitMeansNoLimit(t *testing.T) {
	caps := catalog.ChatCapabilities{CanSend: true}
	fc := &fakeChat{hasToken: true, username: "tester", caps: &caps}
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))
	m = selectChannel(t, m, catalog.Channel{Login: "foo"})
	m.chat.panel.state = ChatStateConnected

	m.chat.composeDraft = strings.Repeat("a", 900)
	if m.chat.draftOverLimit() {
		t.Fatal("a draft is over a limit that does not exist")
	}
	_, cmd := m.submitComposeMessage()
	if cmd == nil || len(fc.sent) != 1 {
		t.Fatalf("a long message was not sent: cmd=%v sent=%d", cmd != nil, len(fc.sent))
	}
}
