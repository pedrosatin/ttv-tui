package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// openedModel is the model as Run builds it, at 120x32: every option goes through
// New, which is where the opening layout is decided.
func openedModel(t *testing.T, lang string, opts ...Option) (Model, *fakePlayback) {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatal(err)
	}
	pb := &fakePlayback{}
	opts = append([]Option{WithPlayback(pb), WithChat(&fakeChat{hasToken: true, username: "tester"})}, opts...)
	return applyMsg(t, New(cat, opts...), tea.WindowSizeMsg{Width: 120, Height: 32}), pb
}

func readyVideoModel(t *testing.T, lang string, opts ...Option) (Model, *fakePlayback, *fakeSurface) {
	t.Helper()
	surface := &fakeSurface{}
	opts = append([]Option{WithVideo(true, ""), WithVideoSurface(surface)}, opts...)
	m, pb := openedModel(t, lang, opts...)
	return m, pb, surface
}

func selectChannel(t *testing.T, m Model, login string) Model {
	t.Helper()
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: login}})
	m = updated.(Model)
	if cmd != nil {
		m = applyMsg(t, m, cmd())
	}
	return m
}

func TestOpeningWithVideoReadyIsTheVideoLayout(t *testing.T) {
	m, _, _ := readyVideoModel(t, i18n.LangEN)
	if m.LayoutMode() != LayoutVideo {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
	if m.videoChatOff || !m.showChat {
		t.Fatalf("the chat of the mode is off: off=%v show=%v", m.videoChatOff, m.showChat)
	}
}

// The mode hides the list, and with nothing playing yet the list is what the user
// came for: it opens over the panel, in the list focus.
func TestOpeningWithVideoReadyShowsTheList(t *testing.T) {
	m, _, surface := readyVideoModel(t, i18n.LangEN)
	if !m.listVisible() || m.Focus() != FocusList {
		t.Fatalf("list visible=%v focus=%v", m.listVisible(), m.Focus())
	}
	got := surface.last(t)
	if got.Visible || got.Playing || got.Rows == 0 {
		t.Fatalf("panel under the list = %+v", got)
	}
}

func TestOpeningRespectsTheChatStripConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opts    []Option
		rows    int
		rowOnly int
	}{
		{"defaults", nil, defaultVideoChatRows, 1},
		{"rows", []Option{WithVideoChatRows(5)}, 5, 1},
		{"on top", []Option{WithVideoChatRows(5), WithVideoChatOnTop(true)}, 5, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, surface := readyVideoModel(t, i18n.LangEN, tc.opts...)
			got := surface.last(t)
			if want := 32 - 1 - tc.rows; got.Rows != want || got.Cols != 120 || got.Row != tc.rowOnly {
				t.Fatalf("panel = %+v, want %d rows at row %d", got, want, tc.rowOnly)
			}
		})
	}
}

// The first frame of the session has no reason to look like a failure.
func TestOpeningSaysTheModeIsOn(t *testing.T) {
	for _, tc := range []struct{ lang, want string }{
		{i18n.LangEN, "Embedded video on"},
		{i18n.LangPTBR, "Vídeo embutido ativo"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			for _, width := range []int{80, 120} {
				m, _, _ := readyVideoModel(t, tc.lang)
				m = resized(t, m, width, 32)
				if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, tc.want) {
					t.Fatalf("width %d: bar = %q", width, bar)
				}
				if m.statusNoticeWarns || m.statusParts().tone != toneNeutral {
					t.Fatalf("width %d: the notice reads as a warning", width)
				}
				if m.videoStateRows != 0 || m.Height() != 32 {
					t.Fatalf("width %d: a state line took rows=%d", width, m.videoStateRows)
				}
			}
		})
	}
}

// A notice that came first keeps its place: it has a reason to be read.
func TestOpeningDoesNotReplaceAnEarlierNotice(t *testing.T) {
	m, _, _ := readyVideoModel(t, i18n.LangEN, WithStatusNotice("earlier notice"))
	if got := m.StatusNotice(); got != "earlier notice" {
		t.Fatalf("notice = %q", got)
	}
}

func TestOpeningNoticeGivesWayToTheListState(t *testing.T) {
	m, _, _ := readyVideoModel(t, i18n.LangEN)
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, m.t("video.list_open")) || strings.Contains(bar, m.t("video.active")) {
		t.Fatalf("bar = %q", bar)
	}
	// Nothing plays yet: the player has no state to put after a separator.
	if extra := m.statusParts().extra; len(extra) != 0 {
		t.Fatalf("empty context on the bar: %q in %q", extra, bar)
	}
}

func TestOpeningAChannelInTheOpeningLayoutIsEmbedded(t *testing.T) {
	m, pb, surface := readyVideoModel(t, i18n.LangEN)
	m = selectChannel(t, m, "gaules")
	if len(pb.embedded) != 1 || !pb.embedded[0] {
		t.Fatalf("embedded = %v", pb.embedded)
	}
	if !m.playerEmbedded || m.videoRestartOffered() {
		t.Fatalf("embedded=%v restart offered=%v", m.playerEmbedded, m.videoRestartOffered())
	}
	if m.videoListOpen || m.listVisible() || m.LayoutMode() != LayoutVideo {
		t.Fatalf("open=%v visible=%v layout=%q", m.videoListOpen, m.listVisible(), m.LayoutMode())
	}
	if got := surface.last(t); !got.Visible || got.Rows == 0 {
		t.Fatalf("panel = %+v", got)
	}
	// Nothing opened a window, so nothing announces one.
	m = applyMsg(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	if m.playerWindowNotified || m.PlayerWindowNotice() {
		t.Fatal("announced an mpv window that does not exist")
	}
	if !surface.last(t).Playing {
		t.Fatalf("panel = %+v", surface.last(t))
	}
}

func TestOpeningWithoutTheModeKeepsTheSplit(t *testing.T) {
	surface := &fakeSurface{}
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{"no config", nil},
		{"not requested", []Option{WithVideo(false, ""), WithVideoSurface(surface)}},
		{"no surface: output is not a terminal", []Option{WithVideo(true, "")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, pb := openedModel(t, i18n.LangEN, tc.opts...)
			if m.LayoutMode() != LayoutSplit || m.videoListOpen || m.showVideo {
				t.Fatalf("layout = %q open=%v", m.LayoutMode(), m.videoListOpen)
			}
			if got := m.StatusNotice(); got != "" {
				t.Fatalf("notice = %q", got)
			}
			m = selectChannel(t, m, "gaules")
			if len(pb.embedded) != 1 || pb.embedded[0] {
				t.Fatalf("embedded = %v", pb.embedded)
			}
			if len(surface.panels) != 0 {
				t.Fatalf("panels = %+v", surface.panels)
			}
		})
	}
}

func TestOpeningWithoutSupportKeepsTheSplitAndSaysWhy(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		for _, reason := range videoFailureReasons {
			t.Run(lang+"/"+reason, func(t *testing.T) {
				m, pb := openedModel(t, lang, WithVideo(true, reason))
				if m.LayoutMode() != LayoutSplit {
					t.Fatalf("layout = %q", m.LayoutMode())
				}
				if got := stateText(m.View(), m.videoStateRows); got != noSpaces(m.t(reason)) {
					t.Fatalf("state line = %q, want %q", got, m.t(reason))
				}
				if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, m.t("video.active")) {
					t.Fatalf("bar claims the mode is on: %q", bar)
				}
				selectChannel(t, m, "gaules")
				if len(pb.embedded) != 1 || pb.embedded[0] {
					t.Fatalf("embedded = %v", pb.embedded)
				}
			})
		}
	}
}

// The panel never opens over a reason, even if something could draw it.
func TestOpeningReasonWinsOverASurface(t *testing.T) {
	m, _ := openedModel(t, i18n.LangEN, WithVideo(true, "video.no_mpv"), WithVideoSurface(&fakeSurface{}))
	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
}

func TestLeavingTheOpeningLayoutNeverRestartsThePlayer(t *testing.T) {
	m, pb, _ := readyVideoModel(t, i18n.LangEN)
	m = selectChannel(t, m, "gaules")
	m = applyMsg(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = sendLayoutKey(t, m, tea.KeyCtrlW)
	if m.LayoutMode() != LayoutSplit {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
	if !m.videoRestartOffered() || !m.videoAudioOnly() {
		t.Fatalf("offered=%v audio only=%v", m.videoRestartOffered(), m.videoAudioOnly())
	}
	for range 10 {
		for _, key := range []tea.KeyType{tea.KeyCtrlW, tea.KeyCtrlH, tea.KeyCtrlW, tea.KeyCtrlL} {
			m = sendLayoutKey(t, m, key)
		}
	}
	if len(pb.started) != 1 || pb.stopped != 0 {
		t.Fatalf("started=%v stopped=%d", pb.started, pb.stopped)
	}
}

func TestCtrlWCyclesFromTheOpeningLayout(t *testing.T) {
	m, _, _ := readyVideoModel(t, i18n.LangEN)
	m = selectChannel(t, m, "gaules")
	for _, want := range []LayoutMode{LayoutSplit, LayoutChat, LayoutList, LayoutFocus, LayoutVideo, LayoutSplit} {
		m = sendLayoutKey(t, m, tea.KeyCtrlW)
		if m.LayoutMode() != want {
			t.Fatalf("got %q want %q", m.LayoutMode(), want)
		}
	}
}

// Before any channel, the video layout is the one layout key that has somewhere to
// go: "no chat yet" would be a key that did nothing.
func TestLayoutKeysLeaveTheOpeningLayoutBeforeAnyChannel(t *testing.T) {
	for name, key := range map[string]tea.KeyType{"ctrl+w": tea.KeyCtrlW, "ctrl+h": tea.KeyCtrlH, "ctrl+l": tea.KeyCtrlL} {
		t.Run(name, func(t *testing.T) {
			m, _, surface := readyVideoModel(t, i18n.LangEN)
			m = sendLayoutKey(t, m, key)
			if m.LayoutMode() != LayoutSplit || m.showVideo || m.videoListOpen {
				t.Fatalf("layout = %q video=%v open=%v", m.LayoutMode(), m.showVideo, m.videoListOpen)
			}
			if !m.showList || !m.showChat {
				t.Fatalf("list=%v chat=%v: the channel would open with a panel missing", m.showList, m.showChat)
			}
			if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, m.t("layout.no_chat")) {
				t.Fatalf("bar = %q", bar)
			}
			if got := surface.last(t); got != (VideoPanel{}) {
				t.Fatalf("panel = %+v", got)
			}
		})
	}
}

// Esc still goes to the panel, and the panel still says how to come back.
func TestOpeningListClosesToAnEmptyPanel(t *testing.T) {
	m, _, surface := readyVideoModel(t, i18n.LangEN)
	m = applyMsg(t, m, escKeyMsg)
	if m.listVisible() || m.LayoutMode() != LayoutVideo {
		t.Fatalf("visible=%v layout=%q", m.listVisible(), m.LayoutMode())
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if len(lines) != 32 || !strings.Contains(lines[0], m.t("video.placeholder")) {
		t.Fatalf("view:\n%s", strings.Join(lines, "\n"))
	}
	if got := surface.last(t); !got.Visible || got.Playing || got.Still {
		t.Fatalf("panel = %+v", got)
	}
	if m = applyMsg(t, m, escKeyMsg); !m.listVisible() {
		t.Fatal("Esc did not reopen the list")
	}
}

func TestOpeningFitsEveryTerminal(t *testing.T) {
	for _, width := range []int{20, 40, 80, 120} {
		for _, height := range []int{1, 2, 3, 7, 9, 10, 24} {
			m, _, _ := readyVideoModel(t, i18n.LangPTBR)
			m = resized(t, m, width, height)
			rows := strings.Split(m.View(), "\n")
			if len(rows) != height {
				t.Fatalf("%dx%d: %d rows", width, height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("%dx%d: row too wide: %q", width, height, row)
				}
			}
		}
	}
}

func TestOpeningHelpAndBarDescribeTheMode(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := readyVideoModel(t, lang)
			m.helpVisible = true
			help := strings.Join(m.renderHelpLines(200), "\n")
			m.helpVisible = false
			if !strings.Contains(help, m.t("video.help")) || strings.Contains(help, m.t("help.video_enable")) {
				t.Fatalf("help:\n%s", help)
			}
			// Enter is the key that chooses a channel, and a key ends the notice.
			m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyDown})
			m = selectChannel(t, m, "gaules")
			m = applyMsg(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
			if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.layoutStatusText()) {
				t.Fatalf("bar = %q, want %q", bar, m.layoutStatusText())
			}
		})
	}
}
