package ui

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const (
	narrowNoticeLead = "Chat hidden (<80 cols)"
	noChatNotice     = "No chat yet"
	focusHintLead    = "Focus mode"
)

var (
	keyCtrlW = tea.KeyMsg{Type: tea.KeyCtrlW}
	keyCtrlH = tea.KeyMsg{Type: tea.KeyCtrlH}
	keyCtrlL = tea.KeyMsg{Type: tea.KeyCtrlL}
	keyY     = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
)

// setupHomeModel is the first screen of a real session: categories loaded, no
// channel opened, so no chat exists yet.
func setupHomeModel(t *testing.T, width int, fc *fakeChat) Model {
	t.Helper()
	cat, err := i18n.New(i18n.LangEN)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	src := &fakeSource{
		categoriesFn: func(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: []catalog.Category{
				{ID: "cat-cs", Name: "Counter-Strike", Viewers: catalog.IntPtr(250000)},
			}}, nil
		},
	}
	m := New(cat, WithSource(src), WithPlayback(&fakePlayback{}), WithChat(fc))
	updated, _ := m.Update(m.Init()())
	return resize(updated.(Model), width, 24)
}

func plainView(m Model) string {
	return ansi.Strip(m.View())
}

// bodyOf drops the status bar: the layouts are told apart by what they draw.
func bodyOf(m Model) string {
	rows := strings.Split(plainView(m), "\n")
	return strings.Join(rows[:len(rows)-1], "\n")
}

func TestChatOnlyIsUsableAt60Columns(t *testing.T) {
	m, _, fc := setupTwoPanelModel(t)
	m = resize(m, 60, 24)
	m = sendKeys(m, keyCtrlW)

	if got := m.LayoutMode(); got != LayoutChat {
		t.Fatalf("LayoutMode=%q, want chat", got)
	}
	if listW, chatW := m.panelWidths(); listW != 0 || chatW != 60 {
		t.Fatalf("panelWidths=(%d,%d), want (0,60)", listW, chatW)
	}
	plain := plainView(m)
	for _, want := range []string{"Chat — #gaules", "TriboOne: VAMO TRIBO!", "TriboTwo: Que jogada absurda"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("chat-only at 60 cols missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{focusHintLead, narrowNoticeLead} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("chat-only at 60 cols shows %q:\n%s", unwanted, plain)
		}
	}
	for i, row := range strings.Split(plain, "\n") {
		if w := ansi.StringWidth(row); w > 60 {
			t.Fatalf("row %d is %d cols: %q", i, w, row)
		}
	}

	if got := m.Focus(); got != FocusChat {
		t.Fatalf("Focus()=%q, want chat", got)
	}
	m = sendKeys(m, keyI)
	m = sendKeys(m, runes("oi tribo")...)
	if !m.chat.composing || m.ComposeDraft() != "oi tribo" {
		t.Fatalf("compose did not take keys: composing=%v draft=%q", m.chat.composing, m.ComposeDraft())
	}
	if !strings.Contains(plainView(m), "oi tribo") {
		t.Fatalf("draft not on screen:\n%s", plainView(m))
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(fc.sent) != 1 || fc.sent[0].Body != "oi tribo" {
		t.Fatalf("Enter did not send the draft: %+v", fc.sent)
	}
}

// The whole chat-only range under the old limit: 79 is the pane a 160-column
// split leaves on the right, by one column.
func TestChatOnlyFitsEveryNarrowWidth(t *testing.T) {
	for _, width := range []int{79, 60, 40, 20, 10, 3, 1} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 24)
			m = sendKeys(m, keyCtrlW)
			if _, chatW := m.panelWidths(); chatW != width {
				t.Fatalf("chatW=%d, want %d", chatW, width)
			}
			if width >= 20 && !strings.Contains(plainView(m), "#gaules") {
				t.Fatalf("chat header gone:\n%s", plainView(m))
			}
			m = sendKeys(m, keyI)
			m = sendKeys(m, runes("abc")...)
			for i, row := range strings.Split(plainView(m), "\n") {
				if w := ansi.StringWidth(row); w > width {
					t.Fatalf("row %d is %d cols: %q", i, w, row)
				}
			}
		})
	}
}

func TestNarrowSplitSaysWhyChatIsMissingAndWhatToDo(t *testing.T) {
	for _, width := range []int{79, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, width, 24)
			if got := m.LayoutMode(); got != LayoutSplit {
				t.Fatalf("LayoutMode=%q, want split", got)
			}
			plain := plainView(m)
			if strings.Contains(plain, "Chat — #gaules") {
				t.Fatalf("chat drawn in a split that cannot fit it:\n%s", plain)
			}
			notice := squash(plain)
			for _, want := range []string{squash(narrowNoticeLead), "Ctrl+L"} {
				if !strings.Contains(notice, want) {
					t.Fatalf("notice missing %q:\n%s", want, plain)
				}
			}
			if strings.Contains(plain, "widen terminal to view chat") {
				t.Fatalf("notice still offers widening as the only way out:\n%s", plain)
			}
		})
	}
}

// Tab, i and y target the chat: in a split that cannot show it they used to do
// nothing and say nothing.
func TestChatKeysAnswerInNarrowSplit(t *testing.T) {
	for name, k := range map[string]tea.KeyMsg{"tab": keyTab, "i": keyI, "y": keyY} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, 60, 24)
			m = sendKeys(m, k)
			want := "Ctrl+L"
			if name == "y" {
				want = "focus the chat"
			}
			if status := ansi.Strip(m.StatusBar()); !strings.Contains(status, want) {
				t.Fatalf("no answer in the status bar: %q", status)
			}
			if m.chat.composing || m.Focus() != FocusList {
				t.Fatalf("key reached the hidden chat: composing=%v focus=%q", m.chat.composing, m.Focus())
			}
		})
	}
}

func TestLayoutKeysFromNarrowSplitReachTheChat(t *testing.T) {
	for name, k := range map[string]tea.KeyMsg{"ctrl+l": keyCtrlL, "ctrl+w": keyCtrlW} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, 60, 24)
			m = sendKeys(m, k)
			if got := m.LayoutMode(); got != LayoutChat {
				t.Fatalf("LayoutMode=%q, want chat", got)
			}
			if plain := plainView(m); !strings.Contains(plain, "TriboOne: VAMO TRIBO!") {
				t.Fatalf("chat not on screen:\n%s", plain)
			}
		})
	}
}

// List-only under 80 columns: the split is not an option, so showing the chat
// has to mean the mode that can draw it.
func TestCtrlLFromNarrowListReachesTheChat(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	m = sendKeys(m, keyCtrlL)
	if got := m.LayoutMode(); got != LayoutList {
		t.Fatalf("LayoutMode=%q, want list", got)
	}
	m = resize(m, 60, 24)
	m = sendKeys(m, keyCtrlL)
	if got := m.LayoutMode(); got != LayoutChat {
		t.Fatalf("LayoutMode=%q, want chat", got)
	}
	if plain := plainView(m); !strings.Contains(plain, "Chat — #gaules") {
		t.Fatalf("chat not on screen:\n%s", plain)
	}
}

func TestCtrlLStaysAToggleOnceChatIsOnScreen(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 60, 24)
	m = sendKeys(m, keyCtrlL, keyCtrlL)
	if got := m.LayoutMode(); got != LayoutFocus {
		t.Fatalf("second Ctrl+L: %q, want focus", got)
	}
	m = sendKeys(m, keyCtrlL)
	if got := m.LayoutMode(); got != LayoutChat {
		t.Fatalf("third Ctrl+L: %q, want chat", got)
	}
	m = sendKeys(m, keyCtrlH)
	if got := m.LayoutMode(); got != LayoutSplit {
		t.Fatalf("Ctrl+H from chat-only: %q, want split", got)
	}
	if plain := squash(plainView(m)); !strings.Contains(plain, squash(narrowNoticeLead)) {
		t.Fatalf("back in the narrow split without the notice:\n%s", plainView(m))
	}
}

func TestChatOnlyKeepsWorkingWhileNarrowing(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	m = sendKeys(m, keyCtrlW, keyI)
	m = sendKeys(m, runes("rascunho")...)
	m = resize(m, 60, 24)
	if !m.chat.composing || m.Focus() != FocusChat {
		t.Fatalf("narrowing dropped compose: composing=%v focus=%q", m.chat.composing, m.Focus())
	}
	m = sendKeys(m, runes(" novo")...)
	if got := m.ComposeDraft(); got != "rascunho novo" {
		t.Fatalf("draft=%q", got)
	}
}

func TestLayoutKeysOnHomeScreenNeverLeaveTheList(t *testing.T) {
	sequences := map[string][]tea.KeyMsg{
		"ctrl+w": {keyCtrlW, keyCtrlW, keyCtrlW, keyCtrlW, keyCtrlW},
		"ctrl+h": {keyCtrlH, keyCtrlH, keyCtrlH},
		"ctrl+l": {keyCtrlL, keyCtrlL, keyCtrlL},
		"mixed":  {keyCtrlW, keyCtrlH, keyCtrlL, keyCtrlW, keyCtrlL, keyCtrlH},
	}
	for _, width := range []int{120, 80, 60} {
		for name, keys := range sequences {
			t.Run(fmt.Sprintf("%s@%d", name, width), func(t *testing.T) {
				m := setupHomeModel(t, width, &fakeChat{})
				for i, k := range keys {
					m = sendKeys(m, k)
					plain := plainView(m)
					if !strings.Contains(plain, "Counter-Strike") {
						t.Fatalf("press %d: list gone:\n%s", i+1, plain)
					}
					if !strings.Contains(plain, noChatNotice) {
						t.Fatalf("press %d: no explanation:\n%s", i+1, plain)
					}
					if strings.Contains(plain, focusHintLead) || strings.Contains(plain, "layout:") {
						t.Fatalf("press %d: announces a layout with no content:\n%s", i+1, plain)
					}
					if m.Focus() != FocusList {
						t.Fatalf("press %d: Focus()=%q", i+1, m.Focus())
					}
				}
			})
		}
	}
}

// Any other key clears the one-shot notice and the home screen goes on working.
func TestHomeScreenStaysOperableAfterCtrlW(t *testing.T) {
	m := setupHomeModel(t, 80, &fakeChat{})
	m = sendKeys(m, keyCtrlW, tea.KeyMsg{Type: tea.KeyDown})
	if plain := plainView(m); strings.Contains(plain, noChatNotice) || !strings.Contains(plain, "Counter-Strike") {
		t.Fatalf("after Down:\n%s", plain)
	}
}

// Compose counts as a chat panel, so a logged-in user can reach chat-only with
// no channel; leaving compose must not strand them on an empty screen.
func TestLeavingComposeWithoutChannelKeepsTheList(t *testing.T) {
	m := setupHomeModel(t, 80, &fakeChat{hasToken: true, username: "tester"})
	m = sendKeys(m, keyI, keyCtrlW, keyEsc)
	if m.chat.hasPanel() {
		t.Fatal("test expects no chat panel after Esc")
	}
	if plain := plainView(m); !strings.Contains(plain, "Counter-Strike") {
		t.Fatalf("list gone after leaving compose:\n%s", plain)
	}
	if status := ansi.Strip(m.StatusBar()); strings.Contains(status, "layout: chat") {
		t.Fatalf("bar announces a chat that is not there: %q", status)
	}
}

func TestCtrlWWithChatCyclesFourDistinctScreens(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)

	steps := []struct {
		mode        LayoutMode
		list, chat  bool
		hint, panel bool
	}{
		{mode: LayoutChat, chat: true},
		{mode: LayoutList, list: true},
		{mode: LayoutFocus, hint: true},
		{mode: LayoutSplit, list: true, chat: true, panel: true},
	}
	seen := map[string]LayoutMode{}
	for _, step := range steps {
		m = sendKeys(m, keyCtrlW)
		if got := m.LayoutMode(); got != step.mode {
			t.Fatalf("LayoutMode=%q, want %q", got, step.mode)
		}
		body := bodyOf(m)
		if got := strings.Contains(body, "Gaules"); got != step.list {
			t.Fatalf("%s: list=%v:\n%s", step.mode, got, body)
		}
		if got := strings.Contains(body, "Chat — #gaules"); got != step.chat {
			t.Fatalf("%s: chat=%v:\n%s", step.mode, got, body)
		}
		if got := strings.Contains(body, focusHintLead); got != step.hint {
			t.Fatalf("%s: hint=%v:\n%s", step.mode, got, body)
		}
		if got := strings.Contains(body, "│"); got != step.panel {
			t.Fatalf("%s: separator=%v:\n%s", step.mode, got, body)
		}
		if prev, dup := seen[body]; dup {
			t.Fatalf("%s draws the same screen as %s:\n%s", step.mode, prev, body)
		}
		seen[body] = step.mode
	}
}

func TestWidthNoticeStaysOffWhenLayoutHidesTheChat(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)

	for _, press := range []struct {
		key  tea.KeyMsg
		mode LayoutMode
	}{{keyCtrlW, LayoutChat}, {keyCtrlW, LayoutList}, {keyCtrlW, LayoutFocus}} {
		m = resize(m, 120, 24)
		m = sendKeys(m, press.key)
		m = resize(m, 60, 24)
		if got := m.LayoutMode(); got != press.mode {
			t.Fatalf("LayoutMode=%q, want %q", got, press.mode)
		}
		if plain := plainView(m); strings.Contains(plain, narrowNoticeLead) {
			t.Fatalf("%s at 60 cols shows the width notice:\n%s", press.mode, plain)
		}
		if m.shouldShowNarrowChatNotice() {
			t.Fatalf("%s: shouldShowNarrowChatNotice=true", press.mode)
		}
	}
}

// Hidden by layout is the user's choice: chat keys must not blame the width.
func TestChatKeysStayQuietWhenLayoutHidesTheChat(t *testing.T) {
	for name, k := range map[string]tea.KeyMsg{"tab": keyTab, "i": keyI, "y": keyY} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m = resize(m, 120, 24)
			m = sendKeys(m, keyCtrlL)
			m = resize(m, 60, 24)
			m = sendKeys(m, k)
			if m.StatusNotice() != "" {
				t.Fatalf("notice for a chat the user hid: %q", m.StatusNotice())
			}
		})
	}
}

func TestEscLeavesFocusModeAloneAndHelpNamesTheWayBack(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 120, 24)
	m = sendKeys(m, keyCtrlW, keyCtrlW, keyCtrlW, keyEsc)
	if got := m.LayoutMode(); got != LayoutFocus {
		t.Fatalf("Esc changed the layout to %q", got)
	}
	plain := plainView(m)
	for _, want := range []string{"Ctrl+H", "Ctrl+L", "Ctrl+W"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("focus hint missing %q:\n%s", want, plain)
		}
	}

	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		cat, err := i18n.New(lang)
		if err != nil {
			t.Fatalf("i18n.New: %v", err)
		}
		help := New(cat)
		help.layout.helpVisible = true
		help = resize(help, 120, 40)
		rows := strings.Join(help.renderHelpLines(120), "\n")
		wants := map[string]string{
			i18n.LangEN:   "Esc keeps the layout",
			i18n.LangPTBR: "Esc mantém o layout",
		}
		if !strings.Contains(rows, wants[lang]) {
			t.Fatalf("%s help does not explain Esc and the layout:\n%s", lang, rows)
		}
		for _, want := range []string{"Ctrl+W", "Ctrl+H", "Ctrl+L", "80"} {
			if !strings.Contains(rows, want) {
				t.Fatalf("%s help missing %q:\n%s", lang, want, rows)
			}
		}
	}
}

// The notice wraps at narrow widths, so the list must give up every row it takes
// or the highlighted channel falls off the bottom.
func TestNarrowNoticeNeverHidesTheHighlight(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 40, 8)
	if rows := len(m.narrowChatNoticeLines(40)); rows < 2 {
		t.Fatalf("notice takes %d row, the test needs it wrapped", rows)
	}
	m.list.channels = nil
	for i := range 12 {
		login := fmt.Sprintf("canal%02d", i)
		m.list.channels = append(m.list.channels, catalog.Channel{ID: login, Login: login, DisplayName: login, Viewers: catalog.IntPtr(10)})
	}
	for range 40 {
		m = sendKeys(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if plain := plainView(m); !strings.Contains(plain, "▸") || !strings.Contains(plain, "canal11") {
		t.Fatalf("highlight off screen:\n%s", plain)
	}
}
