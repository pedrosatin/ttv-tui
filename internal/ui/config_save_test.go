package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var errConfigUnreadable = errors.New("read config: permission denied")

// runConfigSave runs the save command and feeds its message back, as the
// program loop would.
func runConfigSave(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("no save command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func TestFailedFilterSaveWarnsInTheOpenMenu(t *testing.T) {
	m := channelFilterFixture(t)
	m.width, m.height = 100, 30
	m.list.prefs.persist = func([]string, int) error { return errConfigUnreadable }
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runConfigSave(t, m, cmd)
	if !m.ChannelFilterMenuOpen() {
		t.Fatal("the menu closed")
	}
	if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, "! "+m.t("config.save_failed")) {
		t.Fatalf("status bar does not warn about the failed save: %q", got)
	}
}

func TestFailedColumnsSaveWarns(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.catalog = testCatalog(t, i18n.LangEN)
	m.hosted.persist = func(int, int, bool, bool, int) error { return errConfigUnreadable }
	m.resizeHostedList(hostedResizeStep)
	m = runConfigSave(t, m, m.hosted.saveColumns(m.hosted.saveGen))
	if got := ansi.Strip(m.StatusBar()); !strings.Contains(got, "! "+m.t("config.save_failed")) {
		t.Fatalf("status bar does not warn about the failed save: %q", got)
	}
}

func TestSuccessfulConfigSaveKeepsTheStatus(t *testing.T) {
	m := hostedModel(t, 212, 49, true)
	m.catalog = testCatalog(t, i18n.LangEN)
	m.hosted.persist = func(int, int, bool, bool, int) error { return nil }
	m.resizeHostedList(hostedResizeStep)
	notice := m.StatusNotice()
	if cmd := m.hosted.saveColumns(m.hosted.saveGen); cmd != nil {
		if next, _ := m.Update(cmd()); next.(Model).StatusNotice() != notice {
			t.Fatalf("a successful save replaced %q with %q", notice, next.(Model).StatusNotice())
		}
	}
}
