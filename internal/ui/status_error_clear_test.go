package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestPlayerOfflineErrorClearsOnNavigation(t *testing.T) {
	offline := "Erro: Canal offline · r repetir · ? detalhes"
	m, _ := newFailureModel(t, i18n.LangPTBR, 120, "xqc")
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, offline) {
		t.Fatalf("precondition: status must show offline:\n%s", bar)
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	bar := ansi.Strip(m.StatusBar())
	if strings.Contains(bar, offline) {
		t.Fatalf("after Ctrl+R status still shows offline:\n%s", bar)
	}
	if m.PlayerState() == PlayerStateError || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("player error still active: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
	if m.Err() != nil {
		t.Fatalf("list err = %v, want nil", m.Err())
	}
}

func TestPlayerOfflineErrorClearsOnFilterAndSearch(t *testing.T) {
	offline := "Erro: Canal offline · r repetir · ? detalhes"

	t.Run("filter", func(t *testing.T) {
		m, _ := newFailureModel(t, i18n.LangPTBR, 120, "xqc")
		m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		if !m.Filtering() {
			t.Fatal("expected filtering mode")
		}
		bar := ansi.Strip(m.StatusBar())
		if strings.Contains(bar, offline) {
			t.Fatalf("filter prompt must replace offline error:\n%s", bar)
		}
	})

	t.Run("search_prompt", func(t *testing.T) {
		m, _ := newFailureModel(t, i18n.LangPTBR, 120, "xqc")
		m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
		if !m.Searching() {
			t.Fatal("expected searching prompt")
		}
		bar := ansi.Strip(m.StatusBar())
		if strings.Contains(bar, offline) {
			t.Fatalf("search prompt must replace offline error:\n%s", bar)
		}
	})
}

func TestPlayerOfflineErrorRemainsOnSameScreen(t *testing.T) {
	offline := "Erro: Canal offline · r repetir · ? detalhes"
	m, _ := newFailureModel(t, i18n.LangPTBR, 120, "xqc")
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		{ID: "1", Login: "xqc", DisplayName: "xQc"},
		{ID: "2", Login: "gaules", DisplayName: "Gaules"},
	}
	m.list.highlightedID = "1"
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.PlayerState() != PlayerStateError || m.PlayerFailure() != PlayerFailureOffline {
		t.Fatalf("Down must keep player error: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, offline) {
		t.Fatalf("same-screen move must keep offline in status:\n%s", bar)
	}
}

func TestPlayerOfflineDetailSurvivesInHelpAfterNavigation(t *testing.T) {
	m, _ := newFailureModel(t, i18n.LangPTBR, 200, "xqc")
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if strings.Contains(ansi.Strip(m.StatusBar()), "Canal offline") {
		t.Fatalf("status still shows offline after navigation:\n%s", ansi.Strip(m.StatusBar()))
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Último erro do player: "+offlineLine) {
		t.Fatalf("help must keep the technical detail after navigation:\n%s", view)
	}
}

func TestListErrorClearsOnNavigation(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	m := New(cat)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 20})
	m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "categories", Err: errors.New("timeout")}
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, m.t("error.list.network")) {
		t.Fatalf("precondition: list error missing from status:\n%s", bar)
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if m.Err() != nil {
		t.Fatalf("opening filter must clear list err, got %v", m.Err())
	}
	bar := ansi.Strip(m.StatusBar())
	if strings.Contains(bar, m.t("error.list.network")) {
		t.Fatalf("filter must replace list error in status:\n%s", bar)
	}
}

func TestClearStaleStatusErrorsKeepsTransientNotice(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatalf("i18n: %v", err)
	}
	m := New(cat)
	m.notices.status = m.t("favorites.added")
	m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "categories", Err: errors.New("x")}
	m.player.state = PlayerStateError
	m.player.failure = PlayerFailureOffline
	m.player.lastLine = offlineLine

	m.clearStaleStatusErrors()
	if m.Err() != nil {
		t.Fatalf("err = %v, want nil", m.Err())
	}
	if m.PlayerState() != PlayerStateNone || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("player error not dismissed: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
	if m.StatusNotice() != m.t("favorites.added") {
		t.Fatalf("transient notice cleared early: %q", m.StatusNotice())
	}
	if got := m.player.errorLine(); got != offlineLine {
		t.Fatalf("player last line = %q, want kept for help", got)
	}
}

func TestSuccessfulChannelSwitchClearsOfflineStatus(t *testing.T) {
	offline := "Erro: Canal offline · r repetir · ? detalhes"
	m, _ := newFailureModel(t, i18n.LangPTBR, 120, "xqc")
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	m.list.err = &twitch.Error{Kind: twitch.Rede, Op: "channels", Err: errors.New("stale")}

	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	if m.Err() != nil {
		t.Fatalf("channel switch must clear list err, got %v", m.Err())
	}
	if m.PlayerState() != PlayerStateStarting {
		t.Fatalf("state=%q, want starting", m.PlayerState())
	}
	bar := ansi.Strip(m.StatusBar())
	if strings.Contains(bar, offline) {
		t.Fatalf("status still shows offline after switch:\n%s", bar)
	}
}
