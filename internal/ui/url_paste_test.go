package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
)

func TestPasteTwitchURLInSearchOpensChannelWithChat(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{}
	fs := &fakeStore{}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc), WithStore(fs))

	// Open search with Ctrl+F
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !m.Searching() || cmd != nil {
		t.Fatalf("expected search to be active, searching=%v", m.Searching())
	}

	// Type/paste Twitch channel URL
	m = typeRunes(m, "https://www.twitch.tv/gaules")
	m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, m, cmd)

	if m.Searching() {
		t.Fatalf("search prompt should be closed after submitting URL")
	}

	// Should not have switched to search listing mode; instead directly opened channel
	if m.Mode() == ModeSearch {
		t.Fatalf("mode should not be ModeSearch for URL paste, got %q", m.Mode())
	}

	// Check player was started with Twitch login
	if len(pb.started) != 1 || pb.started[0] != "gaules" {
		t.Fatalf("playback started = %v, want [gaules]", pb.started)
	}

	// Chat was started for gaules
	if len(fc.joined) != 1 || fc.joined[0] != "gaules" {
		t.Fatalf("chat joined = %v, want [gaules]", fc.joined)
	}

	// Recents contain the channel
	recents := m.list.toStoreState().Recents
	if len(recents) != 1 || recents[0].Login != "gaules" {
		t.Fatalf("recents = %+v, want [gaules]", recents)
	}
}

func TestPasteKickURLInSearchPlaysDirectlyWithoutChat(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			cat := mustCatalog(t, lang)
			pb := &fakePlayback{}
			fc := &fakeChat{}
			fs := &fakeStore{}
			m := New(cat, WithPlayback(pb), WithChat(fc), WithStore(fs))

			// Open search with Ctrl+F
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})

			// Type/paste Kick channel URL
			kickURL := "https://kick.com/xqc"
			m = typeRunes(m, kickURL)
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainCmd(t, m, cmd)

			// Player started with qualified label kick:xqc and target URL
			if len(pb.started) != 1 || pb.started[0] != "kick:xqc" {
				t.Fatalf("playback started = %v, want [kick:xqc]", pb.started)
			}
			if len(pb.targets) != 1 || pb.targets[0].Channel.URL != kickURL || pb.targets[0].Channel.Platform != store.PlatformKick {
				t.Fatalf("playback target = %+v, want URL %q, platform kick", pb.targets, kickURL)
			}

			// Chat is unsupported: did not join IRC
			if len(fc.joined) != 0 {
				t.Fatalf("chat joined = %v, want empty for non-Twitch stream", fc.joined)
			}

			// Chat view shows the localized notice
			view := ansi.Strip(m.View())
			wantNotice := cat.T("chat.unsupported")
			if !strings.Contains(view, wantNotice) {
				t.Fatalf("view missing unsupported chat notice %q:\n%s", wantNotice, view)
			}

			// Recents contain the Kick channel with URL
			recents := m.list.toStoreState().Recents
			if len(recents) != 1 || recents[0].Platform != store.PlatformKick || recents[0].URL != kickURL {
				t.Fatalf("recents = %+v, want kick channel with URL", recents)
			}
		})
	}
}

func TestPasteYouTubeURLInSearchPlaysDirectly(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{}
	fs := &fakeStore{}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc), WithStore(fs))

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	ytURL := "https://www.youtube.com/watch?v=5qap5aO4i9A"
	m = typeRunes(m, ytURL)
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = drainCmd(t, m, cmd)

	if len(pb.started) != 1 || pb.started[0] != "youtube:5qap5aO4i9A" {
		t.Fatalf("playback started = %v, want [youtube:5qap5aO4i9A]", pb.started)
	}
	if len(pb.targets) != 1 || pb.targets[0].Channel.URL != ytURL || pb.targets[0].Channel.Platform != store.PlatformYouTube {
		t.Fatalf("playback target = %+v, want YouTube target", pb.targets)
	}
}

func TestFilterInputWithURLOpensDirectlyOnEnter(t *testing.T) {
	pb := &fakePlayback{}
	fc := &fakeChat{}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc))

	// Open filter with /
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = typeRunes(m, "https://kick.com/xqc")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	_ = drainCmd(t, m, cmd)

	if len(pb.started) != 1 || pb.started[0] != "kick:xqc" {
		t.Fatalf("playback started = %v, want [kick:xqc]", pb.started)
	}
}

func TestReopeningNonTwitchChannelFromRecentsPlaysURL(t *testing.T) {
	pb := &fakePlayback{}
	fs := &fakeStore{}
	initialRecents := []store.Channel{
		{
			Platform:    store.PlatformKick,
			ID:          "kick:xqc",
			Login:       "xqc",
			DisplayName: "xqc",
			URL:         "https://kick.com/xqc",
		},
	}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithStore(fs), WithRecents(store.ToChannels(initialRecents)))

	// Highlight the recent item and press Enter
	items := m.VisibleItems()
	var recentItem *ListItem
	for i, it := range items {
		if it.Type == ItemTypeRecent && it.Channel != nil && it.Channel.Login == "xqc" {
			recentItem = &items[i]
			break
		}
	}
	if recentItem == nil {
		t.Fatalf("recent item xqc not found in visible items: %+v", items)
	}

	m.list.highlightedID = recentItem.ID()
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if sel, ok := cmd().(ChannelSelectedMsg); ok {
			updated, startCmd := m.Update(sel)
			m = updated.(Model)
			m = drainCmd(t, m, startCmd)
		}
	}

	if len(pb.started) != 1 || pb.started[0] != "kick:xqc" {
		t.Fatalf("playback started = %v, want [kick:xqc]", pb.started)
	}
	if len(pb.targets) != 1 || pb.targets[0].Channel.URL != "https://kick.com/xqc" {
		t.Fatalf("playback target = %+v, want URL https://kick.com/xqc", pb.targets)
	}
}

func TestFavoritingNonTwitchChannelWorks(t *testing.T) {
	pb := &fakePlayback{}
	fs := &fakeStore{}
	initialRecents := []store.Channel{
		{
			Platform:    store.PlatformKick,
			ID:          "kick:xqc",
			Login:       "xqc",
			DisplayName: "xqc",
			URL:         "https://kick.com/xqc",
		},
	}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithStore(fs), WithRecents(store.ToChannels(initialRecents)))

	// Highlight the recent item and press 'f' to favorite
	items := m.VisibleItems()
	var recentItem *ListItem
	for i, it := range items {
		if it.Type == ItemTypeRecent && it.Channel != nil && it.Channel.Login == "xqc" {
			recentItem = &items[i]
			break
		}
	}
	if recentItem == nil {
		t.Fatalf("recent item xqc not found")
	}

	m.list.highlightedID = recentItem.ID()
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = drainCmd(t, m, cmd)

	favs := m.Favorites()
	found := false
	for _, f := range favs {
		if f.Login == "xqc" && f.Platform == store.PlatformKick {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected xqc on kick to be in favorites: %+v", favs)
	}

	// Verify state saved to store contains the platform and URL in favorites
	st := m.list.toStoreState()
	if len(st.Favorites) != 1 || st.Favorites[0].Platform != store.PlatformKick || st.Favorites[0].URL != "https://kick.com/xqc" {
		t.Fatalf("store state favorites = %+v, want platform kick and URL preserved", st.Favorites)
	}
}

func TestRetryKeepsNonTwitchTarget(t *testing.T) {
	pb := &fakePlayback{}
	m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb))

	// Open Kick URL
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
	m = typeRunes(m, "https://kick.com/xqc")
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, m, cmd)

	// Fail playback on generation 1
	m = failWith(t, m, m.PlayerGeneration(), PlayerFailureNetwork, "connection refused")
	if m.PlayerState() != PlayerStateError {
		t.Fatalf("player state = %q, want erro", m.PlayerState())
	}

	// Press 'r' to retry
	_, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if len(pb.started) != 2 || pb.started[1] != "kick:xqc" {
		t.Fatalf("retry playback started = %v, want second start of kick:xqc", pb.started)
	}
	if len(pb.targets) != 2 || pb.targets[1].Channel.URL != "https://kick.com/xqc" {
		t.Fatalf("retry target = %+v, want URL preserved", pb.targets)
	}
}

func TestPasteURLForaDaGramaticaViraBusca(t *testing.T) {
	links := []string{
		"https://twitch.tv/a%0D%0Ab",
		"https://twitch.tv/a,%23b",
		"https://kick.com/a%2e%2e",
		"https://kick.com/x?clip=%1b",
	}
	for _, link := range links {
		t.Run(link, func(t *testing.T) {
			pb := &fakePlayback{}
			fc := &fakeChat{}
			m := New(mustCatalog(t, i18n.LangEN), WithPlayback(pb), WithChat(fc), WithStore(&fakeStore{}))

			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, link)
			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = drainCmd(t, m, cmd)

			if len(pb.started) != 0 || len(fc.joined) != 0 {
				t.Fatalf("playback %v, chat %v, want nada aberto", pb.started, fc.joined)
			}
			if m.Mode() != ModeSearch {
				t.Fatalf("mode = %q, want ModeSearch", m.Mode())
			}
		})
	}
}
