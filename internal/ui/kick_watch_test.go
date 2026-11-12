package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// platformResolver records the identifiers it was asked about and answers with
// the channels it was given.
type platformResolver struct {
	mu     sync.Mutex
	asked  [][]string
	answer []catalog.Channel
	err    error
}

func (r *platformResolver) ChannelsByIDs(_ context.Context, ids []string) ([]catalog.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, slices.Clone(ids))
	return r.answer, r.err
}

func (r *platformResolver) calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.asked)
}

// drainAll runs cmd and nested batches, feeding every message back to the model.
func drainAll(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			m = drainAll(t, m, c)
		}
	default:
		updated, next := m.Update(msg)
		m = drainAll(t, updated.(Model), next)
	}
	return m
}

func liveKick(login string, viewers int, title, category string) catalog.Channel {
	return catalog.Channel{
		ID: "kick:" + login, Login: login, DisplayName: login, Platform: platform.Kick,
		Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(viewers),
		Title: catalog.StringPtr(title), CategoryName: catalog.StringPtr(category),
	}
}

// The saved channels of each platform go to that platform's resolver and to no
// other: "xqc" is one channel on Twitch and another on Kick.
func TestHydrate_AsksEachPlatformItsOwnChannels(t *testing.T) {
	twitch := &platformResolver{answer: []catalog.Channel{
		{ID: "1", Login: "xqc", Availability: catalog.AvailabilityOffline},
	}}
	kick := &platformResolver{answer: []catalog.Channel{liveKick("xqc", 900, "kick title", "Slots")}}
	favs := []catalog.Channel{
		{ID: "1", Login: "xqc", DisplayName: "xQc", Platform: platform.Twitch},
		{ID: "kick:xqc", Login: "xqc", DisplayName: "xqc", Platform: platform.Kick, URL: "https://kick.com/xqc"},
		{ID: "kick:XQC", Login: "XQC", Platform: platform.Kick, URL: "https://kick.com/XQC"},
		{ID: "youtube:@LofiGirl", Login: "@LofiGirl", Platform: platform.YouTube, URL: "https://www.youtube.com/@LofiGirl/live"},
	}
	recents := []catalog.Channel{{ID: "2", Login: "gaules", Platform: platform.Twitch}}
	m := New(mustCatalog(t, i18n.LangEN),
		WithResolver(twitch), WithPlatformResolver(platform.Kick, kick),
		WithFavorites(favs), WithRecents(recents))

	m = drainAll(t, m, m.Init())

	if got := twitch.calls(); len(got) != 1 || !slices.Equal(got[0], []string{"xqc", "gaules"}) {
		t.Fatalf("Twitch resolver asked %v, want one call with [xqc gaules]", got)
	}
	if got := kick.calls(); len(got) != 1 || !slices.Equal(got[0], []string{"xqc"}) {
		t.Fatalf("Kick resolver asked %v, want one call with [xqc]", got)
	}

	// Same login, two platforms, two answers: each lands on its own row.
	if got := m.list.favorites[0]; got.Availability != catalog.AvailabilityOffline || got.Viewers != nil {
		t.Errorf("Twitch xqc = %+v, want offline", got)
	}
	if got := m.list.favorites[1]; got.Availability != catalog.AvailabilityLive || got.Viewers == nil || *got.Viewers != 900 || got.Title == nil || *got.Title != "kick title" {
		t.Errorf("Kick xqc = %+v, want live with 900 viewers and its title", got)
	}
	// A platform with no resolver keeps what it had.
	if got := m.list.favorites[3]; got.Availability != catalog.AvailabilityUnknown {
		t.Errorf("YouTube channel = %+v, want unverified", got)
	}
}

// A blocked Kick lookup must not cost the Twitch channels their state.
func TestHydrate_OnePlatformFailingKeepsTheOthers(t *testing.T) {
	twitch := &platformResolver{answer: []catalog.Channel{
		{ID: "2", Login: "gaules", Availability: catalog.AvailabilityLive, Viewers: catalog.IntPtr(5)},
	}}
	kick := &platformResolver{err: &catalog.Error{Platform: platform.Kick, Kind: catalog.Bloqueio, StatusCode: 403}}
	m := New(mustCatalog(t, i18n.LangEN),
		WithResolver(twitch), WithPlatformResolver(platform.Kick, kick),
		WithFavorites([]catalog.Channel{
			{ID: "2", Login: "gaules", Platform: platform.Twitch},
			{ID: "kick:gaules", Login: "gaules", Platform: platform.Kick, URL: "https://kick.com/gaules"},
		}))

	m = drainAll(t, m, m.Init())

	if got := m.list.favorites[0]; got.Availability != catalog.AvailabilityLive {
		t.Errorf("Twitch gaules = %+v, want live", got)
	}
	if got := m.list.favorites[1]; got.Availability != catalog.AvailabilityUnknown {
		t.Errorf("Kick gaules = %+v, want unverified after the block", got)
	}
	if m.list.err != nil {
		t.Errorf("a failed availability lookup surfaced as a list error: %v", m.list.err)
	}

	// Every platform failing is an error the message carries, still without a list error.
	twitch.err = errors.New("down")
	cmd := m.fetchHydrateCmd(m.list.hydrateGen)
	msg, ok := cmd().(SavedAvailabilityMsg)
	if !ok || msg.Err == nil || len(msg.Channels) != 0 {
		t.Fatalf("all lookups failing = %+v, want an error and no channels", msg)
	}
}

func TestHydrate_NothingToAskIssuesNoRequest(t *testing.T) {
	kick := &platformResolver{}
	m := New(mustCatalog(t, i18n.LangEN), WithPlatformResolver(platform.Kick, kick),
		WithFavorites([]catalog.Channel{{ID: "1", Login: "gaules", Platform: platform.Twitch}}))
	if cmd := m.fetchHydrateCmd(1); cmd != nil {
		t.Fatal("a Twitch favorite with only a Kick resolver must issue no request")
	}
	if cmd := m.fetchChannelStateCmd(catalog.Channel{Platform: platform.YouTube, Login: "abc"}); cmd != nil {
		t.Fatal("a channel of a platform with no resolver must issue no request")
	}
	// Removing a resolver leaves the copies that hold the old map untouched.
	before := m
	m.setResolver(platform.Kick, nil)
	if len(before.resolvers) != 1 || len(m.resolvers) != 0 {
		t.Fatalf("resolvers before %d after %d, want 1 and 0", len(before.resolvers), len(m.resolvers))
	}
}

// The state lookup of a pasted channel is not a hydration round: a reload of
// the list (a mode change, "r") while it runs must not discard its answer.
func TestChannelStateLookup_SurvivesAListReload(t *testing.T) {
	kick := &platformResolver{answer: []catalog.Channel{liveKick("gaules", 3600, "kick title", "Project Zomboid")}}
	m := New(mustCatalog(t, i18n.LangEN), WithPlatformResolver(platform.Kick, kick))
	pasted := catalog.Channel{ID: "kick:gaules", Login: "gaules", Platform: platform.Kick, URL: "https://kick.com/gaules"}
	m.list.selectedChannel = &pasted

	cmd := m.fetchChannelStateCmd(pasted)
	if cmd == nil {
		t.Fatal("no lookup for a Kick channel with a Kick resolver")
	}
	m.list.hydrateGen++
	updated, _ := m.Update(cmd())
	m = updated.(Model)

	got := m.list.selectedChannel
	if got == nil || got.Availability != catalog.AvailabilityLive || got.Viewers == nil || *got.Viewers != 3600 {
		t.Fatalf("selected channel = %+v, want live with 3600 viewers", got)
	}
}

// Pasting a Kick URL opens the channel with the chat next to it, read-only,
// and the bar shows the channel by name with what the site says about it.
func TestPasteKickURL_OpensWithReadOnlyChatAndChannelState(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			cat := mustCatalog(t, lang)
			pb := &fakePlayback{}
			fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{platform.Kick: catalog.ReadOnlyChatCapabilities}}
			kick := &platformResolver{answer: []catalog.Channel{liveKick("gaules", 3681, "GAMEPLAY DO ALASTRO", "Project Zomboid")}}
			m := New(cat, WithPlayback(pb), WithChat(fc), WithStore(&fakeStore{}), WithPlatformResolver(platform.Kick, kick))
			m = resize(m, 140, 24)

			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, "https://kick.com/gaules")
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainAll(t, m, cmd)

			if len(fc.joined) != 1 || fc.joined[0] != "gaules" || fc.joinedChannels[0].Platform != platform.Kick {
				t.Fatalf("chat joined %v (%+v), want the Kick channel gaules", fc.joined, fc.joinedChannels)
			}
			if got := kick.calls(); len(got) != 1 || !slices.Equal(got[0], []string{"gaules"}) {
				t.Fatalf("Kick resolver asked %v, want [gaules] once", got)
			}
			if sel := m.list.selectedChannel; sel == nil || sel.Availability != catalog.AvailabilityLive || sel.Viewers == nil || *sel.Viewers != 3681 {
				t.Fatalf("selected channel = %+v, want live with 3681 viewers", sel)
			}
			// The recents row carries the same evidence once the list shows it.
			if got := m.list.pendingRecents; len(got) != 1 || got[0].Availability != catalog.AvailabilityLive || got[0].Title == nil {
				t.Fatalf("pending recents = %+v, want the live channel with its title", got)
			}

			updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "kick:gaules", Volume: 100})
			m = updated.(Model)
			updated, _ = m.Update(chatLine(1, platform.Kick, "gaules", "salve"))
			m = updated.(Model)
			// The one-time notice about the mpv window would take the lead.
			m.player.windowNotice = false

			bar := ansi.Strip(m.StatusBar())
			if want := fmt.Sprintf(cat.T("player.playing"), "gaules (Kick)"); !strings.Contains(bar, want) {
				t.Errorf("status bar %q does not say %q", bar, want)
			}
			if strings.Contains(bar, "kick:gaules") {
				t.Errorf("status bar shows the raw session label: %q", bar)
			}
			if !strings.Contains(bar, "Project Zomboid") {
				t.Errorf("status bar %q does not show the category", bar)
			}

			view := ansi.Strip(m.View())
			if want := fmt.Sprintf(cat.T("chat.title"), "gaules"); !strings.Contains(view, want) || strings.Contains(view, "#gaules") {
				t.Errorf("chat header does not name the room %q without the IRC #:\n%s", want, view)
			}
			if !strings.Contains(view, "salve") {
				t.Errorf("chat message missing:\n%s", view)
			}
			if want := fmt.Sprintf(cat.T("chat.input.read_only"), "Kick"); !strings.Contains(view, want) {
				t.Errorf("input line does not say %q:\n%s", want, view)
			}
			if strings.Contains(view, cat.T("chat.state.connected_anon")) {
				t.Errorf("a chat with no login to offer says anonymous:\n%s", view)
			}
			if strings.Contains(view, cat.T("chat.unsupported")) {
				t.Errorf("a platform with a chat shows the unsupported panel:\n%s", view)
			}

			// i does not open the message input.
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
			if m.Composing() {
				t.Error("compose mode opened on a read-only chat")
			}
		})
	}
}

// A narrow chat column gets the short read-only text, whole.
func TestReadOnlyInputLine_ShortFormWhenNarrow(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := mustCatalog(t, lang)
		m := New(cat)
		m.chat.panel = newChatPanel()
		m.chat.panel.platform, m.chat.panel.channel = platform.Kick, "gaules"
		m.chat.panel.caps = catalog.ReadOnlyChatCapabilities
		long := fmt.Sprintf(cat.T("chat.input.read_only"), "Kick")
		short := fmt.Sprintf(cat.T("chat.input.read_only_short"), "Kick")
		if line := ansi.Strip(m.renderChatInputLine(len("  > ") + ansi.StringWidth(long))); !strings.Contains(line, long) {
			t.Errorf("%s: line %q, want the long text when it fits", lang, line)
		}
		line := ansi.Strip(m.renderChatInputLine(24))
		if !strings.Contains(line, short) || ansi.StringWidth(line) > 24 {
			t.Errorf("%s: line %q at 24 columns, want %q whole", lang, line, short)
		}
	}
}

func TestChatRoomLabel_HashOnlyOnTwitch(t *testing.T) {
	tests := []struct{ plat, channel, want string }{
		{platform.Twitch, "gaules", "#gaules"},
		{"", "gaules", "#gaules"},
		{platform.Kick, "gaules", "gaules"},
		{platform.YouTube, "@LofiGirl", "@LofiGirl"},
		{platform.Kick, "", ""},
	}
	for _, tc := range tests {
		c := chatState{panel: chatPanel{platform: tc.plat, channel: tc.channel}}
		if got := c.chatRoomLabel(); got != tc.want {
			t.Errorf("chatRoomLabel(%q, %q) = %q, want %q", tc.plat, tc.channel, got, tc.want)
		}
		if got := c.copyRoomLabel(); got != tc.want {
			t.Errorf("copyRoomLabel(%q, %q) = %q, want %q", tc.plat, tc.channel, got, tc.want)
		}
	}
}

// A chat failure with a reason is explained in the language of the UI, naming
// the platform; one without a reason keeps the text of the error.
func TestChatErrorReasons_AreExplained(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := mustCatalog(t, lang)
		for reason, key := range map[catalog.ChatErrorReason]string{
			catalog.ChatErrorBlocked:    "chat.err.blocked",
			catalog.ChatErrorNoRoom:     "chat.err.no_room",
			catalog.ChatErrorKeyRefused: "chat.err.key_refused",
		} {
			fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{platform.Kick: catalog.ReadOnlyChatCapabilities}}
			m := resize(New(cat, WithChat(fc)), 140, 24)
			m = selectChannel(t, m, catalog.Channel{Platform: platform.Kick, Login: "gaules", URL: "https://kick.com/gaules"})
			updated, _ := m.Update(ChatEventMsg{
				Generation: 1, Platform: platform.Kick, Channel: "gaules", State: ChatStateError,
				ErrText: "kick channel [Bloqueio] (HTTP 403): request refused by the site", ErrReason: reason,
			})
			m = updated.(Model)
			want := fmt.Sprintf(cat.T(key), "Kick")
			view := ansi.Strip(m.View())
			// The panel wraps a long reason; the header state holds it whole.
			if got := m.chatHeaderState(); !strings.Contains(got, want) {
				t.Errorf("%s/%s: chat state %q does not say %q:\n%s", lang, reason, got, want, view)
			}
			if first := strings.Fields(want)[1]; !strings.Contains(view, "Kick "+first) {
				t.Errorf("%s/%s: the reason is not on screen:\n%s", lang, reason, view)
			}
			if strings.Contains(view, "Bloqueio") {
				t.Errorf("%s/%s: the raw error reached the screen:\n%s", lang, reason, view)
			}
		}
	}

	m := resize(New(mustCatalog(t, i18n.LangEN), WithChat(&fakeChat{})), 140, 24)
	m = selectChannel(t, m, catalog.Channel{Platform: platform.Twitch, Login: "gaules"})
	updated, _ := m.Update(ChatEventMsg{Generation: 1, Channel: "gaules", State: ChatStateError, ErrText: "dial tcp: connection refused"})
	if view := ansi.Strip(updated.(Model).View()); !strings.Contains(view, "connection refused") {
		t.Errorf("an error with no reason lost its text:\n%s", view)
	}
}

func TestPlayerLabel(t *testing.T) {
	tests := []struct {
		name  string
		ch    catalog.Channel
		login string
		want  string
	}{
		{"twitch keeps the login", catalog.Channel{Platform: platform.Twitch, Login: "gaules", DisplayName: "Gaules"}, "gaules", "gaules"},
		{"kick by url", catalog.Channel{Platform: platform.Kick, Login: "gaules", DisplayName: "gaules", URL: "https://kick.com/gaules"}, "kick:gaules", "gaules (Kick)"},
		{"kick recording names the channel", catalog.Channel{Platform: platform.Kick, Login: "xqc/videos/0a1b", DisplayName: "xqc"}, "kick:xqc/videos/0a1b", "xqc (Kick)"},
		{"youtube", catalog.Channel{Platform: platform.YouTube, Login: "@LofiGirl", DisplayName: "@LofiGirl"}, "youtube:@LofiGirl", "@LofiGirl (YouTube)"},
		{"unknown host keeps its name", catalog.Channel{Platform: "vimeo.com", Login: "123", DisplayName: "123"}, "vimeo.com:123", "123 (vimeo.com)"},
		{"no display name falls back to the id", catalog.Channel{Platform: platform.Kick, Login: "gaules"}, "kick:gaules", "gaules (Kick)"},
		{"label of another channel is left alone", catalog.Channel{Platform: platform.Kick, Login: "gaules"}, "kick:xqc", "kick:xqc"},
		{"nothing playing", catalog.Channel{}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangEN))
			m.player.target = PlayTarget{Channel: tc.ch}
			m.player.login = tc.login
			if got := m.playerLabel(); got != tc.want {
				t.Errorf("playerLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBlockedErrorsHaveTheirOwnText(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := mustCatalog(t, lang)
		m := New(cat)
		m.player.state, m.player.failure = PlayerStateError, PlayerFailureBlocked
		if got := m.playerFailureText(); got != cat.T("player.fail.blocked") || strings.Contains(got, cat.T("player.fail.network")) {
			t.Errorf("%s: playerFailureText = %q, want the blocked text", lang, got)
		}
	}
}

// The short "#login" form is a Twitch thing. A Kick room has no "#", so its
// bare name would match the player status and take its place when the bar runs
// out of room, or trail the bar as a stray word.
func TestStatusBar_RoomNameDoesNotReplaceThePlayerStatus(t *testing.T) {
	cat := mustCatalog(t, i18n.LangEN)
	for width := 80; width <= 200; width += 5 {
		fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{platform.Kick: catalog.ReadOnlyChatCapabilities}}
		kick := &platformResolver{answer: []catalog.Channel{liveKick("gaules", 3681, "GAMEPLAY DO ALASTRO", "Project Zomboid")}}
		m := New(cat, WithPlayback(&fakePlayback{}), WithChat(fc), WithPlatformResolver(platform.Kick, kick))
		m = resize(m, width, 24)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
		m = typeRunes(m, "https://kick.com/gaules")
		m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = drainAll(t, m, cmd)
		// The first playing state brings the one-time notice about the mpv
		// window, which pushes the player status into the parts that may not fit.
		updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "kick:gaules", Volume: 100})
		m = updated.(Model)
		updated, _ = m.Update(chatLine(1, platform.Kick, "gaules", "salve"))
		m = updated.(Model)

		bar := ansi.Strip(m.StatusBar())
		assertStatusWidth(t, bar, width)
		state := strings.SplitN(bar, "|", 3)
		for _, part := range strings.Split(state[len(state)-2], statusSep) {
			if strings.TrimSpace(part) == "gaules" {
				t.Errorf("width %d: the bare room name is a part of the bar: %q", width, bar)
			}
		}
	}
}
