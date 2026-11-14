package ui

import (
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
		{
			name:      "bare @handle",
			input:     "@LofiGirl",
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

func TestFilterBareHandleOpensWhenNoLocalMatch(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{platforms: map[string]catalog.ChatCapabilities{
		platform.YouTube: catalog.ReadOnlyChatCapabilities,
	}}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc))
	m = resize(m, 120, 24)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "@LofiGirl")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
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
