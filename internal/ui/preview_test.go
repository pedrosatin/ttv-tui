package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func openChannelList(t *testing.T, width, height int) (Model, *scenarioWorld) {
	t.Helper()
	m, w := newScenarioClassic(t, width, height)
	m = play(t, m, "enter") // open first category → channels
	return m, w
}

func TestSpaceOpensAndClosesPreviewWithoutMainVideo(t *testing.T) {
	m, w := openChannelList(t, 80, 24)

	m = play(t, m, "space")
	if len(w.playback.previewStarted) != 1 || w.playback.previewStarted[0] != "gaules" {
		t.Fatalf("previewStarted=%v", w.playback.previewStarted)
	}
	if m.PreviewState() != PreviewStateStarting {
		t.Fatalf("state=%q want starting", m.PreviewState())
	}
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	bar := ansi.Strip(m.StatusBar())
	want := m.t("preview.playing")
	// preview.playing is "Preview %s" / "Prévia %s"
	if !strings.Contains(bar, "Gaules") && !strings.Contains(bar, "gaules") {
		t.Fatalf("status bar missing preview channel: %q (fmt %q)", bar, want)
	}
	if ansi.StringWidth(bar) > 80 {
		t.Fatalf("status bar wider than 80: %d %q", ansi.StringWidth(bar), bar)
	}

	m = play(t, m, "space")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if m.previewOpen() {
		t.Fatalf("preview still open: %q", m.PreviewState())
	}
}

func TestEscClosesPreviewOnly(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	modeBefore := m.Mode()
	m = play(t, m, "esc")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if m.Mode() != modeBefore {
		t.Fatalf("Esc left the list mode: %q → %q", modeBefore, m.Mode())
	}
	if m.previewOpen() {
		t.Fatal("preview still open after Esc")
	}
}

func TestEnterClosesPreviewAndOpensChannel(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	_ = play(t, m, "enter")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d want 1", w.playback.previewStopped)
	}
	if len(w.playback.started) != 1 || w.playback.started[0] != "gaules" {
		t.Fatalf("main started=%v, want gaules", w.playback.started)
	}
}

func TestPreviewSwitchDebouncesHighlightMove(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	// play() installs scenarioTimer (which never fires). Use an instant timer
	// for the move so the debounce msg runs inside settle.
	m.timer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		return func() tea.Msg { return fn(time.Now()) }
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = settle(t, updated.(Model), cmd)
	if len(w.playback.previewStarted) < 2 {
		t.Fatalf("previewStarted=%v, want a second start for alanzoka", w.playback.previewStarted)
	}
	if got := w.playback.previewStarted[len(w.playback.previewStarted)-1]; got != "alanzoka" {
		t.Fatalf("last preview=%q want alanzoka", got)
	}
}

// Rapid j j k while a preview is open must not start a new preview until the
// highlight has been still for the debounce; then a single switch runs.
func TestPreviewDebounceSeveralKeysOneSwitch(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	before := len(w.playback.previewStarted)
	// j k j ends on alanzoka; scenarioTimer discards debounce ticks so no
	// switch runs until we fire the final previewSwitchMsg.
	m = play(t, m, "j k j")
	if len(w.playback.previewStarted) != before {
		t.Fatalf("preview switched during rapid moves: %v", w.playback.previewStarted)
	}
	hi, ok := m.HighlightedItem()
	if !ok || hi.Channel == nil || hi.Channel.Login != "alanzoka" {
		t.Fatalf("highlight after j k j = %+v, want alanzoka", hi)
	}
	updated, cmd := m.Update(previewSwitchMsg{gen: m.preview.switchGen})
	m = settle(t, updated.(Model), cmd)
	if len(w.playback.previewStarted) != before+1 {
		t.Fatalf("previewStarted=%v, want exactly one switch after debounce", w.playback.previewStarted)
	}
	if got := w.playback.previewStarted[len(w.playback.previewStarted)-1]; got != "alanzoka" {
		t.Fatalf("last preview=%q want alanzoka", got)
	}
}

func TestPreviewClosesWhenHighlightLeavesChannel(t *testing.T) {
	fav := scenarioChannels()[0]
	m, w := newScenarioClassic(t, 80, 24, WithFavorites([]catalog.Channel{fav}))
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	if !m.previewOpen() {
		t.Fatal("preview did not open on favorite")
	}
	// Home list: favorite, then categories. One j lands on a category row.
	m = play(t, m, "j")
	updated, cmd := m.Update(previewSwitchMsg{gen: m.preview.switchGen})
	m = settle(t, updated.(Model), cmd)
	if m.previewOpen() {
		t.Fatalf("preview still open on non-channel row: %q", m.PreviewState())
	}
	if w.playback.previewStopped < 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
}

func TestStopPreviewDuringStartEndsStopped(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	// Apply Space without settling the StartPreview cmd so awaiting stays set
	// (the fake bridge would otherwise deliver Starting inside play/settle).
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if m.preview.awaiting == 0 {
		t.Fatal("expected awaiting Start after Space")
	}
	startCmd := cmd
	// Close before the Start reply (StopPreview read gen 0).
	m = play(t, m, "space")
	if !m.preview.stopAfterStart || m.preview.awaiting == 0 {
		t.Fatalf("stopAfterStart=%v awaiting=%d", m.preview.stopAfterStart, m.preview.awaiting)
	}
	if m.previewOpen() {
		t.Fatal("UI still shows an open preview after close")
	}
	// Late Start reply must stop that generation again and stay closed.
	m = settle(t, m, startCmd)
	if m.previewOpen() || m.preview.stopAfterStart {
		t.Fatalf("preview resurrected: state=%q stopAfter=%v", m.PreviewState(), m.preview.stopAfterStart)
	}
	if w.playback.previewStopped < 2 {
		t.Fatalf("previewStopped=%d, want Stop again after the Start reply", w.playback.previewStopped)
	}
	// Events from the orphaned generation must not reopen the bar.
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	if m.previewOpen() {
		t.Fatal("stale Playing event reopened the preview")
	}
}

func TestSpaceDoesNotOpenPreviewInHostedWindow(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m.hosted.host = &recordingHost{}
	_ = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("hosted window started preview: %v", w.playback.previewStarted)
	}
}

func TestPreviewClosesOnPlatformSearchAndCategories(t *testing.T) {
	for _, seq := range []string{"p", "ctrl+f", "ctrl+r"} {
		t.Run(seq, func(t *testing.T) {
			m, w := openChannelList(t, 80, 24)
			m = play(t, m, "space")
			m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
			m = play(t, m, seq)
			if m.previewOpen() {
				t.Fatalf("preview still open after %s: %q", seq, m.PreviewState())
			}
			if w.playback.previewStopped < 1 {
				t.Fatalf("%s: previewStopped=%d", seq, w.playback.previewStopped)
			}
		})
	}
}

func TestPreviewFilterSlashClosesPreview(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	m = play(t, m, "/")
	if m.previewOpen() {
		t.Fatalf("preview still open after /: %q", m.PreviewState())
	}
	if w.playback.previewStopped < 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if !m.list.filtering {
		t.Fatal("filter mode not entered")
	}
}

func TestPreviewUnknownFailureShowsLastLineDetail(t *testing.T) {
	m, _ := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{
		Generation: 1, State: PreviewStateFailed, Login: "gaules",
		Failure: PlayerFailureUnknown, LastLine: "streamlink exited with code 1",
	})
	parts := m.statusParts()
	if !strings.Contains(parts.primary, m.t("preview.fail.unknown")) {
		t.Fatalf("primary=%q", parts.primary)
	}
	if !strings.Contains(parts.detail, "streamlink exited with code 1") {
		t.Fatalf("detail=%q, want lastLine", parts.detail)
	}
}

func TestPreviewStatusFits80ColumnsENAndPTBR(t *testing.T) {
	states := []PreviewStatusMsg{
		{Generation: 1, State: PreviewStateStarting, Login: "gaules"},
		{Generation: 1, State: PreviewStatePlaying, Login: "gaules"},
		{Generation: 1, State: PreviewStateBreak, Login: "gaules", BreakElapsed: 12 * time.Second},
		{Generation: 1, State: PreviewStateOffline, Login: "gaules", Failure: PlayerFailureOffline},
		{Generation: 1, State: PreviewStateFailed, Login: "gaules", Failure: PlayerFailureUnknown, LastLine: "boom"},
	}
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			for _, st := range states {
				m, _ := openChannelList(t, 80, 24)
				if lang == i18n.LangEN {
					cat, err := i18n.New(i18n.LangEN)
					if err != nil {
						t.Fatal(err)
					}
					m.catalog = cat
				}
				m = play(t, m, "space")
				m = feed(t, m, st)
				bar := ansi.Strip(m.StatusBar())
				if w := ansi.StringWidth(bar); w > 80 {
					t.Fatalf("state=%s bar width %d > 80: %q", st.State, w, bar)
				}
			}
		})
	}
}

func TestPreviewDoesNotRecordRecent(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	_ = w
	before := len(m.list.recents) + len(m.list.pendingRecents)
	m = play(t, m, "space")
	after := len(m.list.recents) + len(m.list.pendingRecents)
	if after != before {
		t.Fatalf("preview recorded recent: before=%d after=%d", before, after)
	}
}

func TestCtrlXClosesPreviewWhenNoMain(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	_ = play(t, m, "ctrl+x")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
}

func TestSpaceWithMainVideoStillPauses(t *testing.T) {
	// Ticket 01 keeps pause when main video is active; ticket 02 changes this.
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	_ = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("preview started with main video: %v", w.playback.previewStarted)
	}
	if w.playback.pauses != 1 {
		t.Fatalf("pauses=%d want 1", w.playback.pauses)
	}
}

// O usuário, sem vídeo, aperta espaço num canal da lista: a prévia abre e a
// barra mostra o estado tocando (regra 9: o golden congela a tela com a prévia
// aberta). Esc é coberto por TestEscClosesPreviewOnly.
func TestCenarioPreviaSemVideo(t *testing.T) {
	eachSize(t, "previa-sem-video", func(t *testing.T, width, height int) Model {
		m, w := openChannelList(t, width, height)
		m = play(t, m, "space")
		if len(w.playback.previewStarted) != 1 {
			t.Fatalf("prévia não abriu: %v", w.playback.previewStarted)
		}
		m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
		if m.PreviewState() != PreviewStatePlaying {
			t.Fatalf("prévia não ficou tocando: %q", m.PreviewState())
		}
		return m
	})
}
