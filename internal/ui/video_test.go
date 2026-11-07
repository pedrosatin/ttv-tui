package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestVideoLayoutCycle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		reason  string
		video   bool
	}{
		{"opt-in supported", true, "", true},
		{"no flag", false, "", false},
		{"no terminal", true, "video.no_terminal", false},
		{"no mpv", true, "video.no_mpv", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			WithVideo(tc.enabled, tc.reason)(&m)
			m.width, m.height = 120, 32
			want := []LayoutMode{LayoutChat, LayoutList, LayoutFocus, LayoutSplit}
			if tc.video {
				want[3] = LayoutVideo
				want = append(want, LayoutSplit)
			}
			for range 2 {
				for _, mode := range want {
					m = sendLayoutKey(t, m, tea.KeyCtrlW)
					if m.LayoutMode() != mode {
						t.Fatalf("got %q want %q", m.LayoutMode(), mode)
					}
					if mode == LayoutVideo {
						plain := ansi.Strip(m.View())
						lines := strings.Split(plain, "\n")
						if !strings.Contains(lines[0], m.t("video.placeholder")) || !strings.Contains(lines[18], "Chat") || m.listVisible() || m.Focus() != FocusChat {
							t.Fatalf("invalid video layout:\n%s", plain)
						}
						if len(lines) != 32 {
							t.Fatalf("height=%d", len(lines))
						}
					}
				}
			}
			if tc.reason != "" {
				if !strings.Contains(ansi.Strip(m.StatusBar()), m.t(tc.reason)) {
					t.Fatalf("missing reason: %s", m.StatusBar())
				}
				m.helpVisible = true
				if !strings.Contains(strings.Join(m.renderHelpLines(120), "\n"), m.t(tc.reason)) {
					t.Fatal("help missing reason")
				}
			}
		})
	}
}

func TestVideoResizeAndToggles(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, height := range []int{1, 2, 7, 14, 24, 40} {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog, _ = i18n.New(lang)
			WithVideo(true, "")(&m)
			for range 4 {
				m = sendLayoutKey(t, m, tea.KeyCtrlW)
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: height})
			m = updated.(Model)
			if len(strings.Split(m.View(), "\n")) != height {
				t.Fatalf("height %d: %s", height, m.View())
			}
			for _, key := range []tea.KeyType{tea.KeyCtrlH, tea.KeyCtrlL} {
				next := sendLayoutKey(t, m, key)
				if next.LayoutMode() == LayoutVideo {
					t.Fatal("toggle did not leave video")
				}
			}
		}
	}
}

type fakeSurface struct{ panels []VideoPanel }

func (f *fakeSurface) SetPanel(p VideoPanel) { f.panels = append(f.panels, p) }

func (f *fakeSurface) last(t *testing.T) VideoPanel {
	t.Helper()
	if len(f.panels) == 0 {
		t.Fatal("no panel published")
	}
	return f.panels[len(f.panels)-1]
}

// windowVideoModel is a playback that began in the split, in the mpv window, with
// the flag on and the terminal supported, at 120x32.
func windowVideoModel(t *testing.T) (Model, *fakePlayback, *fakeSurface) {
	t.Helper()
	m, pb, _ := setupTwoPanelModel(t)
	surface := &fakeSurface{}
	WithVideo(true, "")(&m)
	WithVideoSurface(surface)(&m)
	return applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 32}), pb, surface
}

func enterVideoLayout(t *testing.T, m Model) Model {
	t.Helper()
	for range 4 {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
	}
	if m.LayoutMode() != LayoutVideo {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
	return m
}

var restartKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}

// pressRestart presses r and completes the Start it causes.
func pressRestart(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(restartKey)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("r started nothing")
	}
	return applyMsg(t, m, cmd())
}

// playingVideoModel is the video layout with a stream playing in the panel: the
// window session of the split, restarted with r, which is the second playback.
func playingVideoModel(t *testing.T) (Model, *fakeSurface) {
	t.Helper()
	m, _, surface := windowVideoModel(t)
	m = pressRestart(t, enterVideoLayout(t, m))
	return applyMsg(t, m, playerState(PlayerStatePlaying)), surface
}

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func playerState(state PlayerState) PlayerStatusMsg {
	return PlayerStatusMsg{Generation: 2, State: state, Login: "gaules", Volume: 85}
}

func clearsScreen(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if clearsScreen(c) {
				return true
			}
		}
		return false
	}
	return msg == tea.ClearScreen()
}

func TestVideoPanelGeometryFollowsTheWindow(t *testing.T) {
	m, surface := playingVideoModel(t)
	want := VideoPanel{Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true, Playing: true, Still: true}
	if got := surface.last(t); got != want {
		t.Fatalf("panel = %+v", got)
	}
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 90, Height: 15})
	want.Rows, want.Cols = 8, 90
	if got := surface.last(t); got != want {
		t.Fatalf("panel after resize = %+v", got)
	}
	calls := len(surface.panels)
	_ = applyMsg(t, m, tea.WindowSizeMsg{Width: 90, Height: 15})
	if len(surface.panels) != calls {
		t.Fatal("published an unchanged panel")
	}
}

func TestVideoPanelRowsNeverCarryText(t *testing.T) {
	m, surface := playingVideoModel(t)
	blank := func(step string) {
		t.Helper()
		rows := surface.last(t).Rows
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		for i, line := range lines[:rows] {
			if strings.TrimSpace(line) != "" {
				t.Fatalf("%s: panel row %d carries %q", step, i, line)
			}
		}
		if !strings.Contains(strings.Join(lines[rows:], "\n"), "TriboOne") {
			t.Fatalf("%s: chat left its rows:\n%s", step, strings.Join(lines, "\n"))
		}
	}
	blank("playing")
	for i := range 50 {
		m = applyMsg(t, m, ChatEventMsg{
			Generation: 1,
			Channel:    "gaules",
			State:      ChatStateConnected,
			Messages:   []ChatMessage{{Generation: 1, Kind: chat.MessageChat, ID: "x" + strconv.Itoa(i), User: "tribo1", DisplayName: "TriboOne", Body: strings.Repeat("spam ", 40)}},
		})
		blank("chat burst")
	}
	m.warnStatus("a long status notice")
	blank("status notice")
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	blank("resize")
	m = applyMsg(t, m, playerState(PlayerStatePaused))
	blank("paused")
	m = applyMsg(t, m, playerState(PlayerStateBreak))
	blank("break")
}

func TestVideoPanelOffOrStopped(t *testing.T) {
	press := func(key tea.KeyType) func(*testing.T, Model) Model {
		return func(t *testing.T, m Model) Model { return sendLayoutKey(t, m, key) }
	}
	state := func(s PlayerState) func(*testing.T, Model) Model {
		return func(t *testing.T, m Model) Model { return applyMsg(t, m, playerState(s)) }
	}
	for _, tc := range []struct {
		name    string
		act     func(*testing.T, Model) Model
		visible bool
		playing bool
	}{
		{"next layout", press(tea.KeyCtrlW), false, false},
		{"list key", press(tea.KeyCtrlH), false, false},
		{"chat key", press(tea.KeyCtrlL), false, false},
		{"help", press(tea.KeyF1), false, false},
		{"window too short", func(t *testing.T, m Model) Model {
			return applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 6})
		}, false, false},
		{"paused", state(PlayerStatePaused), true, false},
		{"break", state(PlayerStateBreak), true, false},
		{"starting", state(PlayerStateStarting), true, false},
		{"ended", state(PlayerStateEnded), true, false},
		{"error", state(PlayerStateError), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, surface := playingVideoModel(t)
			tc.act(t, m)
			got := surface.last(t)
			if got.Visible != tc.visible || got.Playing != tc.playing {
				t.Fatalf("panel = %+v", got)
			}
		})
	}
}

func TestVideoPanelOffWhileQuitting(t *testing.T) {
	m, surface := playingVideoModel(t)
	m.quitting = true
	_ = applyMsg(t, m, struct{}{})
	if got := surface.last(t); got != (VideoPanel{}) {
		t.Fatalf("panel = %+v", got)
	}
}

func TestVideoImageIsClearedOnlyWhenItMustGo(t *testing.T) {
	key := func(k tea.KeyType) tea.Msg { return tea.KeyMsg{Type: k} }
	for _, tc := range []struct {
		name  string
		msg   tea.Msg
		clear bool
	}{
		{"leave the layout", key(tea.KeyCtrlW), true},
		{"open help", key(tea.KeyF1), true},
		{"stream ended", playerState(PlayerStateEnded), true},
		{"stream failed", playerState(PlayerStateError), true},
		{"channel switch", playerState(PlayerStateStarting), true},
		{"pause keeps the frame", playerState(PlayerStatePaused), false},
		{"break keeps the frame", playerState(PlayerStateBreak), false},
		{"resize", tea.WindowSizeMsg{Width: 100, Height: 30}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := playingVideoModel(t)
			_, cmd := m.Update(tc.msg)
			if got := clearsScreen(cmd); got != tc.clear {
				t.Fatalf("clear = %v, want %v", got, tc.clear)
			}
		})
	}
}

func TestVideoFramesStopBeforeTheClear(t *testing.T) {
	m, surface := playingVideoModel(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := surface.last(t); got.Visible {
		t.Fatalf("panel still on when Update returned: %+v", got)
	}
	if !clearsScreen(cmd) {
		t.Fatal("no clear")
	}
	if updated.(Model).LayoutMode() == LayoutVideo {
		t.Fatal("still in the video layout")
	}
}

func TestVideoWithoutSurfaceKeepsThePlaceholderAndNotice(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	WithVideo(true, "")(&m)
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 32})
	for range 4 {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
	}
	if !strings.Contains(ansi.Strip(m.View()), m.t("video.placeholder")) {
		t.Fatal("placeholder missing")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if clearsScreen(cmd) || updated.(Model).videoPublished != (VideoPanel{}) {
		t.Fatal("no surface, nothing to clear or publish")
	}
	if !m.playerWindowNotified {
		t.Fatal("window notice lost without the surface")
	}
}

func TestVideoSurfaceSilentOutsideTheLayout(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	surface := &fakeSurface{}
	WithVideo(true, "")(&m)
	WithVideoSurface(surface)(&m)
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 32})
	for range 3 {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
	}
	m = applyMsg(t, m, playerState(PlayerStatePaused))
	_ = applyMsg(t, m, playerState(PlayerStatePlaying))
	if len(surface.panels) != 0 {
		t.Fatalf("panels = %+v", surface.panels)
	}
}

func TestVideoSurfaceHasNoWindowToAnnounce(t *testing.T) {
	m, _ := playingVideoModel(t)
	m.playerWindowNotified = false
	m = applyMsg(t, m, playerState(PlayerStatePlaying))
	if m.playerWindowNotified || m.playerWindowNotice {
		t.Fatal("announced an mpv window that does not exist")
	}
}

func TestOpeningAChannelPicksTheOutputFromTheLayout(t *testing.T) {
	open := func(t *testing.T, m Model) Model {
		t.Helper()
		updated, _ := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
		return updated.(Model)
	}
	for presses, mode := range []LayoutMode{LayoutSplit, LayoutChat, LayoutList, LayoutFocus, LayoutVideo} {
		t.Run(string(mode), func(t *testing.T) {
			m, pb, _ := windowVideoModel(t)
			for range presses {
				m = sendLayoutKey(t, m, tea.KeyCtrlW)
			}
			if m.LayoutMode() != mode {
				t.Fatalf("layout = %q", m.LayoutMode())
			}
			open(t, m)
			if got, want := pb.embedded[len(pb.embedded)-1], mode == LayoutVideo; got != want {
				t.Fatalf("embedded = %v, want %v", got, want)
			}
		})
	}
}

func TestVideoLayoutWithoutSurfaceStartsInTheWindow(t *testing.T) {
	m, pb, _ := setupTwoPanelModel(t)
	WithVideo(true, "")(&m)
	m = enterVideoLayout(t, m)
	updated, _ := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	if pb.embedded[len(pb.embedded)-1] {
		t.Fatal("nothing would draw the panel")
	}
	if updated.(Model).videoRestartOffered() {
		t.Fatal("offered a restart into a panel that cannot draw")
	}
}

func TestLayoutKeysNeverRestartThePlayer(t *testing.T) {
	windowSession := func(t *testing.T) (Model, *fakePlayback) {
		m, pb, _ := windowVideoModel(t)
		return m, pb
	}
	embeddedSession := func(t *testing.T) (Model, *fakePlayback) {
		m, _ := playingVideoModel(t)
		return m, m.playback.(*fakePlayback)
	}
	for name, setup := range map[string]func(*testing.T) (Model, *fakePlayback){"window": windowSession, "embedded": embeddedSession} {
		t.Run(name, func(t *testing.T) {
			m, pb := setup(t)
			started, stopped := len(pb.started), pb.stopped
			for range 10 {
				for _, key := range []tea.KeyType{tea.KeyCtrlW, tea.KeyCtrlH, tea.KeyCtrlW, tea.KeyCtrlL} {
					m = sendLayoutKey(t, m, key)
				}
			}
			if len(pb.started) != started || pb.stopped != stopped {
				t.Fatalf("started %d more, stopped %d more", len(pb.started)-started, pb.stopped-stopped)
			}
		})
	}
}

func TestWindowSessionInTheLayoutOffersTheRestart(t *testing.T) {
	m, pb, surface := windowVideoModel(t)
	if got := ansi.Strip(m.View()); strings.Contains(got, m.t("video.restart_embedded")) {
		t.Fatalf("offer outside the layout:\n%s", got)
	}
	m = enterVideoLayout(t, m)
	if row := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(row, m.t("video.restart_embedded")) {
		t.Fatalf("panel row: %q", row)
	}
	if got := surface.last(t); !got.Visible || got.Playing {
		t.Fatalf("a window session drew in the panel: %+v", got)
	}
	// The offer stays for as long as it is true, past any other key.
	m = sendLayoutKey(t, m, tea.KeyTab)
	if row := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(row, m.t("video.restart_embedded")) {
		t.Fatalf("offer gone after a key: %q", row)
	}
	m = pressRestart(t, m)
	if got := pb.embedded; len(got) != 2 || got[0] || !got[1] {
		t.Fatalf("embedded = %v", got)
	}
	if m.videoRestartOffered() {
		t.Fatal("still offered after the restart")
	}
	if row := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(row, m.t("video.placeholder")) {
		t.Fatalf("panel row while starting: %q", row)
	}
	_ = applyMsg(t, m, playerState(PlayerStatePlaying))
	if got := surface.last(t); !got.Playing {
		t.Fatalf("panel = %+v", got)
	}
}

func TestLeavingTheLayoutLeavesAudioOnlyUntilRestarted(t *testing.T) {
	m, surface := playingVideoModel(t)
	pb := m.playback.(*fakePlayback)
	bar := func() string { return ansi.Strip(m.StatusBar()) }
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	if got := surface.last(t); got.Visible || got.Playing {
		t.Fatalf("panel still on: %+v", got)
	}
	if !strings.Contains(bar(), m.t("video.audio_only")) {
		t.Fatalf("status bar: %q", bar())
	}
	if m.PlayerState() != PlayerStatePlaying {
		t.Fatalf("state = %q", m.PlayerState())
	}
	// Nothing else says the picture is gone, so a key does not dismiss it.
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	if !strings.Contains(bar(), m.t("video.audio_only")) {
		t.Fatalf("notice gone after a key: %q", bar())
	}
	m = pressRestart(t, m)
	if got := pb.embedded; got[len(got)-1] {
		t.Fatalf("embedded = %v", got)
	}
	if strings.Contains(bar(), m.t("video.audio_only")) {
		t.Fatalf("stale notice: %q", bar())
	}
}

func TestReturningToTheLayoutNeedsNoRestart(t *testing.T) {
	m, surface := playingVideoModel(t)
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	m = enterVideoLayout(t, m)
	want := VideoPanel{Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true, Playing: true, Still: true}
	if got := surface.last(t); got != want {
		t.Fatalf("panel = %+v", got)
	}
	if m.videoRestartOffered() || strings.Contains(ansi.Strip(m.View()), m.t("video.restart_embedded")) {
		t.Fatal("offered a restart with the output already right")
	}
	if strings.Contains(ansi.Strip(m.StatusBar()), m.t("video.audio_only")) {
		t.Fatal("stale notice")
	}
}

func TestAudioOnlyNoticeSurvivesEightyColumns(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _ := playingVideoModel(t)
			m.catalog, _ = i18n.New(lang)
			m = sendLayoutKey(t, m, tea.KeyCtrlW)
			m = applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("video.audio_only")) {
				t.Fatalf("status bar: %q", bar)
			}
		})
	}
}

func TestEndedPlaybackOffersNothing(t *testing.T) {
	for _, state := range []PlayerState{PlayerStateEnded, PlayerStateNone} {
		t.Run(string(state), func(t *testing.T) {
			m, _ := playingVideoModel(t)
			m = sendLayoutKey(t, m, tea.KeyCtrlW)
			m = applyMsg(t, m, playerState(state))
			if m.videoRestartOffered() || m.videoAudioOnly() {
				t.Fatalf("offered with state %q", state)
			}
		})
	}
}

func TestFailureUnderThePanelRetriesEmbedded(t *testing.T) {
	m, _ := playingVideoModel(t)
	pb := m.playback.(*fakePlayback)
	m = applyMsg(t, m, playerState(PlayerStateError))
	m = pressRestart(t, m)
	if got := pb.embedded; !got[len(got)-1] || len(got) != 3 {
		t.Fatalf("embedded = %v", got)
	}
}

func TestRestartKeyStaysTextInAnInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*Model)
		text func(Model) string
	}{
		{"filter", func(m *Model) { m.filtering = true }, func(m Model) string { return m.filter }},
		{"search", func(m *Model) { m.searching = true }, func(m Model) string { return m.searchInput }},
		{"chat draft", func(m *Model) { m.focus, m.composing = FocusChat, true }, func(m Model) string { return m.composeDraft }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := playingVideoModel(t)
			pb := m.playback.(*fakePlayback)
			m = sendLayoutKey(t, m, tea.KeyCtrlW) // split: r is on offer
			tc.open(&m)
			started := len(pb.started)
			next, _ := m.Update(restartKey)
			if len(pb.started) != started {
				t.Fatal("r in an input restarted the player")
			}
			if got := tc.text(next.(Model)); got != "r" {
				t.Fatalf("input holds %q", got)
			}
		})
	}
}

func TestRestartKeyStaysOutOfTheFilterMenu(t *testing.T) {
	m, _ := playingVideoModel(t)
	pb := m.playback.(*fakePlayback)
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	m.channelFilterMenu = true
	started := len(pb.started)
	_, _ = m.Update(restartKey)
	if len(pb.started) != started {
		t.Fatal("r behind the filter menu restarted the player")
	}
}

func TestListErrorKeepsItsRestartKey(t *testing.T) {
	m, _ := playingVideoModel(t)
	pb := m.playback.(*fakePlayback)
	m = sendLayoutKey(t, m, tea.KeyCtrlW) // split: the list is on screen
	m.err = errors.New("list failed")
	started := len(pb.started)
	_, _ = m.Update(restartKey)
	if len(pb.started) != started {
		t.Fatal("r on a list error restarted the player")
	}
}

func TestWindowSessionWithTheFlagStillAnnouncesTheWindow(t *testing.T) {
	m, _, _ := windowVideoModel(t)
	m.playerWindowNotified = false
	m = applyMsg(t, m, playerState(PlayerStatePlaying))
	if !m.playerWindowNotified || !m.playerWindowNotice {
		t.Fatal("the window opened with no notice")
	}
}

func TestWindowSessionHasNoImageToClear(t *testing.T) {
	m, _, _ := windowVideoModel(t)
	m = enterVideoLayout(t, m)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW}); clearsScreen(cmd) {
		t.Fatal("cleared a panel that held no image")
	}
}

var (
	escKeyMsg  = tea.KeyMsg{Type: tea.KeyEsc}
	listKeyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
)

func TestVideoListOpensInsideTheMode(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{"esc": escKeyMsg, "l": listKeyMsg} {
		t.Run(name, func(t *testing.T) {
			m, surface := playingVideoModel(t)
			updated, cmd := m.Update(key)
			m = updated.(Model)
			if !m.listVisible() || m.Focus() != FocusList {
				t.Fatalf("list not reachable: visible=%v focus=%v", m.listVisible(), m.Focus())
			}
			if m.LayoutMode() != LayoutVideo {
				t.Fatalf("the mode was left: %q", m.LayoutMode())
			}
			if plain := ansi.Strip(m.View()); !strings.Contains(plain, m.t("video.list_open")) {
				t.Fatalf("nothing says how to go back:\n%s", plain)
			}
			got := surface.last(t)
			if got.Visible || got.Playing || !got.Still || got.Rows == 0 {
				t.Fatalf("frames not paused: %+v", got)
			}
			if !clearsScreen(cmd) {
				t.Fatal("the image stays under the blank lines of the list")
			}
		})
	}
}

func TestVideoListClosesBackToThePanel(t *testing.T) {
	m, surface := playingVideoModel(t)
	playing := surface.last(t)
	m = applyMsg(t, applyMsg(t, m, escKeyMsg), escKeyMsg)
	if m.listVisible() || m.LayoutMode() != LayoutVideo || m.Focus() != FocusChat {
		t.Fatalf("visible=%v layout=%q focus=%v", m.listVisible(), m.LayoutMode(), m.Focus())
	}
	if got := surface.last(t); got != playing {
		t.Fatalf("panel = %+v, want %+v", got, playing)
	}
	if !m.videoImageOnScreen() {
		t.Fatal("panel rows would carry text over the image")
	}
}

func TestVideoListOverAPauseGetsTheImageBack(t *testing.T) {
	m, surface := playingVideoModel(t)
	m = applyMsg(t, m, playerState(PlayerStatePaused))
	m = applyMsg(t, m, escKeyMsg)
	if got := surface.last(t); got.Visible || !got.Still {
		t.Fatalf("under the list: %+v", got)
	}
	m = applyMsg(t, m, escKeyMsg)
	if got := surface.last(t); !got.Visible || got.Playing || !got.Still {
		t.Fatalf("after the list: %+v", got)
	}
	if !m.videoImageOnScreen() {
		t.Fatal("panel rows would carry text over the image")
	}
}

func TestVideoListChoosingAChannelReturnsToThePanel(t *testing.T) {
	m, surface := playingVideoModel(t)
	m = applyMsg(t, m, escKeyMsg)
	before := len(surface.panels)
	m = applyMsg(t, m, ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	if m.videoListOpen || m.listVisible() {
		t.Fatal("the list stayed open over the new channel")
	}
	if !m.playerEmbedded {
		t.Fatal("the channel chosen in the mode left the panel")
	}
	// One publish: the old channel's frame must not flash on the way out.
	if got := surface.panels[before:]; len(got) != 1 || got[0].Still || got[0].Playing || !got[0].Visible {
		t.Fatalf("panels = %+v", got)
	}
}

func TestVideoListSteppingIntoACategoryStaysOpen(t *testing.T) {
	m, _ := playingVideoModel(t)
	m = applyMsg(t, m, escKeyMsg)
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if !m.videoListOpen {
		t.Fatal("navigation closed the list")
	}
}

func TestVideoListRestartKeyReturnsToThePanel(t *testing.T) {
	m, pb, surface := windowVideoModel(t)
	m = enterVideoLayout(t, m)
	starts := len(pb.started)
	m = applyMsg(t, m, escKeyMsg)
	m = pressRestart(t, m)
	if m.videoListOpen || len(pb.started) != starts+1 {
		t.Fatalf("open=%v starts=%d", m.videoListOpen, len(pb.started)-starts)
	}
	m = applyMsg(t, m, playerState(PlayerStatePlaying))
	if got := surface.last(t); !got.Playing {
		t.Fatalf("panel = %+v", got)
	}
}

func TestVideoListInputsKeepEsc(t *testing.T) {
	m, _ := playingVideoModel(t)
	m = applyMsg(t, m, listKeyMsg)
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.filtering {
		t.Fatal("filter did not open in the list")
	}
	m = applyMsg(t, m, escKeyMsg)
	if m.filtering || !m.videoListOpen {
		t.Fatalf("filtering=%v open=%v", m.filtering, m.videoListOpen)
	}
	m = applyMsg(t, m, escKeyMsg)
	if m.videoListOpen {
		t.Fatal("Esc did not close the list")
	}
}

func TestVideoListKeysStayTextWhileComposing(t *testing.T) {
	m, _ := playingVideoModel(t)
	m.chatPanel.channel = "gaules"
	m.composing = true
	m = applyMsg(t, m, listKeyMsg)
	if m.videoListOpen || m.composeDraft != "l" {
		t.Fatalf("open=%v draft=%q", m.videoListOpen, m.composeDraft)
	}
	m = applyMsg(t, m, escKeyMsg)
	if m.videoListOpen || m.composing {
		t.Fatalf("Esc must only leave the input: open=%v composing=%v", m.videoListOpen, m.composing)
	}
}

func TestVideoListHelpOverItKeepsTheFrame(t *testing.T) {
	m, surface := playingVideoModel(t)
	m = applyMsg(t, m, escKeyMsg)
	applyMsg(t, m, tea.KeyMsg{Type: tea.KeyF1})
	if got := surface.last(t); !got.Still {
		t.Fatalf("help lost the frame: %+v", got)
	}
}

func TestVideoListGoesWithTheLayout(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlW, tea.KeyCtrlH, tea.KeyCtrlL} {
		m, _ := playingVideoModel(t)
		m = applyMsg(t, m, escKeyMsg)
		m = sendLayoutKey(t, m, key)
		if m.videoListOpen || m.showVideo {
			t.Fatalf("key %v: open=%v video=%v", key, m.videoListOpen, m.showVideo)
		}
	}
	m, _ := playingVideoModel(t)
	m = applyMsg(t, m, escKeyMsg)
	m = enterVideoLayout(t, sendLayoutKey(t, m, tea.KeyCtrlW))
	if m.videoListOpen || m.listVisible() {
		t.Fatal("the list came back open with the mode")
	}
}

func TestVideoPanelOffersTheListWithTheRestart(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m, _, _ := windowVideoModel(t)
		m.catalog, _ = i18n.New(lang)
		m = enterVideoLayout(t, m)
		row := strings.Split(ansi.Strip(m.View()), "\n")[0]
		if !strings.Contains(row, "Esc") || !strings.Contains(row, " r ") {
			t.Fatalf("%s: panel row %q", lang, row)
		}
	}
}

func TestVideoHiddenListKeyPointsAtTheOverlay(t *testing.T) {
	m, _ := playingVideoModel(t)
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("video.hidden_list_key")) {
		t.Fatalf("bar: %q", bar)
	}
}

func TestVideoHelpNamesTheListKey(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m, _ := playingVideoModel(t)
		m.catalog, _ = i18n.New(lang)
		m.helpVisible = true
		help := strings.Join(m.renderHelpLines(200), "\n")
		if !strings.Contains(help, m.t("video.help")) || !strings.Contains(m.t("video.help"), "Esc") {
			t.Fatalf("%s: %s", lang, help)
		}
	}
}

func TestRestartKeepsTheVolumeOnTheBar(t *testing.T) {
	m, _, _ := windowVideoModel(t)
	m = applyMsg(t, m, PlayerStatusMsg{Generation: m.playerGen, State: m.playerState, Volume: 40})
	m = enterVideoLayout(t, m)
	if !m.videoRestartOffered() {
		t.Fatal("no restart on offer")
	}
	// The bar is judged before the player answers: the fake always says 100.
	m = applyMsg(t, m, restartKey)
	if m.PlayerVolume() != 40 {
		t.Fatalf("after r: volume %d", m.PlayerVolume())
	}
	m = applyMsg(t, m, ChannelSelectedMsg{Channel: twitch.Channel{Login: m.playerLogin}})
	if m.PlayerVolume() != 40 {
		t.Fatalf("same channel again: volume %d", m.PlayerVolume())
	}
	m = applyMsg(t, m, ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	if m.PlayerVolume() != 100 {
		t.Fatalf("another channel: volume %d", m.PlayerVolume())
	}
}
