package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func hiddenListModel(t *testing.T, chatVisible bool) Model {
	t.Helper()
	m, _, _ := setupTwoPanelModel(t)
	m.width, m.height = 80, 24
	m.moveHighlight(1)
	m.layout.showList, m.layout.showChat = false, chatVisible
	m.normalizeFocusAfterLayout()
	return m
}

func TestHiddenListEnterDoesNotChangeChannel(t *testing.T) {
	m := hiddenListModel(t, false)
	before := m.list.selectedChannel.Login
	next, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	// Execute a selection message too: selecting a row is asynchronous.
	if cmd != nil {
		next, _ = pressKeyMsg(next, cmd())
	}
	if next.list.selectedChannel.Login != before {
		t.Fatalf("Enter changed hidden channel: %s -> %s", before, next.list.selectedChannel.Login)
	}
	if next.notices.status != next.t("layout.hidden_list_key") || !next.notices.statusWarns || cmd != nil {
		t.Fatalf("Enter must warn without a command: notice=%q cmd=%v", next.notices.status, cmd != nil)
	}
}

func TestHiddenListKeys(t *testing.T) {
	keys := []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyDown}, {Type: tea.KeyEnter}, {Type: tea.KeyEsc}, {Type: tea.KeyCtrlF}, {Type: tea.KeyCtrlS}}
	for _, r := range "jkf/ryl" {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	for _, chatOn := range []bool{false, true} {
		for _, k := range keys {
			t.Run(fmt.Sprint(chatOn)+"/"+k.String(), func(t *testing.T) {
				m := hiddenListModel(t, chatOn)
				if k.String() == "j" || k.String() == "down" {
					m.moveHighlight(-1)
				}
				id, gen, login, mode := m.list.highlightedID, m.list.generation, m.list.selectedChannel.Login, m.Mode()
				next, _ := pressKey(m, k)
				if next.list.highlightedID != id || next.list.generation != gen || next.list.selectedChannel.Login != login || next.Mode() != mode || next.list.filtering || next.list.search.active || next.list.prefs.menuOpen || len(next.list.favorites) != len(m.list.favorites) {
					t.Fatalf("hidden list changed after %s", k.String())
				}
			})
		}
	}
}

func TestHiddenListSuspendsTextInputs(t *testing.T) {
	for _, chatOn := range []bool{false, true} {
		for _, searching := range []bool{false, true} {
			m := hiddenListModel(t, chatOn)
			m.list.filtering, m.list.search.active = !searching, searching
			m.list.filter, m.list.search.input = "ga", "ga"
			for _, k := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyEsc}, {Type: tea.KeyBackspace}, {Type: tea.KeyRunes, Runes: []rune{'j'}}} {
				next, _ := pressKey(m, k)
				if next.list.filter != m.list.filter || next.list.search.input != m.list.search.input || next.list.filtering != m.list.filtering || next.list.search.active != m.list.search.active {
					t.Fatal("hidden input changed")
				}
			}
		}
	}
}

func TestHiddenListGlobalKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyCtrlQ}, {Type: tea.KeyCtrlW}, {Type: tea.KeyCtrlH}, {Type: tea.KeyCtrlL}, {Type: tea.KeyCtrlX}, {Type: tea.KeyCtrlR}, {Type: tea.KeyRunes, Runes: []rune{'?'}}, {Type: tea.KeyRunes, Runes: []rune{'+'}}} {
		t.Run(k.String(), func(t *testing.T) {
			m := hiddenListModel(t, false)
			pb := m.playback.(*fakePlayback)
			stopped, volumes := pb.stopped, len(pb.volDeltas)
			next, cmd := pressKey(m, k)
			switch k.String() {
			case "ctrl+q":
				if !next.quitting || cmd == nil {
					t.Fatal("quit blocked")
				}
			case "ctrl+w":
				if next.LayoutMode() != LayoutSplit {
					t.Fatal("cycle blocked")
				}
			case "ctrl+h":
				if next.LayoutMode() != LayoutList {
					t.Fatal("list blocked")
				}
			case "ctrl+l":
				if next.LayoutMode() != LayoutChat {
					t.Fatal("chat blocked")
				}
			case "ctrl+x":
				if pb.stopped != stopped+1 {
					t.Fatal("stop blocked")
				}
			case "ctrl+r":
				if next.Mode() != ModeCategories {
					t.Fatal("categories blocked")
				}
			case "?":
				if !next.layout.helpVisible {
					t.Fatal("help blocked")
				}
			case "+":
				if len(pb.volDeltas) != volumes+1 {
					t.Fatal("volume blocked")
				}
			}
		})
	}
}

func TestHiddenListSplitStillSelectsAndMoves(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m.width, m.height = 120, 24
	id := m.list.highlightedID
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.list.highlightedID == id {
		t.Fatal("j did not move")
	}
	m, cmd := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did not select")
	}
	m, _ = pressKeyMsg(m, cmd())
	if m.list.selectedChannel.Login != "alanzoka" {
		t.Fatal("wrong channel selected")
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if m.list.highlightedID != id {
		t.Fatal("k did not move back")
	}
}

func TestHiddenListNoticeParityAndNoSpam(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		m := hiddenListModel(t, false)
		m.catalog = testCatalog(t, lang)
		notice := m.t("layout.hidden_list_key")
		if notice == "layout.hidden_list_key" || !strings.Contains(notice, "Ctrl+H") {
			t.Fatal("missing translation or recovery key")
		}
		for range 30 {
			var cmd tea.Cmd
			m, cmd = pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil {
				t.Fatal("repeated key scheduled a command")
			}
			if strings.Count(ansi.Strip(m.StatusBar()), notice) != 1 {
				t.Fatalf("notice missing or duplicated: %s", ansi.Strip(m.StatusBar()))
			}
		}
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlH})
		if m.notices.status != "" {
			t.Fatal("notice remained after restoring list")
		}
	}
}

// Run explicitly inside a PTY with TTV_FOCUS_PTY=1; Enter comes from the
// terminal, while the timeout only ends the capture without clearing the notice.
type hiddenListPTYModel struct{ Model }

func (m hiddenListPTYModel) Init() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return tea.QuitMsg{} })
}

func (m hiddenListPTYModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	m.Model = next.(Model)
	return m, cmd
}

func TestHiddenListPTY(t *testing.T) {
	if os.Getenv("TTV_FOCUS_PTY") != "1" {
		t.Skip("explicit PTY capture only")
	}
	m := hiddenListModel(t, false)
	m.catalog = testCatalog(t, i18n.LangPTBR)
	m.browser, m.searcher, m.resolver, m.playback, m.chat.session = nil, nil, nil, nil, nil
	program := tea.NewProgram(hiddenListPTYModel{m}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithoutSignalHandler())
	final, err := program.Run()
	if err != nil {
		t.Fatal(err)
	}
	result := final.(hiddenListPTYModel)
	if result.list.selectedChannel.Login != m.list.selectedChannel.Login || result.notices.status != m.t("layout.hidden_list_key") {
		t.Fatal("Enter did not preserve the channel and warn")
	}
	if _, err := fmt.Fprintln(os.Stdout, "PTY: Enter recusado; canal continua #"+result.list.selectedChannel.Login+"; "+result.notices.status); err != nil {
		t.Fatal(err)
	}
}
