package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
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

// playingVideoModel is the video layout with a stream playing at 120x32.
func playingVideoModel(t *testing.T) (Model, *fakeSurface) {
	t.Helper()
	m, _, _ := setupTwoPanelModel(t)
	surface := &fakeSurface{}
	WithVideo(true, "")(&m)
	WithVideoSurface(surface)(&m)
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 32})
	for range 4 {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
	}
	if m.LayoutMode() != LayoutVideo {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
	return m, surface
}

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func playerState(state PlayerState) PlayerStatusMsg {
	return PlayerStatusMsg{Generation: 1, State: state, Login: "gaules", Volume: 85}
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
	want := VideoPanel{Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true, Playing: true}
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
