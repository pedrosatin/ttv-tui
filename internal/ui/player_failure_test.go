package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// newFailureModel opens login with a fake playback at the given width and
// returns the model after the Start handshake (generation 1).
func newFailureModel(t *testing.T, lang string, width int, login string) (Model, *fakePlayback) {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New(%s): %v", lang, err)
	}
	pb := &fakePlayback{}
	m := New(cat, WithPlayback(pb))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
	m = updated.(Model)
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: login, DisplayName: login}})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	if m.PlayerGeneration() != 1 || m.PlayerState() != PlayerStateStarting {
		t.Fatalf("after select: gen=%d state=%q", m.PlayerGeneration(), m.PlayerState())
	}
	return m, pb
}

// drainCmd runs cmd (and nested batches) feeding PlayerStatusMsg results back.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			m = drainCmd(t, m, c)
		}
	case PlayerStatusMsg:
		updated, _ := m.Update(v)
		m = updated.(Model)
	}
	return m
}

func failWith(t *testing.T, m Model, gen uint64, f PlayerFailure, line string) Model {
	t.Helper()
	updated, _ := m.Update(PlayerStatusMsg{
		Generation: gen,
		State:      PlayerStateError,
		Login:      m.PlayerLogin(),
		LastLine:   line,
		Failure:    f,
	})
	return updated.(Model)
}

const offlineLine = "error: No playable streams found on this URL: https://www.twitch.tv/xqc"

func TestPlayerFailureMessagesPerClass(t *testing.T) {
	cases := []struct {
		failure PlayerFailure
		line    string
		pt      string
		en      string
	}{
		{PlayerFailureOffline, offlineLine, "Erro: Canal offline · r repetir · ? detalhes", "Player error: channel offline · r retry · ? details"},
		{PlayerFailureNetwork, "error: Unable to open URL: https://gql.twitch.tv/gql (Connection refused)", "Erro de rede: r para tentar de novo · ? detalhes", "Network error: press r to retry · ? details"},
		{PlayerFailureTimeout, "error: Connection to gql.twitch.tv timed out", "Erro: tempo esgotado · r repetir · ? detalhes", "Player error: timed out · r retry · ? details"},
		{PlayerFailureStreamlinkMissing, "player: streamlink not found: exec: \"streamlink\": executable file not found in $PATH", "Erro: streamlink ausente · r repetir · ? detalhes", "Player error: streamlink missing · r retry · ? details"},
		{PlayerFailureMPVMissing, "error: Failed to start player: mpv (Player executable not found)", "Erro: mpv ausente · r repetir · ? detalhes", "Player error: mpv missing · r retry · ? details"},
		{PlayerFailureUnknown, "streamlink exited with code 1", "Falha no player: r para tentar de novo · ? detalhes", "Player failed: press r to retry · ? details"},
	}
	for _, c := range cases {
		for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
			t.Run(string(c.failure)+"/"+lang, func(t *testing.T) {
				m, _ := newFailureModel(t, lang, 200, "xqc")
				m = failWith(t, m, 1, c.failure, c.line)
				want := c.en
				detailLabel := "detail: "
				if lang == i18n.LangPTBR {
					want = c.pt
					detailLabel = "detalhe: "
				}
				bar := ansi.Strip(m.StatusBar())
				iMsg := strings.Index(bar, want)
				if iMsg < 0 {
					t.Fatalf("status bar lacks %q:\n%s", want, bar)
				}
				iDetail := strings.Index(bar, detailLabel+c.line)
				if iDetail < 0 {
					t.Fatalf("status bar lacks the technical detail %q:\n%s", c.line, bar)
				}
				if iDetail < iMsg {
					t.Fatalf("technical detail must come after the main message:\n%s", bar)
				}
				if strings.Contains(bar, "Erro no player: error:") || strings.Contains(bar, "Player error: error:") {
					t.Fatalf("raw line still leads the message:\n%s", bar)
				}
			})
		}
	}
}

func TestPlayerFailureDetailInHelp(t *testing.T) {
	m, _ := newFailureModel(t, i18n.LangPTBR, 200, "xqc")
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "Último erro do player: "+offlineLine) {
		t.Fatalf("help overlay must show the technical detail:\n%s", ansi.Strip(m.View()))
	}
}

func TestStalePlayerFailureDoesNotOverwriteCurrentChannel(t *testing.T) {
	m, _ := newFailureModel(t, i18n.LangPTBR, 200, "xqc")
	// Switch to gaules: generation 2 starts and plays.
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)

	// A late offline failure from xqc (generation 1) arrives.
	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStateError, Login: "xqc", LastLine: offlineLine, Failure: PlayerFailureOffline})
	m = updated.(Model)
	if m.PlayerState() != PlayerStatePlaying || m.PlayerLogin() != "gaules" || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("stale failure leaked: state=%q login=%q failure=%q", m.PlayerState(), m.PlayerLogin(), m.PlayerFailure())
	}
	if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, "offline") {
		t.Fatalf("status bar shows the stale failure:\n%s", bar)
	}

	// Same while the switch handshake is still in flight.
	updated, _ = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStateError, Login: "gaules", LastLine: "boom", Failure: PlayerFailureNetwork})
	m = updated.(Model)
	if m.PlayerState() != PlayerStateStarting || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("pre-handshake failure leaked: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
}

func TestPlayerFailureAt80ColumnsKeepsMainAction(t *testing.T) {
	wants := map[string][]string{
		i18n.LangPTBR: {
			"Erro: Canal offline · r repetir · ? detalhes",
			"Erro de rede: r para tentar de novo · ? detalhes",
			"Erro: tempo esgotado · r repetir · ? detalhes",
			"Erro: streamlink ausente · r repetir · ? detalhes",
			"Erro: mpv ausente · r repetir · ? detalhes",
			"Falha no player: r para tentar de novo · ? detalhes",
		},
		i18n.LangEN: {
			"Player error: channel offline · r retry · ? details",
			"Network error: press r to retry · ? details",
			"Player error: timed out · r retry · ? details",
			"Player error: streamlink missing · r retry · ? details",
			"Player error: mpv missing · r retry · ? details",
			"Player failed: press r to retry · ? details",
		},
	}
	failures := []PlayerFailure{
		PlayerFailureOffline, PlayerFailureNetwork, PlayerFailureTimeout,
		PlayerFailureStreamlinkMissing, PlayerFailureMPVMissing, PlayerFailureUnknown,
	}
	for lang, msgs := range wants {
		for i, f := range failures {
			m, _ := newFailureModel(t, lang, 80, "xqc")
			m = failWith(t, m, 1, f, offlineLine)
			bar := ansi.Strip(m.StatusBar())
			if w := ansi.StringWidth(bar); w != 80 {
				t.Fatalf("%s/%s: bar width %d, want 80", lang, f, w)
			}
			if !strings.Contains(bar, msgs[i]) {
				t.Fatalf("%s/%s: main action truncated at 80 columns:\n%q", lang, f, bar)
			}
			if !strings.Contains(bar, "Ctrl+Q") {
				t.Fatalf("%s/%s: quit hint lost:\n%q", lang, f, bar)
			}
			if strings.Contains(bar, "Ctrl+C") {
				t.Fatalf("%s/%s: quit hint still cites Ctrl+C:\n%q", lang, f, bar)
			}
		}
	}
}

func TestPlayerFailureAt80ColumnsWithChat(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	m = updated.(Model)
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "Player error: channel offline · r retry · ? details") {
		t.Fatalf("main action truncated with chat connected:\n%q", bar)
	}
}

func TestRetryKeyRestartsFailedPlayback(t *testing.T) {
	m, pb := newFailureModel(t, i18n.LangPTBR, 120, "gaules")
	m = failWith(t, m, 1, PlayerFailureNetwork, "error: Unable to open URL")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if m.PlayerState() != PlayerStateStarting || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("after r: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
	m = drainCmd(t, m, cmd)
	if len(pb.started) != 2 || pb.started[1] != "gaules" {
		t.Fatalf("r must restart playback of gaules; started=%v", pb.started)
	}
	if m.PlayerGeneration() != 2 {
		t.Fatalf("generation after retry = %d, want 2", m.PlayerGeneration())
	}
}

// liveChannel seeds a channel that still looks live in the listing (stale
// viewers) so an offline play failure can correct it.
func liveChannel(id, login, display string, viewers int) twitch.Channel {
	return twitch.Channel{
		ID:           id,
		Login:        login,
		DisplayName:  display,
		Viewers:      twitch.IntPtr(viewers),
		Title:        twitch.StringPtr("Live now"),
		Availability: twitch.AvailabilityLive,
	}
}

func channelByLogin(chs []twitch.Channel, login string) (twitch.Channel, bool) {
	for _, ch := range chs {
		if strings.EqualFold(ch.Login, login) {
			return ch, true
		}
	}
	return twitch.Channel{}, false
}

// setupStaleLiveListing builds a channels-mode model with xqc live in the
// listing, favorites and recents, plus another live channel whose viewers
// must survive an offline failure for xqc.
func setupStaleLiveListing(t *testing.T) Model {
	t.Helper()
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	xqc := liveChannel("xqc-id", "xqc", "xQc", 13000)
	gaules := liveChannel("gaules-id", "gaules", "Gaules", 8500)
	m := New(catEN,
		WithPlayback(pb),
		WithFavorites([]twitch.Channel{xqc, gaules}),
		WithRecents([]twitch.Channel{xqc}),
	)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	m.mode = ModeChannels
	m.channels = []twitch.Channel{xqc, gaules}
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: xqc})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	if m.PlayerGeneration() != 1 || m.PlayerLogin() != "xqc" {
		t.Fatalf("after select: gen=%d login=%q", m.PlayerGeneration(), m.PlayerLogin())
	}
	return m
}

func TestOfflinePlayFailureMarksChannelOfflineInListFavoritesRecents(t *testing.T) {
	m := setupStaleLiveListing(t)
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)

	assertOfflineCleared := func(t *testing.T, where string, ch twitch.Channel) {
		t.Helper()
		if ch.Availability != twitch.AvailabilityOffline {
			t.Fatalf("%s availability = %v, want offline", where, ch.Availability)
		}
		if ch.Viewers != nil {
			t.Fatalf("%s viewers = %v, want nil after offline evidence", where, *ch.Viewers)
		}
	}

	ch, ok := channelByLogin(m.Channels(), "xqc")
	if !ok {
		t.Fatal("xqc missing from channels listing")
	}
	assertOfflineCleared(t, "channels", ch)

	fav, ok := channelByLogin(m.Favorites(), "xqc")
	if !ok {
		t.Fatal("xqc missing from favorites")
	}
	assertOfflineCleared(t, "favorites", fav)

	rec, ok := channelByLogin(m.Recents(), "xqc")
	if !ok {
		t.Fatal("xqc missing from recents")
	}
	assertOfflineCleared(t, "recents", rec)

	byLogin := map[string]ListItem{}
	for _, it := range m.VisibleItems() {
		byLogin[strings.ToLower(it.Login())] = it
	}
	if !byLogin["xqc"].Offline() {
		t.Fatalf("visible xqc availability = %v, want offline", byLogin["xqc"].Availability)
	}

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "xQc (xqc) — [offline]") {
		t.Fatalf("view must show [offline] without stale viewers:\n%s", plain)
	}
	if strings.Contains(plain, "13.0k") || strings.Contains(plain, "13k") {
		t.Fatalf("stale viewers for offline channel still rendered:\n%s", plain)
	}
}

func TestNetworkOrTimeoutFailureDoesNotChangeAvailability(t *testing.T) {
	for _, f := range []PlayerFailure{PlayerFailureNetwork, PlayerFailureTimeout, PlayerFailureUnknown} {
		t.Run(string(f), func(t *testing.T) {
			m := setupStaleLiveListing(t)
			beforeCh := m.Channels()
			beforeFav := m.Favorites()
			beforeRec := m.Recents()

			m = failWith(t, m, 1, f, "error: Unable to open URL")

			ch, _ := channelByLogin(m.Channels(), "xqc")
			if ch.Availability != twitch.AvailabilityLive || ch.Viewers == nil || *ch.Viewers != 13000 {
				t.Fatalf("channels xqc changed on %s: %+v", f, ch)
			}
			fav, _ := channelByLogin(m.Favorites(), "xqc")
			if fav.Availability != twitch.AvailabilityLive || fav.Viewers == nil || *fav.Viewers != 13000 {
				t.Fatalf("favorites xqc changed on %s: %+v", f, fav)
			}
			rec, _ := channelByLogin(m.Recents(), "xqc")
			if rec.Availability != twitch.AvailabilityLive || rec.Viewers == nil || *rec.Viewers != 13000 {
				t.Fatalf("recents xqc changed on %s: %+v", f, rec)
			}
			if len(m.Channels()) != len(beforeCh) || len(m.Favorites()) != len(beforeFav) || len(m.Recents()) != len(beforeRec) {
				t.Fatalf("%s altered collection sizes", f)
			}
		})
	}
}

func TestStaleOfflineFailureDoesNotMarkCurrentChannel(t *testing.T) {
	m := setupStaleLiveListing(t)

	// Switch to gaules: generation 2 starts.
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: liveChannel("gaules-id", "gaules", "Gaules", 8500)})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)

	// Late offline failure from xqc (generation 1) must be ignored.
	updated, _ = m.Update(PlayerStatusMsg{
		Generation: 1,
		State:      PlayerStateError,
		Login:      "xqc",
		LastLine:   offlineLine,
		Failure:    PlayerFailureOffline,
	})
	m = updated.(Model)

	if m.PlayerState() != PlayerStatePlaying || m.PlayerLogin() != "gaules" {
		t.Fatalf("stale failure affected player: state=%q login=%q", m.PlayerState(), m.PlayerLogin())
	}
	ch, _ := channelByLogin(m.Channels(), "xqc")
	if ch.Availability != twitch.AvailabilityLive || ch.Viewers == nil || *ch.Viewers != 13000 {
		t.Fatalf("stale offline marked xqc offline: %+v", ch)
	}
	fav, _ := channelByLogin(m.Favorites(), "xqc")
	if fav.Availability != twitch.AvailabilityLive || fav.Viewers == nil {
		t.Fatalf("stale offline marked favorite xqc: %+v", fav)
	}
}

func TestOfflinePlayFailurePreservesOtherChannelViewers(t *testing.T) {
	m := setupStaleLiveListing(t)
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)

	other, ok := channelByLogin(m.Channels(), "gaules")
	if !ok {
		t.Fatal("gaules missing from channels")
	}
	if other.Availability != twitch.AvailabilityLive || other.Viewers == nil || *other.Viewers != 8500 {
		t.Fatalf("other channel viewers changed: %+v", other)
	}
	favOther, ok := channelByLogin(m.Favorites(), "gaules")
	if !ok {
		t.Fatal("gaules missing from favorites")
	}
	if favOther.Availability != twitch.AvailabilityLive || favOther.Viewers == nil || *favOther.Viewers != 8500 {
		t.Fatalf("favorite other channel viewers changed: %+v", favOther)
	}

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "8.5k") {
		t.Fatalf("gaules viewers must remain visible:\n%s", plain)
	}
}

func TestPlayerTerminalStatesRender(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, size := range [][2]int{{80, 24}, {120, 40}} {
			t.Run(fmt.Sprintf("%s/%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				m, pb := newFailureModel(t, lang, size[0], "xaryu")
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = updated.(Model)
				m.chatPanel.state = ChatStateConnected
				m.chatPanel.channel = "xaryu"
				updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "xaryu", Volume: 100})
				playing := updated.(Model)
				updated, cmd := playing.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
				stopped := drainCmd(t, updated.(Model), cmd)
				neutral := ansi.Strip(stopped.View())
				if pb.stopped == 0 || !strings.Contains(neutral, m.t("player.ended")) || strings.Contains(neutral, m.t("player.fail.unknown")) {
					t.Fatalf("intentional stop must stay visible and neutral:\n%s", neutral)
				}
				for _, failure := range []PlayerFailure{PlayerFailureNone, PlayerFailureUnknown, PlayerFailureOffline, PlayerFailureNetwork, PlayerFailureTimeout, PlayerFailureStreamlinkMissing, PlayerFailureMPVMissing} {
					failed := failWith(t, playing, 1, failure, "")
					failed.statusNotice = "secondary notice"
					failed.copyNotice = "copy notice"
					failed.err = errors.New("list error")
					view := ansi.Strip(failed.View())
					want := failed.playerFailureText()
					if !strings.Contains(view, want) || !strings.Contains(want, "r ") || !strings.Contains(want, "?") || strings.Contains(view, m.t("player.ended")) {
						t.Fatalf("failure %q must retain label, reason and actions:\n%s", failure, view)
					}
					if failure == PlayerFailureUnknown {
						t.Logf("FAILURE\n%s\nSTOPPED\n%s", view, neutral)
					}
				}
			})
		}
	}
}
