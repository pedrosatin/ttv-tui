package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func playingModel(t *testing.T, lang string, pb *fakePlayback) Model {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithPlayback(pb))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules"}})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	return updated.(Model)
}

// The mpv window changes volume with no key reaching the TUI: only a status
// message arrives, and the bar must follow it at 80 columns, even while the
// one-time window notice leads.
func TestVolumeFromMPVWindowUpdatesStatusBar(t *testing.T) {
	pb := &fakePlayback{}
	m := playingModel(t, i18n.LangEN, pb)
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)

	for _, vol := range []int{95, 40, 0} {
		updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: vol})
		m = updated.(Model)
		if m.PlayerVolume() != vol {
			t.Fatalf("PlayerVolume = %d, want %d", m.PlayerVolume(), vol)
		}
		if want := fmt.Sprintf("vol %d", vol); !strings.Contains(ansi.Strip(m.StatusBar()), want) {
			t.Fatalf("status bar missing %q: %q", want, ansi.Strip(m.StatusBar()))
		}
	}
	if !m.PlayerWindowNotice() {
		t.Fatal("precondition: the window notice must still lead the bar")
	}
	if len(pb.volDeltas) != 0 {
		t.Fatalf("TUI sent volume commands: %v", pb.volDeltas)
	}
}

// The bar is judged before the player answers: the fake always says 100.
func TestReopeningAChannelKeepsTheVolumeOnTheBar(t *testing.T) {
	m := playingModel(t, i18n.LangEN, &fakePlayback{})
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 40})
	m = updated.(Model)

	updated, _ = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules"}})
	m = updated.(Model)
	if m.PlayerVolume() != 40 {
		t.Fatalf("same channel again: volume %d", m.PlayerVolume())
	}

	updated, _ = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	m = updated.(Model)
	if m.PlayerVolume() != 100 {
		t.Fatalf("another channel: volume %d", m.PlayerVolume())
	}
}

// A pause in the mpv window never reaches the TUI as a key, so the one-time
// window notice cannot wait for one.
func TestPauseFromMPVWindowDismissesWindowNotice(t *testing.T) {
	m := playingModel(t, i18n.LangEN, &fakePlayback{})
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)
	if !m.PlayerWindowNotice() {
		t.Fatal("first playing event must raise the window notice")
	}
	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePaused, Login: "gaules", Volume: 100})
	m = updated.(Model)
	if m.PlayerWindowNotice() {
		t.Fatalf("notice still shown: %q", ansi.Strip(m.StatusBar()))
	}
	if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, "Paused gaules") {
		t.Fatalf("status bar missing the pause: %q", got)
	}
}

func TestInputConfWarningShownOncePerPlayback(t *testing.T) {
	for lang, want := range map[string]string{
		i18n.LangEN:   "mpv window has no keys: use Space and +/- in the terminal",
		i18n.LangPTBR: "Janela do mpv sem teclas: use Espaço e +/- no terminal",
	} {
		t.Run(lang, func(t *testing.T) {
			m := playingModel(t, lang, &fakePlayback{})
			msg := PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, InputConfFailed: true}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, want) {
				t.Fatalf("status bar at 80 columns missing the whole warning: %q", got)
			}

			// A key dismisses it; later events of the same playback stay quiet.
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
			m = updated.(Model)
			updated, _ = m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); strings.Contains(got, want) {
				t.Fatalf("warning came back in the same playback: %q", got)
			}

			msg.Generation = 2
			updated, _ = m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, want) {
				t.Fatalf("new playback did not warn: %q", got)
			}
		})
	}
}

func TestNoWarningWhenInputConfWorked(t *testing.T) {
	m := playingModel(t, i18n.LangEN, &fakePlayback{})
	updated, _ := m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)
	if m.StatusNotice() != "" {
		t.Fatalf("StatusNotice = %q", m.StatusNotice())
	}
}

func TestContainmentWarningShownOncePerPlayback(t *testing.T) {
	for lang, want := range map[string]string{
		i18n.LangEN:   "Windows: video may keep running if the app crashes",
		i18n.LangPTBR: "Windows: se o app cair, o vídeo pode continuar",
	} {
		t.Run(lang, func(t *testing.T) {
			m := playingModel(t, lang, &fakePlayback{})
			msg := PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, ContainmentDegraded: true}
			updated, _ := m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, want) {
				t.Fatalf("status bar at 80 columns missing the whole warning: %q", got)
			}

			// A key dismisses it; later events of the same playback stay quiet.
			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
			m = updated.(Model)
			updated, _ = m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); strings.Contains(got, want) {
				t.Fatalf("warning came back in the same playback: %q", got)
			}

			msg.Generation = 2
			updated, _ = m.Update(msg)
			m = updated.(Model)
			if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, want) {
				t.Fatalf("new playback did not warn: %q", got)
			}
		})
	}
}
