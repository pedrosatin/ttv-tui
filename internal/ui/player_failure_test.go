package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// newFailureModel opens login with a fake playback at the given width and
// returns the model after the Start handshake (generation 1).
func newFailureModel(t *testing.T, lang string, width int, login string) (Model, *fakePlayback) {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New(%s): %v", lang, err)
	}
	pb := &fakePlayback{}
	m := New(cat, WithPlayback(pb))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
	m = updated.(Model)
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: login, DisplayName: login}})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	if m.PlayerGeneration() != 1 || m.PlayerState() != PlayerStateStarting {
		t.Fatalf("after select: gen=%d state=%q", m.PlayerGeneration(), m.PlayerState())
	}
	return m, pb
}

// drainCmd runs cmd (and nested batches) feeding PlayerStatusMsg results back.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	switch v := msg.(type) {
	case tea.BatchMsg:
		for _, c := range v {
			m = drainCmd(t, m, c)
		}
	case PlayerStatusMsg:
		updated, _ := m.Update(v)
		m = updated.(Model)
	}
	return m
}

func failWith(t *testing.T, m Model, gen uint64, f PlayerFailure, line string) Model {
	t.Helper()
	updated, _ := m.Update(PlayerStatusMsg{
		Generation: gen,
		State:      PlayerStateError,
		Login:      m.PlayerLogin(),
		LastLine:   line,
		Failure:    f,
	})
	return updated.(Model)
}

const offlineLine = "error: No playable streams found on this URL: https://www.twitch.tv/xqc"

func TestPlayerFailureMessagesPerClass(t *testing.T) {
	cases := []struct {
		failure PlayerFailure
		line    string
		pt      string
		en      string
	}{
		{PlayerFailureOffline, offlineLine, "Canal offline: escolha outro canal", "Channel offline: pick another channel"},
		{PlayerFailureNetwork, "error: Unable to open URL: https://gql.twitch.tv/gql (Connection refused)", "Erro de rede: r para tentar de novo", "Network error: press r to retry"},
		{PlayerFailureTimeout, "error: Connection to gql.twitch.tv timed out", "Tempo esgotado: r para tentar de novo", "Timed out: press r to retry"},
		{PlayerFailureStreamlinkMissing, "player: streamlink not found: exec: \"streamlink\": executable file not found in $PATH", "streamlink não encontrado: instale-o", "streamlink not found: install it"},
		{PlayerFailureMPVMissing, "error: Failed to start player: mpv (Player executable not found)", "mpv não encontrado: instale o mpv", "mpv not found: install mpv"},
		{PlayerFailureUnknown, "streamlink exited with code 1", "Falha no player: r para tentar de novo", "Player failed: press r to retry"},
	}
	for _, c := range cases {
		for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
			t.Run(string(c.failure)+"/"+lang, func(t *testing.T) {
				m, _ := newFailureModel(t, lang, 200, "xqc")
				m = failWith(t, m, 1, c.failure, c.line)
				want := c.en
				detailLabel := "detail: "
				if lang == i18n.LangPTBR {
					want = c.pt
					detailLabel = "detalhe: "
				}
				bar := ansi.Strip(m.StatusBar())
				iMsg := strings.Index(bar, want)
				if iMsg < 0 {
					t.Fatalf("status bar lacks %q:\n%s", want, bar)
				}
				iDetail := strings.Index(bar, detailLabel+c.line)
				if iDetail < 0 {
					t.Fatalf("status bar lacks the technical detail %q:\n%s", c.line, bar)
				}
				if iDetail < iMsg {
					t.Fatalf("technical detail must come after the main message:\n%s", bar)
				}
				if strings.Contains(bar, "Erro no player: error:") || strings.Contains(bar, "Player error: error:") {
					t.Fatalf("raw line still leads the message:\n%s", bar)
				}
			})
		}
	}
}

func TestPlayerFailureDetailInHelp(t *testing.T) {
	m, _ := newFailureModel(t, i18n.LangPTBR, 200, "xqc")
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(Model)
	if !strings.Contains(ansi.Strip(m.View()), "Último erro do player: "+offlineLine) {
		t.Fatalf("help overlay must show the technical detail:\n%s", ansi.Strip(m.View()))
	}
}

func TestStalePlayerFailureDoesNotOverwriteCurrentChannel(t *testing.T) {
	m, _ := newFailureModel(t, i18n.LangPTBR, 200, "xqc")
	// Switch to gaules: generation 2 starts and plays.
	updated, cmd := m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "gaules", DisplayName: "Gaules"}})
	m = updated.(Model)
	m = drainCmd(t, m, cmd)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = updated.(Model)

	// A late offline failure from xqc (generation 1) arrives.
	updated, _ = m.Update(PlayerStatusMsg{Generation: 1, State: PlayerStateError, Login: "xqc", LastLine: offlineLine, Failure: PlayerFailureOffline})
	m = updated.(Model)
	if m.PlayerState() != PlayerStatePlaying || m.PlayerLogin() != "gaules" || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("stale failure leaked: state=%q login=%q failure=%q", m.PlayerState(), m.PlayerLogin(), m.PlayerFailure())
	}
	if bar := ansi.Strip(m.StatusBar()); strings.Contains(bar, "offline") {
		t.Fatalf("status bar shows the stale failure:\n%s", bar)
	}

	// Same while the switch handshake is still in flight.
	updated, _ = m.Update(ChannelSelectedMsg{Channel: twitch.Channel{Login: "alanzoka"}})
	m = updated.(Model)
	updated, _ = m.Update(PlayerStatusMsg{Generation: 2, State: PlayerStateError, Login: "gaules", LastLine: "boom", Failure: PlayerFailureNetwork})
	m = updated.(Model)
	if m.PlayerState() != PlayerStateStarting || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("pre-handshake failure leaked: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
}

func TestPlayerFailureAt80ColumnsKeepsMainAction(t *testing.T) {
	wants := map[string][]string{
		i18n.LangPTBR: {
			"Canal offline: escolha outro canal",
			"Erro de rede: r para tentar de novo",
			"Tempo esgotado: r para tentar de novo",
			"streamlink não encontrado: instale-o",
			"mpv não encontrado: instale o mpv",
			"Falha no player: r para tentar de novo",
		},
		i18n.LangEN: {
			"Channel offline: pick another channel",
			"Network error: press r to retry",
			"Timed out: press r to retry",
			"streamlink not found: install it",
			"mpv not found: install mpv",
			"Player failed: press r to retry",
		},
	}
	failures := []PlayerFailure{
		PlayerFailureOffline, PlayerFailureNetwork, PlayerFailureTimeout,
		PlayerFailureStreamlinkMissing, PlayerFailureMPVMissing, PlayerFailureUnknown,
	}
	for lang, msgs := range wants {
		for i, f := range failures {
			m, _ := newFailureModel(t, lang, 80, "xqc")
			m = failWith(t, m, 1, f, offlineLine)
			bar := ansi.Strip(m.StatusBar())
			if w := ansi.StringWidth(bar); w != 80 {
				t.Fatalf("%s/%s: bar width %d, want 80", lang, f, w)
			}
			if !strings.Contains(bar, msgs[i]) {
				t.Fatalf("%s/%s: main action truncated at 80 columns:\n%q", lang, f, bar)
			}
			if !strings.Contains(bar, "Ctrl+C/Ctrl+Q") {
				t.Fatalf("%s/%s: quit hint lost:\n%q", lang, f, bar)
			}
		}
	}
}

func TestPlayerFailureAt80ColumnsWithChat(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	m = updated.(Model)
	m = failWith(t, m, 1, PlayerFailureOffline, offlineLine)
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "Channel offline: pick another channel") {
		t.Fatalf("main action truncated with chat connected:\n%q", bar)
	}
}

func TestRetryKeyRestartsFailedPlayback(t *testing.T) {
	m, pb := newFailureModel(t, i18n.LangPTBR, 120, "gaules")
	m = failWith(t, m, 1, PlayerFailureNetwork, "error: Unable to open URL")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if m.PlayerState() != PlayerStateStarting || m.PlayerFailure() != PlayerFailureNone {
		t.Fatalf("after r: state=%q failure=%q", m.PlayerState(), m.PlayerFailure())
	}
	m = drainCmd(t, m, cmd)
	if len(pb.started) != 2 || pb.started[1] != "gaules" {
		t.Fatalf("r must restart playback of gaules; started=%v", pb.started)
	}
	if m.PlayerGeneration() != 2 {
		t.Fatalf("generation after retry = %d, want 2", m.PlayerGeneration())
	}
}
