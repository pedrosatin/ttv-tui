package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// helpSectionRows splits the whole help, at a width where nothing wraps, into
// the rows of each section.
func helpSectionRows(t *testing.T, m Model) map[helpSection][]string {
	t.Helper()
	doc := m.helpDocument(400)
	out := map[helpSection][]string{}
	for i, s := range helpSections {
		end := len(doc.body)
		if i+1 < len(doc.starts) {
			end = doc.starts[i+1]
		}
		for _, row := range doc.body[doc.starts[i]:end] {
			out[s] = append(out[s], ansi.Strip(row))
		}
	}
	return out
}

// helpRowWith is the row of the section that shows keys at the key column and
// text after them, or "" when none does.
// A text that wraps goes on in the rows below, indented to the text column.
func helpRowWith(rows []string, keys, text string) string {
	for i, row := range rows {
		trimmed := strings.TrimSpace(row)
		rest, ok := strings.CutPrefix(trimmed, keys+" ")
		if !ok && trimmed == keys {
			// Keys wider than the column sit alone, with the text on the next row.
			rest, ok = "", true
		}
		if !ok {
			continue
		}
		got := strings.TrimSpace(rest)
		for _, next := range rows[i+1:] {
			if indent := len(next) - len(strings.TrimLeft(next, " ")); indent < 6 || strings.TrimSpace(next) == "" {
				break
			}
			got = strings.TrimSpace(got + " " + strings.TrimSpace(next))
		}
		if got == text {
			return row
		}
	}
	return ""
}

// The keys the UX review found working but missing from the help (analise.md,
// "Teclas que funcionam e faltam na ajuda") show up in the section where the
// viewer looks for them, with the text of what they do there.
func TestHelpShowsTheKeysItLackedInTheRightSection(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m := twoPlatforms(t, lang, &countingSource{}, &countingSource{})
			m.layout.helpVisible = true
			sections := helpSectionRows(t, m)
			space := keyLabel("space", m.catalog)
			for _, want := range []struct {
				section    helpSection
				keys, text string
			}{
				// Chat: j/k scroll, Enter on the panel starts writing.
				{sectionChat, "↑/k ↓/j", "help.use.chat_scroll"},
				{sectionChat, "Enter", "help.use.chat_enter"},
				// Writing: Enter sends, Esc leaves and keeps the draft, Backspace/Delete.
				{sectionChat, "Enter", "help.use.compose_send"},
				{sectionChat, "Esc", "help.use.compose_leave"},
				{sectionChat, "Backspace/Delete", "help.field.erase"},
				// Search prompt: Enter or Ctrl+F confirms, Esc cancels.
				{sectionSearch, "Enter Ctrl+F/S", "help.use.search_confirm"},
				{sectionSearch, "Esc", "help.use.search_cancel"},
				// Filter: ↑/↓ while typing, Enter opens or searches, Ctrl+F searches, Esc clears.
				{sectionSearch, "↑/↓ PgUp PgDn", "help.use.filter_move"},
				{sectionSearch, "Enter", "help.use.filter_enter"},
				{sectionSearch, "Ctrl+F/S", "help.use.filter_search"},
				{sectionSearch, "Esc", "help.use.filter_clear"},
				// Filter menu: ↑/↓/j/k, Home/End/G, PgUp/PgDn; l also closes.
				{sectionSearch, "↑/k ↓/j PgUp PgDn Home/g End/G", "help.use.menu_move"},
				{sectionSearch, "Enter " + space, "help.use.menu_toggle"},
				{sectionSearch, "Esc l", "help.use.menu_close"},
				// Esc from a search goes back where it came from.
				{sectionNav, "Esc", "action.back"},
				// f also takes the favorite away.
				{sectionNav, "f", "action.favorite"},
				// Volume: = and _ too.
				{sectionVideo, "+/=", "action.volume_up"},
				{sectionVideo, "-/_", "action.volume_down"},
				// r also retries the video, in the video section.
				{sectionVideo, "r", "action.retry"},
				// Ctrl+C explains how to quit.
				{sectionGlobal, "Ctrl+C", "action.ctrl_c"},
				// The window keys, always.
				{sectionWindow, "Ctrl+O", "action.chat_overlay"},
				{sectionWindow, "Ctrl+P", "action.swap_sides"},
				{sectionWindow, "Alt+↑", "action.chat_more_opaque"},
				{sectionWindow, "Alt+↓", "action.chat_less_opaque"},
				{sectionWindow, "Ctrl+→", "action.list_wider"},
				{sectionWindow, "Ctrl+←", "action.list_narrower"},
				{sectionWindow, "Alt+←", "action.chat_wider"},
				{sectionWindow, "Alt+→", "action.chat_narrower"},
			} {
				if helpRowWith(sections[want.section], want.keys, m.t(want.text)) == "" {
					t.Errorf("seção %s sem a linha %q %q:\n%s", m.helpSectionName(want.section), want.keys, m.t(want.text),
						strings.Join(sections[want.section], "\n"))
				}
			}
			// The settings section names the language, NO_COLOR and the colors.
			settings := strings.Join(sections[sectionSettings], "\n")
			for _, want := range []string{m.catalog.Lang(), "NO_COLOR", lastWords(m.t("help.colors"), 2)} {
				if !strings.Contains(settings, want) {
					t.Errorf("Configurações sem %q:\n%s", want, settings)
				}
			}
		})
	}
}

// The last player error goes to the settings section.
func TestHelpPlayerErrorIsInTheSettingsSection(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangPTBR))
	m.layout.helpVisible = true
	m.player.lastLine = "streamlink saiu com 1"
	if got := strings.Join(helpSectionRows(t, m)[sectionSettings], "\n"); !strings.Contains(got, "streamlink saiu com 1") {
		t.Fatalf("Configurações sem o último erro do player:\n%s", got)
	}
}

// The help opens at the section of what the focus is using.
func TestHelpOpensAtTheSectionOfTheFocus(t *testing.T) {
	want := map[string]helpSection{
		"list": sectionNav, "filter": sectionSearch, "search": sectionSearch,
		"chat": sectionChat, "compose": sectionChat,
	}
	modes := append(everyModeEnter(t), struct {
		name  string
		enter func(t *testing.T, m Model) Model
	}{"filter menu", func(t *testing.T, m Model) Model {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		return m
	}})
	want["filter menu"] = sectionSearch
	for _, mode := range modes {
		for name, open := range map[string]tea.KeyMsg{"f1": keyHelpF1, "?": keyHelpAsk} {
			if name == "?" && mode.name != "list" && mode.name != "chat" {
				continue // ? is a letter in a text field, and the menu takes its own keys
			}
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				m, _, _ := setupTwoPanelModel(t)
				m = resize(m, 80, 24)
				m, _ = pressKey(m, keyEsc)
				m = mode.enter(t, m)
				m = sendKeys(m, open)
				if !m.HelpVisible() {
					t.Fatal("a ajuda não abriu")
				}
				header := helpSectionHeader(m, want[mode.name])
				if got := helpTopRow(m); got != header {
					t.Fatalf("a ajuda abriu em %q, quer %q:\n%s", got, header, strings.Join(helpBody(m), "\n"))
				}
			})
		}
	}
}

// In 80x24 the help scrolls and its last row says where one is: the section,
// its position among the sections and the row.
func TestHelpPositionRowNamesTheSection(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = mustCatalog(t, lang)
			m = resize(m, 80, 24)
			m = sendKeys(m, keyEsc, keyHelpAsk)
			rows := helpBody(m)
			last := rows[len(rows)-1]
			where := m.t("help.position")
			for _, want := range []string{m.helpSectionName(sectionNav), strings.Fields(where)[2], "2", "7", m.t("help.position_hint")} {
				if !strings.Contains(last, want) {
					t.Errorf("linha de posição %q sem %q", last, want)
				}
			}
			if _, _, ok := helpPosition(m); !ok {
				t.Fatalf("linha de posição sem a linha atual: %q", last)
			}
		})
	}
}

// Tab and → go to the next section, Shift+Tab and ← to the previous one, and
// both wrap around.
func TestHelpTabAndArrowsJumpSections(t *testing.T) {
	m, _, _ := setupTwoPanelModel(t)
	m = resize(m, 80, 24)
	m = sendKeys(m, keyEsc, keyHelpAsk)
	at := helpSectionIndex(sectionNav)
	for _, step := range []struct {
		key   tea.KeyMsg
		delta int
	}{
		{keyHelpTab, 1}, {tea.KeyMsg{Type: tea.KeyRight}, 1}, {keyHelpShiftTab, -1}, {tea.KeyMsg{Type: tea.KeyLeft}, -1},
		{keyHelpShiftTab, -1}, {keyHelpShiftTab, -1},
	} {
		m = sendKeys(m, step.key)
		at = (at + step.delta + len(helpSections)) % len(helpSections)
		want := helpSections[at]
		if m.layout.helpSection != want {
			t.Fatalf("%s levou à seção %d, quer %d", step.key, m.layout.helpSection, want)
		}
		// A section near the end may start below the top: the last page holds it.
		if top := helpTopRow(m); top != helpSectionHeader(m, want) && !strings.Contains(strings.Join(helpBody(m), "\n"), helpSectionHeader(m, want)) {
			t.Fatalf("%s: seção %q fora da tela:\n%s", step.key, helpSectionHeader(m, want), strings.Join(helpBody(m), "\n"))
		}
	}
	// Scrolling row by row updates the section the position row names.
	m = sendKeys(m, keyHome)
	for range 200 {
		if m.layout.helpSection == sectionNav {
			break
		}
		m = sendKeys(m, keyDown)
	}
	if helpTopRow(m) != helpSectionHeader(m, sectionNav) {
		t.Fatalf("rolando, a seção virou Navegação antes do título dela chegar ao topo:\n%s", strings.Join(helpBody(m), "\n"))
	}
}

// The window keys are always in the help. Outside the window they sit under
// "window only"; inside it, with no mark.
func TestHelpWindowKeysCarryTheMarkOnlyOutsideTheWindow(t *testing.T) {
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m := New(mustCatalog(t, lang))
			m.layout.helpVisible = true
			mark := m.t("help.ctx.window_only")
			tui := strings.Join(helpSectionRows(t, m)[sectionWindow], "\n")
			if !strings.Contains(tui, mark) || !strings.Contains(tui, "Ctrl+O") {
				t.Fatalf("fora da janela, sem %q ou sem Ctrl+O:\n%s", mark, tui)
			}
			markRow := strings.Index(tui, mark)
			if strings.Index(tui, "Ctrl+O") < markRow || strings.Index(tui, "Ctrl+P") < markRow {
				t.Fatalf("Ctrl+O e Ctrl+P fora do grupo %q:\n%s", mark, tui)
			}

			m.hosted.host = &recordingHost{}
			window := strings.Join(helpSectionRows(t, m)[sectionWindow], "\n")
			if strings.Contains(window, mark) || !strings.Contains(window, "Ctrl+O") {
				t.Fatalf("na janela, com %q ou sem Ctrl+O:\n%s", mark, window)
			}
		})
	}
}

// The help's own keys are in the registry, under the help mode, and every row
// of edit primitives names keys the text field really takes first.
func TestHelpKeysComeFromTheRegistry(t *testing.T) {
	keys := defaultKeys()
	for name, want := range map[string]actionID{
		"tab": actHelpNext, "right": actHelpNext, "shift+tab": actHelpPrev, "left": actHelpPrev,
		"q": actCloseHelp, "esc": actBack, "up": actUp, "end": actEnd,
	} {
		msg, _ := parseKeyName(name)
		if got := keys.actionFor(modeHelp, msg); got != want {
			t.Errorf("%s na ajuda dispara %q, quer %s", name, got, want)
		}
	}
	for _, f := range fieldKeys {
		for _, name := range f.Keys {
			msg, ok := parseKeyName(name)
			if !ok || !isFieldEditKey(msg) && fieldArrow(msg) == 0 {
				t.Errorf("%q na ajuda como primitiva, mas o campo não a trata", name)
			}
		}
		for _, use := range f.Uses {
			if !slices.Contains(helpSections, use.Section) {
				t.Errorf("%v: seção %d fora da ajuda", f.Keys, use.Section)
			}
		}
	}
}
