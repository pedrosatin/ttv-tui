package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var videoChatKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}

// playingVideoSession is playingVideoModel with the playback kept, so a test can
// see that nothing restarted it.
func playingVideoSession(t *testing.T) (Model, *fakePlayback, *fakeSurface) {
	t.Helper()
	m, pb, surface := windowVideoModel(t)
	m = pressRestart(t, enterVideoLayout(t, m))
	return applyMsg(t, m, playerState(PlayerStatePlaying)), pb, surface
}

func pressVideoChat(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(videoChatKey)
	return updated.(Model), cmd
}

func TestVideoChatToggleResizesThePanel(t *testing.T) {
	m, pb, surface := playingVideoSession(t)
	starts := len(pb.started)
	available := m.height - 1
	on := surface.last(t)
	if on.Rows != available-defaultVideoChatRows || on.Cols != m.width || !on.Visible || !on.Playing {
		t.Fatalf("panel with the chat on = %+v", on)
	}
	m, cmd := pressVideoChat(t, m)
	off := surface.last(t)
	if off.Rows != available || off.Cols != m.width || off.Row != 1 || !off.Visible || !off.Playing {
		t.Fatalf("panel with the chat off = %+v", off)
	}
	if !clearsScreen(cmd) {
		t.Fatal("the image moved without a clear: its old rows would keep stale pixels")
	}
	m, _ = pressVideoChat(t, m)
	if got := surface.last(t); got != on {
		t.Fatalf("panel after the chat came back = %+v, want %+v", got, on)
	}
	if len(pb.started) != starts || pb.stopped != 0 {
		t.Fatalf("toggling the chat touched the player: starts %d→%d, stops %d", starts, len(pb.started), pb.stopped)
	}
	if m.LayoutMode() != LayoutVideo {
		t.Fatalf("layout = %q", m.LayoutMode())
	}
}

func TestVideoChatOffReservesNoRows(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {80, 24}, {60, 7}, {200, 60}} {
		m, _, surface := playingVideoSession(t)
		m = applyMsg(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m, _ = pressVideoChat(t, m)
		panel := surface.last(t)
		if panel.Rows != size[1]-1 || panel.Cols != size[0] {
			t.Fatalf("%v: panel = %+v, want every row but the bar", size, panel)
		}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, line := range lines[:panel.Rows] {
			if strings.TrimSpace(line) != "" {
				t.Fatalf("%v: panel row %d carries %q", size, i, line)
			}
		}
		if m.chatColumnVisible() || m.composing {
			t.Fatalf("%v: chat still claims the screen", size)
		}
	}
}

func TestVideoChatStripNeverOverlapsThePanel(t *testing.T) {
	for _, size := range [][2]int{{120, 32}, {80, 24}, {90, 15}, {60, 12}, {200, 60}} {
		m, _, surface := playingVideoSession(t)
		m = applyMsg(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		available := size[1] - 1
		panel := surface.last(t)
		strip := available - panel.Rows
		if strip != defaultVideoChatRows || panel.Row != 1 || panel.Cols != size[0] {
			t.Fatalf("%v: panel = %+v leaves %d rows to the chat", size, panel, strip)
		}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, line := range lines[:panel.Rows] {
			if strings.TrimSpace(line) != "" {
				t.Fatalf("%v: panel row %d carries %q", size, i, line)
			}
		}
		if !strings.Contains(lines[panel.Rows], "Chat") {
			t.Fatalf("%v: the strip does not start right below the panel:\n%s", size, strings.Join(lines, "\n"))
		}
		for i, line := range lines[panel.Rows:available] {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v: strip row %d is wider than the screen", size, i)
			}
		}
	}
}

func TestVideoChatStripShrinksBeforeThePanelGoesOff(t *testing.T) {
	m, _, surface := playingVideoSession(t)
	m = applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: defaultVideoChatRows + 1})
	if got := surface.last(t); got.Visible {
		t.Fatalf("a panel with no room for the chat strip = %+v", got)
	}
	_, _ = pressVideoChat(t, m)
	if got := surface.last(t); !got.Visible || got.Rows != defaultVideoChatRows {
		t.Fatalf("with the chat off the panel = %+v", got)
	}
}

func TestVideoChatRowsOption(t *testing.T) {
	m, _, surface := windowVideoModel(t)
	WithVideoChatRows(5)(&m)
	m = pressRestart(t, enterVideoLayout(t, m))
	m = applyMsg(t, m, playerState(PlayerStatePlaying))
	if got := surface.last(t); got.Rows != m.height-1-5 {
		t.Fatalf("panel = %+v", got)
	}
	WithVideoChatRows(0)(&m)
	if got := m.videoChatStripRows(m.height - 1); got != 5 {
		t.Fatalf("a zero height replaced the configured one: %d", got)
	}
}

// keyMapBindings lists every binding of the key map by field name, so a new
// binding cannot skip the collision check below.
func keyMapBindings(t *testing.T) map[string]key.Binding {
	t.Helper()
	km := reflect.ValueOf(defaultKeyMap())
	out := map[string]key.Binding{}
	for i := range km.NumField() {
		b, ok := km.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("field %s is not a key.Binding", km.Type().Field(i).Name)
		}
		out[km.Type().Field(i).Name] = b
	}
	return out
}

func TestVideoChatKeyCollidesWithNothing(t *testing.T) {
	bindings := keyMapBindings(t)
	toggle, ok := bindings["ToggleVideoChat"]
	if !ok {
		t.Fatal("the key map has no ToggleVideoChat binding")
	}
	if len(toggle.Keys()) == 0 {
		t.Fatal("ToggleVideoChat has no key")
	}
	reserved := []string{"ctrl+q", "?", "ctrl+w", "ctrl+h", "ctrl+l", "ctrl+x", "ctrl+t", "ctrl+f", "ctrl+r", "i", "f", "r", "y", "/", "l", "tab", "ctrl+c", "esc", "enter", " ", "q"}
	for _, k := range toggle.Keys() {
		for _, r := range reserved {
			if k == r {
				t.Fatalf("ToggleVideoChat uses the reserved key %q", k)
			}
		}
		for name, b := range bindings {
			if name == "ToggleVideoChat" {
				continue
			}
			for _, other := range b.Keys() {
				if other == k {
					t.Fatalf("ToggleVideoChat key %q is also %s", k, name)
				}
			}
		}
	}
}

func TestVideoChatStateOnTheBarAndInTheHelp(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m, _, _ := playingVideoSession(t)
		m.catalog, _ = i18n.New(lang)
		on := m.t("layout.mode.video")
		off := m.t("layout.mode.video_nochat")
		if off == "layout.mode.video_nochat" || off == on {
			t.Fatalf("%s: no label for the chat off", lang)
		}
		if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, on) {
			t.Fatalf("%s: bar with the chat on: %q", lang, bar)
		}
		m, _ = pressVideoChat(t, m)
		if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, off) {
			t.Fatalf("%s: bar with the chat off: %q", lang, bar)
		}
		m.helpVisible = true
		help := strings.Join(m.renderHelpLines(200), "\n")
		if !strings.Contains(help, m.t("video.help")) || !strings.Contains(m.t("video.help"), "c ") {
			t.Fatalf("%s: help does not name the key:\n%s", lang, help)
		}
	}
}

func TestVideoChatKeyOutsideTheVideoBody(t *testing.T) {
	type step struct {
		name string
		prep func(*testing.T) Model
	}
	split := func(t *testing.T) Model {
		m, _, _ := setupTwoPanelModel(t)
		return applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 32})
	}
	flagged := func(t *testing.T) Model {
		m, _, _ := windowVideoModel(t)
		return m
	}
	for _, s := range []step{
		{"split without the flag", split},
		{"chat without the flag", func(t *testing.T) Model { return sendLayoutKey(t, split(t), tea.KeyCtrlW) }},
		{"list without the flag", func(t *testing.T) Model {
			return sendLayoutKey(t, sendLayoutKey(t, split(t), tea.KeyCtrlW), tea.KeyCtrlW)
		}},
		{"focus without the flag", func(t *testing.T) Model {
			m := split(t)
			for range 3 {
				m = sendLayoutKey(t, m, tea.KeyCtrlW)
			}
			return m
		}},
		{"split with the flag", flagged},
		{"chat with the flag", func(t *testing.T) Model { return sendLayoutKey(t, flagged(t), tea.KeyCtrlW) }},
		{"list over the video", func(t *testing.T) Model {
			m := enterVideoLayout(t, flagged(t))
			return applyMsg(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		}},
	} {
		t.Run(s.name, func(t *testing.T) {
			// Any key drops the one-time notices, so c is held against a key no
			// binding uses: outside the video body it must do exactly as little.
			m := s.prep(t)
			press := func(r rune) (Model, tea.Cmd) {
				updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				return updated.(Model), cmd
			}
			inert, inertCmd := press('z')
			next, cmd := press('c')
			if (cmd == nil) != (inertCmd == nil) {
				t.Fatal("c returned a command where a free key returns none")
			}
			if next.videoChatOff != m.videoChatOff {
				t.Fatal("c toggled the video chat outside the video body")
			}
			if next.LayoutMode() != inert.LayoutMode() || next.Focus() != inert.Focus() || next.composing != inert.composing {
				t.Fatalf("layout %q/%q focus %q/%q", inert.LayoutMode(), next.LayoutMode(), inert.Focus(), next.Focus())
			}
			if next.View() != inert.View() {
				t.Fatalf("c changed the screen:\n%s\n---\n%s", inert.View(), next.View())
			}
		})
	}
}

func TestVideoChatKeyStaysTextWhileComposing(t *testing.T) {
	m, _, _ := playingVideoSession(t)
	m.composing = true
	m, _ = pressVideoChat(t, m)
	if m.videoChatOff || m.composeDraft != "c" {
		t.Fatalf("c while composing: chat off %v, draft %q", m.videoChatOff, m.composeDraft)
	}
}

func TestVideoChatOffKeepsComposeShut(t *testing.T) {
	m, _, _ := playingVideoSession(t)
	m, _ = pressVideoChat(t, m)
	m = applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if m.composing {
		t.Fatal("i opened an input with the chat off")
	}
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("video.chat_off_key")) {
		t.Fatalf("i said nothing: %q", bar)
	}
	m, _ = pressVideoChat(t, m)
	if m.Focus() != FocusChat || !m.chatColumnVisible() {
		t.Fatalf("chat back but focus %q", m.Focus())
	}
}
