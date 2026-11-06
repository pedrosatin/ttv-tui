package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

type feedbackStore struct {
	err   error
	calls int
}

func (s *feedbackStore) Save(store.State) error { s.calls++; return s.err }

func feedbackModel(t *testing.T, s Store) Model {
	m := New(mustCatalog(t, i18n.LangPTBR), WithStore(s))
	m.width, m.height = 80, 24
	m.mode = ModeChannels
	m.channels = []twitch.Channel{{ID: "1", Login: "demo", DisplayName: "Demo"}}
	m.reconcileHighlight()
	return m
}

func TestFeedbackFavoriteSave(t *testing.T) {
	for _, removed := range []bool{false, true} {
		for _, fails := range []bool{false, true} {
			t.Run(fmt.Sprintf("removed=%v/fails=%v", removed, fails), func(t *testing.T) {
				s := &feedbackStore{}
				if fails {
					s.err = errors.New("permission denied")
				}
				m := feedbackModel(t, s)
				if removed {
					m.favorites = append([]twitch.Channel(nil), m.channels...)
				}
				m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
				if m.StatusNotice() != "" {
					t.Errorf("confirmed before Save: %q", m.StatusNotice())
				}
				if s.calls != 0 {
					t.Fatal("Save ran synchronously")
				}
				if cmd == nil {
					t.Fatal("no save command")
				}
				m, _ = pressKeyMsg(m, cmd())
				bar := ansi.Strip(m.StatusBar())
				if fails {
					if !strings.Contains(bar, "! ") || !strings.Contains(bar, "permissões/espaço") {
						t.Errorf("missing save warning/action: %q", bar)
					}
					if strings.Contains(bar, m.t("favorites.added")) || strings.Contains(bar, m.t("favorites.removed")) {
						t.Errorf("false success: %q", bar)
					}
					m.playerState = PlayerStateError
					m.playerFailure = PlayerFailureNetwork
					if !strings.Contains(ansi.Strip(m.StatusBar()), "✖ "+m.playerFailureText()) {
						t.Fatal("warning outranks player error")
					}
				} else {
					key := "favorites.added"
					if removed {
						key = "favorites.removed"
					}
					if !strings.Contains(bar, m.t(key)) {
						t.Errorf("missing confirmation after save: %q", bar)
					}
				}
			})
		}
	}
}

func TestFeedbackRecentSaveFailure(t *testing.T) {
	m := feedbackModel(t, &feedbackStore{err: errors.New("disk full")})
	m.queueRecent(m.channels[0])
	m, _ = pressKeyMsg(m, m.saveStoreCmd()())
	if !strings.Contains(ansi.Strip(m.StatusBar()), "! ") {
		t.Fatal("recent save failure swallowed")
	}
}

func TestFeedbackPlayerInfo(t *testing.T) {
	for _, line := range []string{"[cli][info] Stream ended", "[plugins.twitch][info] Will skip ad segments"} {
		m := New(mustCatalog(t, i18n.LangPTBR))
		m, _ = pressKeyMsg(m, PlayerStatusMsg{State: PlayerStatePlaying, LastLine: line})
		if m.playerErrorLine() != "" {
			t.Errorf("info exposed as last error: %q", m.playerErrorLine())
		}
		if !strings.Contains(ansi.Strip(m.StatusBar()), fmt.Sprintf(m.t("player.info"), line)) {
			t.Errorf("info not displayed separately: %q", ansi.Strip(m.StatusBar()))
		}
		m, _ = pressKeyMsg(m, PlayerStatusMsg{State: PlayerStateEnded, LastLine: line})
		if m.PlayerState() == PlayerStateError || m.statusParts().tone == toneError {
			t.Fatal("normal end is failure")
		}
		m, _ = pressKeyMsg(m, PlayerStatusMsg{State: PlayerStateError, Failure: PlayerFailureUnknown, LastLine: line})
		if m.PlayerState() != PlayerStateError || m.statusParts().tone != toneError {
			t.Fatal("unexpected exit hidden by info")
		}
		if m.playerErrorLine() != "" {
			t.Fatal("info labelled as error detail")
		}
	}
}

func TestFeedbackLoadingAndEmpty(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, mode := range []Mode{ModeCategories, ModeChannels, ModeSearch} {
			m := New(mustCatalog(t, lang))
			m.width, m.height, m.mode = 80, 24, mode
			m.loading = true
			loading := ansi.Strip(m.View())
			m.loading = false
			empty := ansi.Strip(m.View())
			if loading == empty {
				t.Fatal("loading and empty identical")
			}
			if !strings.Contains(empty, "Esc") || !strings.Contains(empty, "r ") {
				t.Errorf("empty has no next step: %q", empty)
			}
			if m.statusParts().primary == m.t("status.ready") {
				t.Error("empty claims ready")
			}
		}
	}
}

func TestFeedbackDisconnectedChat(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, state := range []ChatState{ChatStateConnecting, ChatStateReconnecting, ChatStateDisconnected, ChatStateError} {
			m := New(mustCatalog(t, lang))
			m.chatPanel.channel, m.chatPanel.state, m.chatPanel.errText = "demo", state, "connection refused"
			view := ansi.Strip(strings.Join(m.renderChatLines(12, 80), "\n"))
			waiting := fmt.Sprintf(m.t("chat.empty"), "demo")
			if strings.Contains(view, waiting) {
				t.Errorf("%s/%s contradicts connection state: %q", lang, state, view)
			}
			if (state == ChatStateReconnecting || state == ChatStateDisconnected) && !strings.Contains(view, "connection refused") {
				t.Errorf("known reason lost: %q", view)
			}
		}
	}
}

// Use a real read-only state directory; no operator config is loaded.
func TestFeedbackReadOnlyState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	s := store.New(filepath.Join(dir, "state.json"))
	ch := store.Channel{ID: "1", Login: "demo", DisplayName: "Demo"}
	if err := s.Save(store.State{Favorites: []store.Channel{ch}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	m := feedbackModel(t, s)
	m.favorites = []twitch.Channel{ch.ToTwitch()}
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if m.StatusNotice() != "" {
		t.Fatal("success before save")
	}
	m, _ = pressKeyMsg(m, cmd())
	after, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("state changed despite read-only directory")
	}
	if len(m.favorites) != 0 {
		t.Fatal("in-memory change lost")
	}
	if !strings.Contains(ansi.Strip(m.StatusBar()), "! "+m.t("store.save_failed")) {
		t.Fatal(m.StatusBar())
	}
}

func TestFeedbackTranslations(t *testing.T) {
	for _, lang := range orientationLangs {
		for _, key := range []string{"store.save_failed", "list.empty_action", "player.info"} {
			if got := mustCatalog(t, lang).T(key); got == "" || got == key {
				t.Fatalf("%s: missing %s", lang, key)
			}
		}
	}
}

type feedbackPTYModel struct{ Model }

func (m feedbackPTYModel) Init() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return tea.QuitMsg{} })
}

func TestFeedbackPTY(t *testing.T) {
	state := os.Getenv("TTV_FEEDBACK_PTY")
	if state == "" {
		t.Skip("opt-in PTY capture")
	}
	m := New(mustCatalog(t, i18n.LangPTBR))
	m.width, m.height = 80, 24
	switch state {
	case "loading":
		m.loading = true
	case "empty":
	case "save-failure":
		m = feedbackModel(t, &feedbackStore{err: errors.New("permission denied")})
		var cmd tea.Cmd
		m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
		m, _ = pressKeyMsg(m, cmd())
	case "disconnected":
		m.chatPanel.channel, m.chatPanel.state, m.chatPanel.errText = "demo", ChatStateReconnecting, "connection refused"
		m.showList, m.showChat = false, true
	default:
		t.Fatal("unknown PTY state")
	}
	if _, err := tea.NewProgram(feedbackPTYModel{m}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
}
