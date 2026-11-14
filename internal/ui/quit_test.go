package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func pressKey(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func typeRunes(m Model, s string) Model {
	for _, r := range s {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// cmdQuits runs cmd (flattening batches) and reports whether any part yields tea.QuitMsg.
func cmdQuits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if cmdQuits(c) {
				return true
			}
		}
	}
	return false
}

func everyModeEnter(t *testing.T) []struct {
	name  string
	enter func(t *testing.T, m Model) Model
} {
	t.Helper()
	return []struct {
		name  string
		enter func(t *testing.T, m Model) Model
	}{
		{"list", func(t *testing.T, m Model) Model {
			if m.Focus() != FocusList {
				t.Fatalf("focus = %v, want list", m.Focus())
			}
			return m
		}},
		{"filter", func(t *testing.T, m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			m = typeRunes(m, "ga")
			if !m.Filtering() {
				t.Fatal("not filtering")
			}
			return m
		}},
		{"search", func(t *testing.T, m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlF})
			m = typeRunes(m, "gau")
			if !m.Searching() {
				t.Fatal("not searching")
			}
			return m
		}},
		{"chat", func(t *testing.T, m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyTab})
			if m.Focus() != FocusChat || m.Composing() {
				t.Fatalf("focus = %v composing = %v, want chat focus without composing", m.Focus(), m.Composing())
			}
			return m
		}},
		{"compose", func(t *testing.T, m Model) Model {
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
			m = typeRunes(m, "half a mess")
			if !m.Composing() || m.ComposeDraft() != "half a mess" {
				t.Fatalf("composing = %v draft = %q", m.Composing(), m.ComposeDraft())
			}
			return m
		}},
	}
}

// Ctrl+Q takes the shutdown path from every mode: player and chat stopped,
// program told to quit.
func TestCtrlQQuitsFromEveryMode(t *testing.T) {
	for _, mode := range everyModeEnter(t) {
		t.Run(mode.name, func(t *testing.T) {
			m, pb, fc := setupTwoPanelModel(t)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
			m = mode.enter(t, m)
			pbStops, chatStops := pb.stopped, fc.stopped

			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlQ})
			if !m.Quitting() {
				t.Fatal("Quitting() = false")
			}
			if pb.stopped != pbStops+1 {
				t.Errorf("player stops = %d, want %d", pb.stopped, pbStops+1)
			}
			if fc.stopped != chatStops+1 {
				t.Errorf("chat stops = %d, want %d", fc.stopped, chatStops+1)
			}
			if !cmdQuits(cmd) {
				t.Error("returned command does not quit the program")
			}
			if len(fc.sent) != 0 {
				t.Errorf("quitting sent chat messages: %v", fc.sent)
			}
			if v := m.View(); v != "" {
				t.Errorf("View() after quit = %q, want empty", v)
			}
		})
	}
}

// Ctrl+C never quits. In every mode it shows a localized hint to use Ctrl+Q
// and leaves player, chat, filter, search, and compose state alone.
func TestCtrlCDoesNotQuitAndShowsHint(t *testing.T) {
	for _, mode := range everyModeEnter(t) {
		t.Run(mode.name, func(t *testing.T) {
			m, pb, fc := setupTwoPanelModel(t)
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
			m = mode.enter(t, m)
			pbStops, chatStops := pb.stopped, fc.stopped
			wasFiltering, wasSearching := m.Filtering(), m.Searching()
			wasComposing, draft := m.Composing(), m.ComposeDraft()
			focus := m.Focus()

			m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlC})
			if m.Quitting() || cmdQuits(cmd) {
				t.Fatal("Ctrl+C quit the app")
			}
			if pb.stopped != pbStops {
				t.Errorf("player stops = %d, want %d", pb.stopped, pbStops)
			}
			if fc.stopped != chatStops {
				t.Errorf("chat stops = %d, want %d", fc.stopped, chatStops)
			}
			wantHint := m.t("status.ctrl_c_hint")
			if m.CtrlCNotice() != wantHint {
				t.Fatalf("CtrlCNotice() = %q, want %q", m.CtrlCNotice(), wantHint)
			}
			bar := ansi.Strip(m.StatusBar())
			if !strings.Contains(bar, wantHint) {
				t.Fatalf("status bar missing Ctrl+C hint %q:\n%q", wantHint, bar)
			}
			if m.Filtering() != wasFiltering || m.Searching() != wasSearching {
				t.Fatalf("mode changed: filtering=%v searching=%v", m.Filtering(), m.Searching())
			}
			if m.Composing() != wasComposing || m.ComposeDraft() != draft {
				t.Fatalf("compose changed: composing=%v draft=%q", m.Composing(), m.ComposeDraft())
			}
			if m.Focus() != focus {
				t.Fatalf("focus = %v, want %v", m.Focus(), focus)
			}
			if len(fc.sent) != 0 {
				t.Errorf("Ctrl+C sent chat messages: %v", fc.sent)
			}
		})
	}
}

// 'q' is not a quit key: inside the compose input it is text, and outside it
// does nothing.
func TestQIsTextWhileComposingAndNotAQuitKey(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)

	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.Quitting() || cmdQuits(cmd) {
		t.Fatal("'q' outside the compose input quit the app")
	}

	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = typeRunes(m, "qq q")
	if m.Quitting() {
		t.Fatal("'q' while composing quit the app")
	}
	if got := m.ComposeDraft(); got != "qq q" {
		t.Fatalf("draft = %q, want %q", got, "qq q")
	}
	_, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fc.sent) != 1 || fc.sent[0].Body != "qq q" {
		t.Fatalf("sent = %+v, want one message %q", fc.sent, "qq q")
	}
}

func TestHelpAndStatusBarCiteOnlyCtrlQAt80Cols(t *testing.T) {
	for _, tc := range []struct {
		lang    string
		barQuit string
	}{
		{i18n.LangEN, "Ctrl+Q quit"},
		{i18n.LangPTBR, "Ctrl+Q sair"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			cat, err := i18n.New(tc.lang)
			if err != nil {
				t.Fatalf("i18n.New(%s): %v", tc.lang, err)
			}
			m.catalog = cat
			m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = updated.(Model)
			// Shift+Tab takes the help from the list keys to the global ones.
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeyF1}, tea.KeyMsg{Type: tea.KeyShiftTab})
			if !m.HelpVisible() {
				t.Fatal("help not visible")
			}
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			for _, l := range lines {
				if ansi.StringWidth(l) > 80 {
					t.Errorf("line wider than 80 cols: %q", l)
				}
			}
			// Ctrl+Q is the key that quits; Ctrl+C has a row that says it does not.
			if helpRowWith(lines, "Ctrl+Q", m.t("action.quit")) == "" {
				t.Errorf("help has no whole Ctrl+Q row:\n%s", strings.Join(lines, "\n"))
			}
			if helpRowWith(lines, "Ctrl+C", m.t("action.ctrl_c")) == "" {
				t.Errorf("help has no Ctrl+C row saying it does not quit:\n%s", strings.Join(lines, "\n"))
			}
			status := lines[len(lines)-1]
			if !strings.Contains(status, tc.barQuit) {
				t.Errorf("status bar %q does not cite %q", status, tc.barQuit)
			}
			if strings.Contains(status, "Ctrl+C") {
				t.Errorf("status bar still cites Ctrl+C: %q", status)
			}
		})
	}
}
