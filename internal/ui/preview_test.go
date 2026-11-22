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

func TestSpaceOpensPreviewInHostedWindowWithoutMain(t *testing.T) {
	m, w := openChannelList(t, 120, 30)
	m.hosted.host = &recordingHost{}
	m = play(t, m, "space")
	if len(w.playback.previewStarted) != 1 || w.playback.previewStarted[0] != "gaules" {
		t.Fatalf("hosted window previewStarted = %v, want [gaules]", w.playback.previewStarted)
	}
	if !strings.Contains(m.hostedIdleText(), "gaules") {
		t.Fatalf("hostedIdleText during starting = %q, want channel loading text", m.hostedIdleText())
	}
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	if got := m.hostedIdleText(); got != "" {
		t.Fatalf("hostedIdleText while preview playing = %q, want empty", got)
	}
	m = play(t, m, "esc")
	if m.previewOpen() {
		t.Fatalf("preview still open after Esc: %q", m.PreviewState())
	}
	if !strings.Contains(m.hostedIdleText(), "Gaules") {
		t.Fatalf("hostedIdleText after closing preview = %q, want idle channel prompt", m.hostedIdleText())
	}
}

func TestSpaceDoesNotOpenPreviewInHostedWindowWithMain(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m.hosted.host = &recordingHost{}
	m = feed(t, m, PlayerStatusMsg{State: PlayerStatePlaying, Login: "gaules"})
	_ = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("hosted window with active main started preview: %v", w.playback.previewStarted)
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

func TestSpaceWithMainVideoOpensPreviewAndMutes(t *testing.T) {
	for _, state := range []PlayerState{PlayerStatePlaying, PlayerStatePaused, PlayerStateBreak, PlayerStateStarting} {
		t.Run(string(state), func(t *testing.T) {
			m, w := openChannelList(t, 80, 24)
			m = feed(t, m, PlayerStatusMsg{Generation: 1, State: state, Login: "gaules", Volume: 80})
			m = play(t, m, "space")
			if len(w.playback.previewStarted) != 1 || w.playback.previewStarted[0] != "gaules" {
				t.Fatalf("previewStarted=%v", w.playback.previewStarted)
			}
			if w.playback.pauses != 0 {
				t.Fatalf("list Space paused main: pauses=%d", w.playback.pauses)
			}
			if !w.playback.mainMutedForPrev {
				t.Fatal("MuteMainForPreview was not called")
			}
			m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
			m = play(t, m, "space")
			if !w.playback.mainMuteRestored {
				t.Fatal("RestoreMainMute was not called on close")
			}
			if m.previewOpen() {
				t.Fatal("preview still open after Space")
			}
			if m.PlayerState() != state {
				t.Fatalf("main state changed: %q → %q", state, m.PlayerState())
			}
		})
	}
}

func TestSpaceOnChatStillPausesWithMainVideo(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = chatLines(t, m, [2]string{"Fulano", "oi"})
	m = play(t, m, "tab")
	if m.Focus() != FocusChat {
		t.Fatalf("focus=%v want chat", m.Focus())
	}
	_ = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("chat Space opened preview: %v", w.playback.previewStarted)
	}
	if w.playback.pauses != 1 {
		t.Fatalf("pauses=%d want 1", w.playback.pauses)
	}
}

func TestSpaceOnNonChannelWarnsWithoutPausing(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "esc") // back to categories
	if m.Mode() != ModeCategories {
		t.Fatalf("mode=%q want categories", m.Mode())
	}
	m = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("preview started on category: %v", w.playback.previewStarted)
	}
	if w.playback.pauses != 0 {
		t.Fatalf("category Space paused: pauses=%d", w.playback.pauses)
	}
	if got := m.StatusNotice(); got != m.t("preview.channels_only") {
		t.Fatalf("notice=%q want preview.channels_only", got)
	}
}

func TestEnterOnPreviewOfMainChannelOnlyCloses(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	beforeStarts := len(w.playback.started)
	m = play(t, m, "enter")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if len(w.playback.started) != beforeStarts {
		t.Fatalf("Enter restarted main: started=%v", w.playback.started)
	}
	if m.previewOpen() {
		t.Fatal("preview still open")
	}
	if !w.playback.mainMuteRestored {
		t.Fatal("mute not restored")
	}
}

func TestEnterOnPreviewOfOtherChannelSwitchesMain(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "j") // alanzoka
	m = play(t, m, "space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "alanzoka"})
	m = play(t, m, "enter")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if len(w.playback.started) != 1 || w.playback.started[0] != "alanzoka" {
		t.Fatalf("main started=%v, want alanzoka", w.playback.started)
	}
	if m.previewOpen() {
		t.Fatal("preview still open after Enter")
	}
}

func TestCtrlXWithMainAndPreviewStopsBoth(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "j space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "alanzoka"})
	w.playback.cmdOrder = nil
	m = play(t, m, "ctrl+x")
	if w.playback.previewStopped != 1 {
		t.Fatalf("previewStopped=%d", w.playback.previewStopped)
	}
	if w.playback.stopped != 1 {
		t.Fatalf("main stopped=%d", w.playback.stopped)
	}
	if !w.playback.mainMuteRestored {
		t.Fatal("mute not restored when Ctrl+X closed preview")
	}
	if m.previewOpen() {
		t.Fatal("preview still open after Ctrl+X")
	}
	if !orderHasBefore(w.playback.cmdOrder, "restore", "stop") {
		t.Fatalf("restore must run before stop, got %v", w.playback.cmdOrder)
	}
}

func TestEnterOnOtherChannelRestoresMuteBeforeStart(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "j space")
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "alanzoka"})
	w.playback.cmdOrder = nil
	m = play(t, m, "enter")
	if !w.playback.mainMuteRestored {
		t.Fatal("mute not restored")
	}
	if !orderHasBefore(w.playback.cmdOrder, "restore", "start") {
		t.Fatalf("restore must run before start, got %v", w.playback.cmdOrder)
	}
	if m.previewOpen() {
		t.Fatal("preview still open")
	}
}

// orderHasBefore reports whether a appears before b in order.
func orderHasBefore(order []string, a, b string) bool {
	ai, bi := -1, -1
	for i, s := range order {
		if s == a && ai < 0 {
			ai = i
		}
		if s == b && bi < 0 {
			bi = i
		}
	}
	return ai >= 0 && bi >= 0 && ai < bi
}

func TestPreviewPreservesAlreadyMutedMain(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80, Muted: true})
	m = play(t, m, "space")
	if !w.playback.mainMutedForPrev {
		t.Fatal("MuteMainForPreview was not called")
	}
	m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
	m = play(t, m, "esc")
	if !w.playback.mainMuteRestored {
		t.Fatal("RestoreMainMute was not called")
	}
	// The fake bridge only records the call; the app bridge restores the saved
	// value (true). UI mute flag is unchanged by the restore cmd itself.
	if m.PlayerState() != PlayerStatePlaying {
		t.Fatalf("main state=%q", m.PlayerState())
	}
}

func TestSpaceInSearchKeepsPrecedence(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = play(t, m, "ctrl+f")
	if !m.list.search.active {
		t.Fatal("search not active")
	}
	m = play(t, m, "space")
	if len(w.playback.previewStarted) != 0 {
		t.Fatalf("search Space opened preview: %v", w.playback.previewStarted)
	}
	if w.playback.pauses != 0 {
		t.Fatalf("search Space paused: %d", w.playback.pauses)
	}
	if m.list.search.input != " " {
		t.Fatalf("search input=%q want space", m.list.search.input)
	}
}

func TestPreviewFailureLeavesMainIntact(t *testing.T) {
	m, w := openChannelList(t, 80, 24)
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 70, Muted: false})
	w.playback.previewStartMsg = PreviewStatusMsg{
		Generation: 1, State: PreviewStateFailed, Failure: PlayerFailureNetwork, LastLine: "no route",
	}
	m = play(t, m, "j space")
	if !w.playback.mainMutedForPrev {
		t.Fatal("main was not muted before failing preview")
	}
	if w.playback.stopped != 0 {
		t.Fatalf("failing preview stopped main: %d", w.playback.stopped)
	}
	if m.PlayerState() != PlayerStatePlaying || m.PlayerLogin() != "gaules" {
		t.Fatalf("main changed: state=%q login=%q", m.PlayerState(), m.PlayerLogin())
	}
	if m.PreviewState() != PreviewStateFailed {
		t.Fatalf("preview state=%q", m.PreviewState())
	}
	m = play(t, m, "esc")
	if !w.playback.mainMuteRestored {
		t.Fatal("closing failed preview did not restore mute")
	}
	if m.previewOpen() {
		t.Fatal("failed preview still open after Esc")
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

// Com o principal tocando, espaço noutro canal abre a prévia, silencia o
// principal e a barra mostra a prévia com "mudo" (regra 9).
func TestCenarioPreviaComVideo(t *testing.T) {
	eachSize(t, "previa-com-video", func(t *testing.T, width, height int) Model {
		m, w := openChannelList(t, width, height)
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80})
		m.player.windowNotice = false
		m = play(t, m, "j space")
		if len(w.playback.previewStarted) != 1 || w.playback.previewStarted[0] != "alanzoka" {
			t.Fatalf("prévia: %v", w.playback.previewStarted)
		}
		if !w.playback.mainMutedForPrev {
			t.Fatal("principal não foi silenciado")
		}
		// Evento de mudo que o pump mandaria depois do SetMute da prévia.
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 80, Muted: true})
		m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "alanzoka"})
		if m.PreviewState() != PreviewStatePlaying {
			t.Fatalf("prévia: %q", m.PreviewState())
		}
		return m
	})
}
