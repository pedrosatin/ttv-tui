package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// TestMultiplatformIdentity_IsPlayingAndOffline verifies that channels sharing
// the same login across different platforms (e.g. Twitch xqc vs Kick xqc) do not
// confuse playback state or offline marking.
func TestMultiplatformIdentity_IsPlayingAndOffline(t *testing.T) {
	twitchXQC := catalog.Channel{
		Platform:     platform.Twitch,
		Login:        "xqc",
		DisplayName:  "xQc",
		Availability: catalog.AvailabilityLive,
	}
	kickXQC := catalog.Channel{
		Platform:     platform.Kick,
		Login:        "xqc",
		DisplayName:  "xQc",
		Availability: catalog.AvailabilityLive,
	}

	// 1. isPlaying check:
	// When Twitch xqc is playing (login = "xqc"):
	twitchPlaying := playbackState{login: "xqc", state: PlayerStatePlaying}
	if !twitchPlaying.isPlaying(twitchXQC) {
		t.Error("expected Twitch xqc to be playing when player login is xqc")
	}
	if twitchPlaying.isPlaying(kickXQC) {
		t.Error("Kick xqc must NOT be reported as playing when Twitch xqc is playing")
	}

	// When Kick xqc is playing (login = "kick:xqc"):
	kickPlaying := playbackState{login: "kick:xqc", state: PlayerStatePlaying}
	if !kickPlaying.isPlaying(kickXQC) {
		t.Error("expected Kick xqc to be playing when player login is kick:xqc")
	}
	if kickPlaying.isPlaying(twitchXQC) {
		t.Error("Twitch xqc must NOT be reported as playing when Kick xqc is playing")
	}

	// 2. markChannelOffline check:
	viewersTwitch := 50000
	viewersKick := 15000
	twitchXQC.Viewers = &viewersTwitch
	kickXQC.Viewers = &viewersKick

	m := New(mustCatalog(t, i18n.LangEN))
	m.list.channels = []catalog.Channel{twitchXQC, kickXQC}
	m.list.favorites = []catalog.Channel{twitchXQC, kickXQC}
	m.list.recents = []catalog.Channel{twitchXQC, kickXQC}

	// Marking Twitch xqc offline by its PlayerLabel ("xqc")
	m.markChannelOffline("xqc")

	for _, ch := range m.list.channels {
		if platform.IsTwitch(ch.Platform) {
			if ch.Availability != catalog.AvailabilityOffline || ch.Viewers != nil {
				t.Errorf("Twitch xqc should be offline with nil viewers, got avail=%v viewers=%v", ch.Availability, ch.Viewers)
			}
		} else {
			if ch.Availability != catalog.AvailabilityLive || ch.Viewers == nil || *ch.Viewers != viewersKick {
				t.Errorf("Kick xqc should remain live with %d viewers, got avail=%v viewers=%v", viewersKick, ch.Availability, ch.Viewers)
			}
		}
	}

	// Reset channels
	twitchXQC.Availability = catalog.AvailabilityLive
	twitchXQC.Viewers = &viewersTwitch
	kickXQC.Availability = catalog.AvailabilityLive
	kickXQC.Viewers = &viewersKick
	m.list.channels = []catalog.Channel{twitchXQC, kickXQC}

	// Marking Kick xqc offline by its PlayerLabel ("kick:xqc")
	m.markChannelOffline("kick:xqc")

	for _, ch := range m.list.channels {
		if platform.IsTwitch(ch.Platform) {
			if ch.Availability != catalog.AvailabilityLive || ch.Viewers == nil || *ch.Viewers != viewersTwitch {
				t.Errorf("Twitch xqc should remain live with %d viewers, got avail=%v viewers=%v", viewersTwitch, ch.Availability, ch.Viewers)
			}
		} else {
			if ch.Availability != catalog.AvailabilityOffline || ch.Viewers != nil {
				t.Errorf("Kick xqc should be offline with nil viewers, got avail=%v viewers=%v", ch.Availability, ch.Viewers)
			}
		}
	}
}

// TestLateChatEventDoesNotOverwriteUnsupportedPanel verifies that late Twitch chat events
// do not overwrite the channel header or inject messages into an unsupported chat panel.
func TestLateChatEventDoesNotOverwriteUnsupportedPanel(t *testing.T) {
	fc := &fakeChat{}
	m := New(mustCatalog(t, i18n.LangEN), WithChat(fc))

	// Start Twitch chat on gaules
	m.startChatChannel(catalog.Channel{Login: "gaules"})
	m.chat.panel.gen = 1
	m.chat.panel.state = ChatStateConnected

	// Switch to Kick channel xqc (unsupported chat)
	m.setChatUnsupported(catalog.Channel{Platform: platform.Kick, Login: "xqc"})
	if !m.chat.panel.unsupported || m.chat.panel.channel != "xqc" {
		t.Fatalf("expected unsupported panel for xqc, got: %+v", m.chat.panel)
	}
	oldGen := uint64(1)

	// Simulate late chat events from the previous Twitch session (generation 1, channel gaules)
	lateMsg := ChatEventMsg{
		Generation: oldGen,
		Channel:    "gaules",
		State:      ChatStateConnected,
		Messages: []ChatMessage{
			{Body: "late twitch message from previous channel"},
		},
	}
	updated, _ := m.Update(lateMsg)
	m = updated.(Model)

	if m.chat.panel.channel != "xqc" {
		t.Fatalf("late chat event rewrote panel channel to %q, want %q", m.chat.panel.channel, "xqc")
	}
	if !m.chat.panel.unsupported {
		t.Fatalf("late chat event reset unsupported flag")
	}
	if m.chat.panel.buf != nil && m.chat.panel.buf.Len() > 0 {
		t.Fatalf("late chat event appended messages to unsupported buffer: %d messages", m.chat.panel.buf.Len())
	}

	view := ansi.Strip(m.View())
	if strings.Contains(view, "#gaules") {
		t.Fatalf("view leaked #gaules into Kick live header:\n%s", view)
	}
	// The "#" of IRC belongs to Twitch: a Kick room is named without it.
	if !strings.Contains(view, "Chat — xqc") || strings.Contains(view, "#xqc") {
		t.Fatalf("view does not name the Kick room as xqc in the chat header:\n%s", view)
	}
}

// TestChannelMatches_StableIDComparison verifies matching by stable ID rather than raw URL.
func TestChannelMatches_StableIDComparison(t *testing.T) {
	// Kick kick.com vs www.kick.com match
	kick1 := catalog.Channel{Platform: platform.Kick, Login: "xqc", URL: "https://kick.com/xqc"}
	kick2 := catalog.Channel{Platform: platform.Kick, Login: "xqc", URL: "https://www.kick.com/xqc"}
	kickOther := catalog.Channel{Platform: platform.Kick, Login: "other", URL: "https://kick.com/other"}
	if !channelMatches(kick1, kick2) {
		t.Errorf("channelMatches(kick1, kick2) = false, want true for same Kick channel")
	}
	if channelMatches(kick1, kickOther) {
		t.Errorf("channelMatches(kick1, kickOther) = true, want false for different Kick channel")
	}

	// YouTube case sensitivity
	ytUpper := catalog.Channel{Platform: platform.YouTube, Login: "AbC", URL: "https://youtu.be/AbC"}
	ytLower := catalog.Channel{Platform: platform.YouTube, Login: "abc", URL: "https://youtu.be/abc"}
	ytWatch := catalog.Channel{Platform: platform.YouTube, Login: "AbC", URL: "https://www.youtube.com/watch?v=AbC"}
	if channelMatches(ytUpper, ytLower) {
		t.Errorf("channelMatches(ytUpper, ytLower) = true, want false (YouTube video IDs are case-sensitive)")
	}
	if !channelMatches(ytUpper, ytWatch) {
		t.Errorf("channelMatches(ytUpper, ytWatch) = false, want true for same video ID")
	}

	// Different platforms with same login do not match
	twitchCh := catalog.Channel{Platform: platform.Twitch, Login: "xqc"}
	if channelMatches(twitchCh, kick1) {
		t.Errorf("channelMatches(twitch, kick) = true, want false across platforms")
	}
}

// TestNonTwitchChannel_EmptyURL_ShowsWarning verifies that selecting a non-Twitch
// channel with an empty URL shows an error warning and does not call StartTarget.
func TestNonTwitchChannel_EmptyURL_ShowsWarning(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			cat := mustCatalog(t, lang)
			pb := &fakePlayback{}
			m := New(cat, WithPlayback(pb))

			ch := catalog.Channel{
				Platform:    platform.Kick,
				Login:       "xqc",
				DisplayName: "xQc",
				URL:         "", // Empty URL (e.g. manually edited in state)
			}

			updated, cmd := m.Update(ChannelSelectedMsg{Channel: ch})
			m = updated.(Model)
			if cmd != nil {
				t.Fatalf("cmd = %v, want nil (must not call StartTarget)", cmd)
			}
			if len(pb.started) > 0 {
				t.Fatalf("pb.started = %v, want empty (playback must not start)", pb.started)
			}

			wantWarning := cat.T("player.warn.missing_url")
			view := ansi.Strip(m.View())
			if !strings.Contains(view, wantWarning) {
				t.Fatalf("view missing warning %q:\n%s", wantWarning, view)
			}
		})
	}
}

// TestStatusBar_KickChannelDoesNotDuplicateName verifies that when playing a Kick channel,
// the status bar matches the active channel and does not repeat the login.
func TestStatusBar_KickChannelDoesNotDuplicateName(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangEN))
	ch := catalog.Channel{
		Platform:    platform.Kick,
		Login:       "xqc",
		DisplayName: "xqc",
		URL:         "https://kick.com/xqc",
	}
	m.list.selectedChannel = &ch
	m.player.state = PlayerStatePlaying
	m.player.login = "kick:xqc"

	summary := m.activeContextSummary()
	// Since playerHasLogin is true, chText is empty and login is not appended.
	if strings.Contains(summary, "xqc") {
		t.Fatalf("activeContextSummary duplicated name %q when player is already showing kick:xqc", summary)
	}
}
