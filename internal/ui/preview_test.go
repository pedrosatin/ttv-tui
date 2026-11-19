package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// O usuário, sem vídeo, aperta espaço num canal da lista: a prévia abre na
// barra; Esc fecha sem sair da lista de canais.
func TestCenarioPreviaSemVideo(t *testing.T) {
	eachSize(t, "previa-sem-video", func(t *testing.T, width, height int) Model {
		m, w := openChannelList(t, width, height)
		m = play(t, m, "space")
		if len(w.playback.previewStarted) != 1 {
			t.Fatalf("prévia não abriu: %v", w.playback.previewStarted)
		}
		m = feed(t, m, PreviewStatusMsg{Generation: 1, State: PreviewStatePlaying, Login: "gaules"})
		m = play(t, m, "esc")
		if w.playback.previewStopped != 1 {
			t.Fatalf("prévia não fechou: stopped=%d", w.playback.previewStopped)
		}
		return m
	})
}
