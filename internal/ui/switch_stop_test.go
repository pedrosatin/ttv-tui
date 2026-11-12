package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// Ctrl+X while the channel is still coming up: the Stop reply comes before the
// Start reply and is dropped as part of the switch. The Start reply must not
// bring the player back.
func TestStopDuringSwitchEndsStopped(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	replies := map[string]PlayerStatusMsg{
		"player still starting": {Generation: 1, State: PlayerStateStarting, Login: "gaules", Volume: 100, Selection: 1},
		"start cancelled": {Generation: 1, State: PlayerStateError, Login: "gaules", LastLine: "player: stopped before attach",
			Failure: PlayerFailureUnknown, Selection: 1},
	}
	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			pb := &fakePlayback{}
			m := New(catEN, WithPlayback(pb))
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
			m = updated.(Model)
			updated, _ = m.Update(ChannelSelectedMsg{Channel: catalog.Channel{Login: "gaules"}})
			m = updated.(Model)

			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
			m = updated.(Model)
			if cmd == nil {
				t.Fatal("Ctrl+X during the switch sent no Stop")
			}
			updated, _ = m.Update(cmd())
			m = updated.(Model)

			updated, cmd = m.Update(reply)
			m = updated.(Model)
			for cmd != nil {
				updated, cmd = m.Update(cmd())
				m = updated.(Model)
			}
			if m.PlayerState() != PlayerStateEnded {
				t.Fatalf("PlayerState = %q after Ctrl+X during the switch, want encerrado", m.PlayerState())
			}
			if pb.stopped < 2 {
				t.Fatalf("stopped %d times; the playback the Start reply reports was never stopped", pb.stopped)
			}
			if view := m.View(); !strings.Contains(view, catEN.T("player.ended")) {
				t.Fatalf("status bar does not say the player stopped:\n%s", view)
			}
		})
	}
}

// A selection whose message is processed after Ctrl+Q must not start a player
// that nothing will stop.
func TestChannelSelectedAfterQuitStartsNothing(t *testing.T) {
	catEN, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(catEN, WithPlayback(pb))
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m = updated.(Model)
	if !m.Quitting() {
		t.Fatal("Ctrl+Q did not quit")
	}
	_, _ = m.Update(ChannelSelectedMsg{Channel: catalog.Channel{Login: "gaules"}})
	if len(pb.started) != 0 {
		t.Fatalf("Start called after quit for %v", pb.started)
	}
}
