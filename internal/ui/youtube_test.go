package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func TestYouTubeFailureMessagesPerClass(t *testing.T) {
	cases := []struct {
		failure PlayerFailure
		line    string
		key     string
	}{
		{
			failure: PlayerFailureVOD,
			line:    "[plugins.youtube][error] This plugin does not support VOD content",
			key:     "player.fail.vod",
		},
		{
			failure: PlayerFailureBotCheck,
			line:    "ERROR: [youtube] abc123: Sign in to confirm you're not a bot. This helps protect our community.",
			key:     "player.fail.bot_check",
		},
		{
			failure: PlayerFailureLoginRequired,
			line:    "[plugins.youtube][error] Could not get video info - LOGIN_REQUIRED: Sign in to confirm your age",
			key:     "player.fail.login_required",
		},
	}

	for _, tc := range cases {
		for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
			t.Run(string(tc.failure)+"/"+lang, func(t *testing.T) {
				m, _ := newFailureModel(t, lang, 280, "youtube:jfKfPfyJRdk")
				m = failWith(t, m, 1, tc.failure, tc.line)
				want := CatalogText(m.catalog, tc.key)
				bar := ansi.Strip(m.StatusBar())
				if !strings.Contains(bar, want) {
					t.Fatalf("status bar %q does not contain %q", bar, want)
				}
				if !strings.Contains(bar, tc.line) {
					t.Fatalf("status bar %q does not retain detail %q", bar, tc.line)
				}
			})
		}
	}
}

func TestYouTubeVODFailureMarksChannelOffline(t *testing.T) {
	yt := catalog.Channel{
		Platform:     platform.YouTube,
		ID:           "youtube:jfKfPfyJRdk",
		Login:        "jfKfPfyJRdk",
		DisplayName:  "Lofi Girl",
		URL:          "https://www.youtube.com/watch?v=jfKfPfyJRdk",
		Availability: catalog.AvailabilityLive,
		Viewers:      catalog.IntPtr(12000),
	}
	m := New(mustCatalog(t, i18n.LangEN),
		WithPlayback(&fakePlayback{}),
		WithFavorites([]catalog.Channel{yt}),
		WithRecents([]catalog.Channel{yt}),
	)
	m = resize(m, 120, 24)
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: yt})
	m = drainCmd(t, updated.(Model), cmd)

	m = failWith(t, m, 1, PlayerFailureVOD, "[plugins.youtube][error] This plugin does not support VOD content")
	if got := m.Favorites()[0]; got.Availability != catalog.AvailabilityOffline || got.Viewers != nil {
		t.Fatalf("favorite after VOD failure = %+v, want offline with nil viewers", got)
	}
	if got := m.Recents()[0]; got.Availability != catalog.AvailabilityOffline || got.Viewers != nil {
		t.Fatalf("recent after VOD failure = %+v, want offline with nil viewers", got)
	}
}

func TestYouTubeLoginRequiredDoesNotMarkChannelOffline(t *testing.T) {
	yt := catalog.Channel{
		Platform:     platform.YouTube,
		ID:           "youtube:jfKfPfyJRdk",
		Login:        "jfKfPfyJRdk",
		DisplayName:  "Lofi Girl",
		URL:          "https://www.youtube.com/watch?v=jfKfPfyJRdk",
		Availability: catalog.AvailabilityLive,
		Viewers:      catalog.IntPtr(12000),
	}
	m := New(mustCatalog(t, i18n.LangEN),
		WithPlayback(&fakePlayback{}),
		WithFavorites([]catalog.Channel{yt}),
		WithRecents([]catalog.Channel{yt}),
	)
	m = resize(m, 120, 24)
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: yt})
	m = drainCmd(t, updated.(Model), cmd)

	m = failWith(t, m, 1, PlayerFailureLoginRequired, "[plugins.youtube][error] Could not get video info - LOGIN_REQUIRED: Sign in to confirm your age")
	if got := m.Favorites()[0]; got.Availability != catalog.AvailabilityLive {
		t.Fatalf("favorite after login_required = %+v, want live preserved", got)
	}
	if got := m.Recents()[0]; got.Availability != catalog.AvailabilityLive {
		t.Fatalf("recent after login_required = %+v, want live preserved", got)
	}
}

func TestYouTubeChatErrorReasons(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat := mustCatalog(t, lang)
		for reason, key := range map[catalog.ChatErrorReason]string{
			catalog.ChatErrorVOD:           "chat.err.vod",
			catalog.ChatErrorBotCheck:      "chat.err.bot_check",
			catalog.ChatErrorLoginRequired: "chat.err.login_required",
		} {
			fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
				platform.YouTube: catalog.ReadOnlyChatCapabilities,
			}}
			m := resize(New(cat, WithChat(fc)), 140, 24)
			m = selectChannel(t, m, catalog.Channel{
				Platform: platform.YouTube,
				Login:    "jfKfPfyJRdk",
				URL:      "https://www.youtube.com/watch?v=jfKfPfyJRdk",
			})
			updated, _ := m.Update(ChatEventMsg{
				Generation: 1,
				Platform:   platform.YouTube,
				Channel:    "jfKfPfyJRdk",
				State:      ChatStateError,
				ErrText:    "raw technical error",
				ErrReason:  reason,
			})
			m = updated.(Model)
			want := fmt.Sprintf(CatalogText(cat, key), "YouTube")
			if got := m.chatHeaderState(); !strings.Contains(got, want) {
				t.Errorf("%s/%s: chatHeaderState() = %q, want %q", lang, reason, got, want)
			}
		}
	}
}

func TestYouTubeCaseSensitiveLabelsAndOffline(t *testing.T) {
	upper := catalog.Channel{
		Platform:     platform.YouTube,
		ID:           "youtube:AbCdEfGhIjK",
		Login:        "AbCdEfGhIjK",
		DisplayName:  "Upper Stream",
		URL:          "https://www.youtube.com/watch?v=AbCdEfGhIjK",
		Availability: catalog.AvailabilityLive,
		Viewers:      catalog.IntPtr(500),
	}
	lower := catalog.Channel{
		Platform:     platform.YouTube,
		ID:           "youtube:abcdefghijk",
		Login:        "abcdefghijk",
		DisplayName:  "Lower Stream",
		URL:          "https://www.youtube.com/watch?v=abcdefghijk",
		Availability: catalog.AvailabilityLive,
		Viewers:      catalog.IntPtr(900),
	}

	ps := playbackState{login: "youtube:AbCdEfGhIjK", state: PlayerStatePlaying}
	if !ps.isPlaying(upper) {
		t.Fatal("expected upper YouTube video to match isPlaying")
	}
	if ps.isPlaying(lower) {
		t.Fatal("lower YouTube video differing only in case must NOT match isPlaying")
	}

	m := New(mustCatalog(t, i18n.LangEN))
	m.list.favorites = []catalog.Channel{upper, lower}
	m.markChannelOffline("youtube:AbCdEfGhIjK")
	if m.list.favorites[0].Availability != catalog.AvailabilityOffline {
		t.Fatalf("upper availability = %v, want offline", m.list.favorites[0].Availability)
	}
	if m.list.favorites[1].Availability != catalog.AvailabilityLive {
		t.Fatalf("lower availability changed to %v, want live", m.list.favorites[1].Availability)
	}

	m.player.target = PlayTarget{Channel: lower}
	m.player.login = "youtube:AbCdEfGhIjK"
	if got := m.playerLabel(); got != "youtube:AbCdEfGhIjK" {
		t.Fatalf("playerLabel with mismatched case = %q, want raw qualified label", got)
	}
}

func TestPasteYouTubeURLsAndHandlesInSearch(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStart string
		wantURL   string
		wantJoin  string
	}{
		{
			name:      "watch url",
			input:     "https://www.youtube.com/watch?v=jfKfPfyJRdk",
			wantStart: "youtube:jfKfPfyJRdk",
			wantURL:   "https://www.youtube.com/watch?v=jfKfPfyJRdk",
			wantJoin:  "jfKfPfyJRdk",
		},
		{
			name:      "youtu.be short url",
			input:     "https://youtu.be/AbCdEfGhIjK",
			wantStart: "youtube:AbCdEfGhIjK",
			wantURL:   "https://youtu.be/AbCdEfGhIjK",
			wantJoin:  "AbCdEfGhIjK",
		},
		{
			name:      "live path url",
			input:     "https://www.youtube.com/live/XyZ_123-abc",
			wantStart: "youtube:XyZ_123-abc",
			wantURL:   "https://www.youtube.com/live/XyZ_123-abc",
			wantJoin:  "XyZ_123-abc",
		},
		{
			name:      "channel handle url",
			input:     "https://www.youtube.com/@LofiGirl/live",
			wantStart: "youtube:@LofiGirl",
			wantURL:   "https://www.youtube.com/@LofiGirl/live",
			wantJoin:  "@LofiGirl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pb := &fakePlayback{}
			fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
				platform.YouTube: catalog.ReadOnlyChatCapabilities,
			}}
			m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc), WithStore(&fakeStore{}))
			m = resize(m, 120, 24)

			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, tc.input)
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainAll(t, m, cmd)

			if !slices.Equal(pb.started, []string{tc.wantStart}) {
				t.Fatalf("playback started = %v, want [%s]", pb.started, tc.wantStart)
			}
			if pb.targets[0].Channel.URL != tc.wantURL {
				t.Fatalf("target URL = %q, want %q", pb.targets[0].Channel.URL, tc.wantURL)
			}
			if !slices.Equal(fc.joined, []string{tc.wantJoin}) {
				t.Fatalf("chat joined = %v, want [%s]", fc.joined, tc.wantJoin)
			}
			if m.Composing() || m.ChatCapabilities().CanSend {
				t.Fatal("YouTube chat must be read-only")
			}
		})
	}
}

// A bare @handle is how one calls someone on Twitch and Kick, so the search
// goes to the list's platform and YouTube is only an offer: nothing plays until
// Enter lands on that row.
func TestSearchBareHandleOffersYouTube(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc), WithStore(&fakeStore{}))
	m = resize(m, 120, 24)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "@LofiGirl")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)

	if len(pb.started) != 0 {
		t.Fatalf("playback started = %v before the user chose the YouTube row", pb.started)
	}
	if m.Mode() != ModeSearch || m.SearchQuery() != "@LofiGirl" {
		t.Fatalf("mode = %v, query = %q, want search for @LofiGirl", m.Mode(), m.SearchQuery())
	}
	if !strings.Contains(ansi.Strip(m.View()), "Open @LofiGirl on YouTube") {
		t.Fatalf("search results do not offer YouTube:\n%s", ansi.Strip(m.View()))
	}

	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = drainAll(t, m, cmd)
	if !slices.Equal(pb.started, []string{"youtube:@LofiGirl"}) {
		t.Fatalf("playback started = %v, want [youtube:@LofiGirl]", pb.started)
	}
	if got := pb.targets[0].Channel.URL; got != "https://www.youtube.com/@LofiGirl/live" {
		t.Fatalf("target URL = %q, want the @LofiGirl live URL", got)
	}
	if !slices.Equal(fc.joined, []string{"@LofiGirl"}) {
		t.Fatalf("chat joined = %v, want [@LofiGirl]", fc.joined)
	}
}

// Only a valid handle gets the offer: plain text, a lone @ and a handle
// followed by more words search the platform alone.
func TestSearchOffersYouTubeOnlyForValidHandle(t *testing.T) {
	for _, q := range []string{"LofiGirl", "@", "@Lofi Girl", "@a/b"} {
		t.Run(q, func(t *testing.T) {
			m := New(mustCatalog(t, i18n.LangEN), WithPlayback(&fakePlayback{}))
			m = resize(m, 120, 24)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, q)
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainAll(t, m, cmd)
			if strings.Contains(ansi.Strip(m.View()), "on YouTube") {
				t.Fatalf("search for %q offered YouTube:\n%s", q, ansi.Strip(m.View()))
			}
			for _, it := range m.VisibleItems() {
				if it.Type == ItemTypeYouTubeOffer {
					t.Fatalf("search for %q listed the YouTube offer", q)
				}
			}
		})
	}
}

// The "Open @channel on YouTube" row is the last item of a @channel search on
// both Twitch and Kick. It is not highlighted first, "f" does not favorite it,
// and Enter on it plays the YouTube channel while the list stays on its
// platform.
func TestYouTubeOfferRowOpensChannelFromEachPlatform(t *testing.T) {
	for _, plat := range []string{platform.Twitch, platform.Kick} {
		t.Run(plat, func(t *testing.T) {
			kick := kickCatalog()
			m, w := newScenario(t, 120, 40, WithPlatformCatalog(platform.Kick, kick, kick))
			w.chat.platforms = map[string]catalog.ChatCapabilities{
				platform.Twitch:  catalog.TwitchChatCapabilities,
				platform.YouTube: catalog.ReadOnlyChatCapabilities,
			}
			if plat == platform.Kick {
				m = play(t, m, "p")
			}
			m = play(t, m, "ctrl+f @gaules enter")

			if plat == platform.Twitch && !slices.Equal(w.searched, []string{"gaules"}) {
				t.Fatalf("Twitch searches = %q, want [gaules]", w.searched)
			}
			if plat == platform.Kick && (len(w.searched) != 0 || !slices.Contains(kick.calls(), "search:gaules")) {
				t.Fatalf("Twitch searches = %q, Kick calls = %q, want only the Kick search for gaules", w.searched, kick.calls())
			}

			items := m.VisibleItems()
			if len(items) < 2 {
				t.Fatalf("got %d items, want platform results plus the YouTube offer", len(items))
			}
			last := items[len(items)-1]
			if last.Type != ItemTypeYouTubeOffer || last.Channel == nil {
				t.Fatalf("last item = %+v, want the YouTube offer", last)
			}
			if last.Channel.Platform != platform.YouTube || last.Channel.Login != "@gaules" {
				t.Fatalf("offer channel = %+v, want YouTube @gaules", *last.Channel)
			}
			if got := last.ID(); got != youtubeOfferIDPrefix+"@gaules" {
				t.Fatalf("offer ID = %q, want %q", got, youtubeOfferIDPrefix+"@gaules")
			}
			for _, it := range items[:len(items)-1] {
				if it.Type == ItemTypeYouTubeOffer {
					t.Fatalf("YouTube offer listed before the end: %+v", items)
				}
			}
			if it, ok := m.HighlightedItem(); !ok || it.Type == ItemTypeYouTubeOffer {
				t.Fatalf("highlight = %+v, want a platform result first", it)
			}

			m = play(t, m, "end")
			if it, ok := m.HighlightedItem(); !ok || it.Type != ItemTypeYouTubeOffer {
				t.Fatalf("highlight after End = %+v, want the YouTube offer", it)
			}
			favs := len(m.list.favorites)
			m = play(t, m, "f")
			if len(m.list.favorites) != favs {
				t.Fatal("f favorited the YouTube offer row")
			}
			if len(w.playback.started) != 0 {
				t.Fatalf("playback started = %q before Enter", w.playback.started)
			}

			m = play(t, m, "enter")
			if !slices.Equal(w.playback.started, []string{"youtube:@gaules"}) {
				t.Fatalf("playback started = %q, want [youtube:@gaules]", w.playback.started)
			}
			if got := w.playback.targets[0].Channel.URL; got != "https://www.youtube.com/@gaules/live" {
				t.Fatalf("target URL = %q, want the @gaules live URL", got)
			}
			if !slices.Equal(w.chat.joined, []string{"@gaules"}) {
				t.Fatalf("chat joined = %q, want [@gaules]", w.chat.joined)
			}
			if m.ListPlatform() != plat {
				t.Fatalf("list platform = %q, want it to stay on %q", m.ListPlatform(), plat)
			}
		})
	}
}

// The platform search drops the @ in front, so "@gaules" finds gaules.
func TestPlatformSearchQueryDropsLeadingAt(t *testing.T) {
	for in, want := range map[string]string{
		"@gaules":   "gaules",
		" @gaules ": "gaules",
		"gaules":    "gaules",
		"@":         "@",
		"a@b":       "a@b",
	} {
		if got := platformSearchQuery(in); got != want {
			t.Errorf("platformSearchQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// With no local match, Enter in the filter runs the platform search, as for any
// text; the YouTube offer waits there and never opens on its own.
func TestFilterBareHandleOffersYouTubeWhenNoLocalMatch(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc))
	m = resize(m, 120, 24)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "@LofiGirl")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)

	if len(pb.started) != 0 {
		t.Fatalf("playback started = %v from the filter without a choice", pb.started)
	}
	if !strings.Contains(ansi.Strip(m.View()), "Open @LofiGirl on YouTube") {
		t.Fatalf("filter search does not offer YouTube:\n%s", ansi.Strip(m.View()))
	}
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = drainAll(t, m, cmd)
	if !slices.Equal(pb.started, []string{"youtube:@LofiGirl"}) {
		t.Fatalf("playback started = %v, want [youtube:@LofiGirl]", pb.started)
	}
}

func TestYouTubeChatNeutralizesBidiAndControlChars(t *testing.T) {
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	m := New(mustCatalog(t, i18n.LangPTBR), WithChat(fc))
	m = resize(m, 120, 24)
	m = selectChannel(t, m, catalog.Channel{
		Platform: platform.YouTube,
		Login:    "@LofiGirl",
		URL:      "https://www.youtube.com/@LofiGirl/live",
	})

	updated, _ := m.Update(ChatEventMsg{
		Generation: 1,
		Platform:   platform.YouTube,
		Channel:    "@LofiGirl",
		State:      ChatStateConnected,
		Messages: []ChatMessage{{
			Generation:  1,
			Kind:        catalog.ChatMessageChat,
			ReceivedAt:  time.Now(),
			ID:          "yt-msg-1",
			User:        "evil\u202euser",
			DisplayName: "Evil\x1b[31mUser\u202e",
			Body:        "hello\u202eworld\x07!",
		}},
	})
	m = updated.(Model)
	view := ansi.Strip(m.View())
	if strings.ContainsRune(view, '\u202e') || strings.ContainsRune(view, '\x07') {
		t.Fatalf("chat view retained bidi or control rune:\n%s", view)
	}
	if !strings.Contains(view, "helloworld!") {
		t.Fatalf("chat view missing sanitized body:\n%s", view)
	}
}

func TestYouTubeChatHeaderShowsChannelName(t *testing.T) {
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	m := New(mustCatalog(t, i18n.LangPTBR), WithChat(fc))
	m = resize(m, 120, 24)

	// Without DisplayName, header falls back to video ID and keeps panelKey exact.
	m = selectChannel(t, m, catalog.Channel{
		Platform: platform.YouTube,
		Login:    "jfKfPfyJRdk",
		URL:      "https://www.youtube.com/watch?v=jfKfPfyJRdk",
	})
	if got := m.chat.chatRoomLabel(); got != "jfKfPfyJRdk" {
		t.Fatalf("initial chatRoomLabel() = %q, want jfKfPfyJRdk", got)
	}
	key, ok := m.chat.panelKey()
	if !ok || key.channel != "jfKfPfyJRdk" {
		t.Fatalf("panelKey() = (%+v, %v), want jfKfPfyJRdk", key, ok)
	}

	// ChatEventMsg carrying ChannelName updates header without breaking panelKey.
	updated, _ := m.Update(ChatEventMsg{
		Generation:  1,
		Platform:    platform.YouTube,
		Channel:     "jfKfPfyJRdk",
		ChannelName: "Lofi Girl",
		State:       ChatStateConnected,
	})
	m = updated.(Model)
	if got := m.chat.chatRoomLabel(); got != "Lofi Girl" {
		t.Fatalf("chatRoomLabel() after ChatEventMsg = %q, want Lofi Girl", got)
	}
	if key2, ok := m.chat.panelKey(); !ok || key2.channel != "jfKfPfyJRdk" {
		t.Fatalf("panelKey() mutated to (%+v, %v), want jfKfPfyJRdk", key2, ok)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Chat — Lofi Girl") {
		t.Fatalf("header did not show channel display name:\n%s", view)
	}
	if strings.Contains(view, "Chat — jfKfPfyJRdk") {
		t.Fatalf("header still showed raw video ID:\n%s", view)
	}

	// SavedAvailabilityMsg also populates channelName when lookup resolves DisplayName.
	m = selectChannel(t, m, catalog.Channel{
		Platform: platform.YouTube,
		Login:    "AbCdEfGhIjK",
		URL:      "https://www.youtube.com/watch?v=AbCdEfGhIjK",
	})
	updated, _ = m.Update(SavedAvailabilityMsg{
		ChannelLookup: true,
		Channels: []catalog.Channel{{
			Platform:     platform.YouTube,
			ID:           "AbCdEfGhIjK",
			Login:        "AbCdEfGhIjK",
			DisplayName:  "Chill Beats",
			URL:          "https://www.youtube.com/watch?v=AbCdEfGhIjK",
			Availability: catalog.AvailabilityLive,
		}},
	})
	m = updated.(Model)
	if got := m.chat.chatRoomLabel(); got != "Chill Beats" {
		t.Fatalf("chatRoomLabel() after SavedAvailabilityMsg = %q, want Chill Beats", got)
	}
}

func TestPlainTextSearchStaysOnTwitch(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.Twitch:  catalog.TwitchChatCapabilities,
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	var searched []string
	src := &fakeSource{
		searchFn: func(_ context.Context, q string) (catalog.Page[catalog.Channel], error) {
			searched = append(searched, q)
			return catalog.Page[catalog.Channel]{Items: []catalog.Channel{{
				ID:           "ch-lofi",
				Login:        "lofigirl",
				DisplayName:  "LofiGirl",
				Availability: catalog.AvailabilityLive,
			}}}, nil
		},
	}
	m := New(mustCatalog(t, i18n.LangEN), WithSource(src), WithPlayback(pb), WithChat(fc))
	m = resize(m, 120, 24)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "LofiGirl")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainAll(t, m, cmd)

	if !slices.Equal(searched, []string{"LofiGirl"}) {
		t.Fatalf("Twitch searched = %v, want [LofiGirl]", searched)
	}
	if len(pb.started) != 0 {
		t.Fatalf("plain search unexpectedly started YouTube playback: %v", pb.started)
	}
	if m.ListPlatform() != platform.Twitch {
		t.Fatalf("ListPlatform = %q, want twitch", m.ListPlatform())
	}
}
