package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// notInHelp lists the actions the help leaves out, with the reason. A new
// hidden action has to be added here on purpose.
var notInHelp = map[actionID]string{
	actCtrlC: "Ctrl+C não é atalho: só avisa que a tecla de sair é outra, e o aviso cita essa tecla",
}

// Every action has its description in both languages: the help by sections and
// the config read it from there.
func TestEveryActionIsDescribedInBothLanguages(t *testing.T) {
	seen := map[actionID]bool{}
	for _, a := range defaultActions() {
		if seen[a.ID] {
			t.Errorf("%s aparece duas vezes no registro", a.ID)
		}
		seen[a.ID] = true
		if len(a.Modes) == 0 || len(a.Default) == 0 {
			t.Errorf("%s sem modo ou sem tecla padrão", a.ID)
		}
		for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
			if !mustCatalog(t, lang).Has(string(a.ID)) {
				t.Errorf("%s sem descrição em %s", a.ID, lang)
			}
		}
	}
}

// Every default key is a name the terminal delivers, and pressing it fires its
// action in each of its modes.
func TestEveryDefaultKeyFiresItsAction(t *testing.T) {
	keys := defaultKeys()
	for _, a := range defaultActions() {
		for _, name := range a.Default {
			msg, ok := parseKeyName(name)
			if !ok {
				t.Errorf("%s: %q não é nome de tecla", a.ID, name)
				continue
			}
			for _, mode := range a.Modes {
				if got := keys.actionFor(mode, msg); got != a.ID {
					t.Errorf("%s no modo %d dispara %q, quer %s", name, mode, got, a.ID)
				}
			}
		}
	}
}

// No key fires two actions in the same context: the modes a context consults
// share one keyboard.
func TestNoKeyFiresTwoActionsInOneContext(t *testing.T) {
	for _, modes := range keyContexts {
		owner := map[string]actionID{}
		for _, a := range defaultActions() {
			if !slices.ContainsFunc(a.Modes, func(m keyMode) bool { return slices.Contains(modes, m) }) {
				continue
			}
			for _, name := range a.Default {
				name = indexName(name)
				if other, taken := owner[name]; taken && other != a.ID {
					t.Errorf("modos %v: %q dispara %s e %s", modes, name, other, a.ID)
				}
				owner[name] = a.ID
			}
		}
	}
}

// A text field takes its edit primitives before any shortcut, so an action of a
// field mode bound to one of them would never fire.
func TestFieldModesLeaveTheEditPrimitivesToTheField(t *testing.T) {
	for _, a := range defaultActions() {
		for _, mode := range a.Modes {
			if mode != modeSearch && mode != modeFilter && mode != modeCompose {
				continue
			}
			for _, name := range a.Default {
				if msg, _ := parseKeyName(name); isFieldEditKey(msg) || fieldArrow(msg) != 0 && mode == modeFilter {
					t.Errorf("%s no modo %d usa %q, uma primitiva do campo", a.ID, mode, name)
				}
			}
		}
	}
}

// The tab some terminals send as a rune is still Tab.
func TestTabRuneIsTab(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\t'}}
	if got := defaultKeys().actionFor(modeNormal, msg); got != actSwitchFocus {
		t.Fatalf("runa \\t dispara %q", got)
	}
}

// uniqueKeys gives every action one key of its own, so the help can only show it
// if the text took it from the registry.
func uniqueKeys(t *testing.T) map[actionID][]string {
	t.Helper()
	runes := []rune("αβγδεζηθικλμνξοπρστυφχψωΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ")
	actions := defaultActions()
	if len(actions) > len(runes) {
		t.Fatalf("%d ações para %d teclas", len(actions), len(runes))
	}
	out := map[actionID][]string{}
	for i, a := range actions {
		out[a.ID] = []string{string(runes[i])}
	}
	return out
}

// Every action shows up in the help with its effective key, in both languages:
// with each key remapped, the help names the new one.
func TestHelpShowsEveryActionWithItsEffectiveKey(t *testing.T) {
	overrides := uniqueKeys(t)
	for _, lang := range []string{i18n.LangEN, i18n.LangPTBR} {
		t.Run(lang, func(t *testing.T) {
			m := twoPlatforms(t, lang, &countingSource{}, &countingSource{})
			m.hosted.host = &recordingHost{}
			m.keys = newKeyRegistry(defaultActions(), overrides)
			m.layout.helpVisible = true
			help := ansi.Strip(strings.Join(m.renderHelpLines(400), "\n"))
			for _, a := range defaultActions() {
				key := overrides[a.ID][0]
				_, hidden := notInHelp[a.ID]
				if hidden != (a.Section == sectionHidden) {
					t.Errorf("%s: seção %d e notInHelp=%v não combinam", a.ID, a.Section, hidden)
				}
				if shown := strings.Contains(help, key); shown == hidden {
					t.Errorf("%s (tecla %s): na ajuda = %v, quer %v\n%s", a.ID, key, shown, !hidden, help)
				}
			}
		})
	}
}

// keyReference is a {…} reference in a catalog text.
var keyReference = regexp.MustCompile(`\{[^{}]*\}`)

// Every key reference in both catalogs names an action of the registry, with an
// index the default map has.
func TestCatalogKeyReferencesResolve(t *testing.T) {
	keys := defaultKeys()
	for _, lang := range []string{"en", "pt-BR"} {
		data, err := os.ReadFile(filepath.Join("..", "i18n", "locales", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var texts map[string]string
		if err := json.Unmarshal(data, &texts); err != nil {
			t.Fatal(err)
		}
		cat := mustCatalog(t, lang)
		for k, text := range texts {
			for _, ref := range keyReference.FindAllString(text, -1) {
				label, ok := keys.refLabel(strings.Trim(ref, "{}"), cat)
				if !ok || label == "" {
					t.Errorf("%s %s: %s não resolve no registro", lang, k, ref)
				}
			}
			if got := keys.expandKeys(text, cat); strings.ContainsAny(got, "{}") {
				t.Errorf("%s %s: sobrou chave em %q", lang, k, got)
			}
		}
	}
}

func TestKeyLabels(t *testing.T) {
	en, pt := mustCatalog(t, i18n.LangEN), mustCatalog(t, i18n.LangPTBR)
	for name, want := range map[string]string{
		"ctrl+q":     "Ctrl+Q",
		"ctrl+right": "Ctrl+→",
		"alt+up":     "Alt+↑",
		"alt+j":      "Alt+j",
		"shift+tab":  "Shift+Tab",
		"pgdown":     "PgDn",
		"pgup":       "PgUp",
		"enter":      "Enter",
		"esc":        "Esc",
		"f1":         "F1",
		"G":          "G",
		"?":          "?",
		"+":          "+",
		" ":          "Space",
		"space":      "Space",
	} {
		if got := keyLabel(name, en); got != want {
			t.Errorf("keyLabel(%q) = %q, quer %q", name, got, want)
		}
	}
	if got := keyLabel("space", pt); got != "Espaço" {
		t.Errorf("espaço em pt-BR = %q", got)
	}
}

func TestJoinKeyLabels(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"Ctrl+←", "Ctrl+→"}, "Ctrl+←/→"},
		{[]string{"Alt+↑", "Alt+↓"}, "Alt+↑/↓"},
		{[]string{"PgUp", "PgDn"}, "PgUp/Dn"},
		{[]string{"Home", "g"}, "Home/g"},
		{[]string{"End", "Enter"}, "End/Enter"},
		{[]string{"F1", "F2"}, "F1/F2"},
		{[]string{"Space", "Space"}, "Space"},
		{[]string{"Ctrl+F", "", "Ctrl+S"}, "Ctrl+F/S"},
		{nil, ""},
	} {
		if got := joinKeyLabels(tc.in); got != tc.want {
			t.Errorf("joinKeyLabels(%q) = %q, quer %q", tc.in, got, tc.want)
		}
	}
}

func TestExpandKeys(t *testing.T) {
	cat := mustCatalog(t, i18n.LangEN)
	keys := defaultKeys()
	for in, want := range map[string]string{
		"{action.quit} quit":                       "Ctrl+Q quit",
		"{action.home} {action.end}":               "Home/g End/G",
		"({action.search:2} alt)":                  "(Ctrl+S alt)",
		"{action.up:1}{action.down:1}":             "↑↓",
		"{action.list_narrower/action.list_wider}": "Ctrl+←/→",
		"{action.up:3}x":                           "x",
		"{action.nope} {x} {":                      "{action.nope} {x} {",
		"sem chave":                                "sem chave",
	} {
		if got := keys.expandKeys(in, cat); got != want {
			t.Errorf("expandKeys(%q) = %q, quer %q", in, got, want)
		}
	}
}

// The screen names the remapped key.
func TestRemappedKeyReachesTheText(t *testing.T) {
	m := New(mustCatalog(t, i18n.LangEN))
	m.keys = newKeyRegistry(defaultActions(), map[actionID][]string{actQuit: {"ctrl+g"}})
	if got := m.t("status.quit_hint"); got != "Ctrl+G quit" {
		t.Fatalf("status.quit_hint = %q", got)
	}
	if got := m.keys.actionFor(modeApp, tea.KeyMsg{Type: tea.KeyCtrlG}); got != actQuit {
		t.Fatalf("Ctrl+G dispara %q", got)
	}
	if got := m.keys.actionFor(modeApp, tea.KeyMsg{Type: tea.KeyCtrlQ}); got != "" {
		t.Fatalf("Ctrl+Q ainda dispara %q", got)
	}
}
