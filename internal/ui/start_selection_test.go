package ui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// Enter on X and right away on Y: Y's Start supersedes X's, whose reply comes
// back first. The UI waits for Y's reply, never shows X's failure, and r does
// not bring X back.
func TestStartReplyOfEarlierSelectionIsIgnored(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{startMsg: PlayerStatusMsg{Generation: 1, State: PlayerStateError,
		LastLine: "player: superseded before attach", Failure: PlayerFailureUnknown}}
	m := New(cat, WithPlayback(pb))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	updated, startX := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "xqc"}})
	m = updated.(Model)
	pb.startMsg = PlayerStatusMsg{}
	updated, startY := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules"}})
	m = updated.(Model)

	m = drainCmd(t, m, startX)
	if m.PlayerState() != PlayerStateStarting || m.PlayerLogin() != "gaules" {
		t.Fatalf("after the reply for xqc: state=%q login=%q, want iniciando on gaules", m.PlayerState(), m.PlayerLogin())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = drainCmd(t, updated.(Model), cmd)
	if !slices.Equal(pb.started, []string{"xqc", "gaules"}) {
		t.Fatalf("started %v, want [xqc gaules]", pb.started)
	}

	m = drainCmd(t, m, startY)
	if m.PlayerState() != PlayerStateStarting || m.PlayerLogin() != "gaules" || m.PlayerGeneration() != 2 {
		t.Fatalf("after the reply for gaules: state=%q login=%q gen=%d", m.PlayerState(), m.PlayerLogin(), m.PlayerGeneration())
	}
}

// Enter on alpha and right away on beta: each selection reaches Update from its
// own goroutine, so beta's may come first. The last row chosen is what plays.
func TestLateSelectionMessageDoesNotOverrideNewerEnter(t *testing.T) {
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	pb := &fakePlayback{}
	m := New(cat, WithPlayback(pb))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = updated.(Model)
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		liveChannel("a-id", "alpha", "Alpha", 20000),
		liveChannel("b-id", "beta", "Beta", 5000),
	}
	m.setHighlightToItem(m.VisibleItems(), 0)
	updated, selectAlpha := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m.setHighlightToItem(m.VisibleItems(), 1)
	updated, selectBeta := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	updated, _ = m.Update(selectBeta())
	m = updated.(Model)
	updated, _ = m.Update(selectAlpha())
	m = updated.(Model)

	if !slices.Equal(pb.started, []string{"beta"}) {
		t.Fatalf("started %v, want [beta]", pb.started)
	}
	if m.PlayerLogin() != "beta" || m.list.selectedChannel == nil || m.list.selectedChannel.Login != "beta" {
		t.Fatalf("player on %q, selected %+v; want beta", m.PlayerLogin(), m.list.selectedChannel)
	}
}
