package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestResolveKeyOverridesDesconhecidoInvalidoConflitoEImprimivel(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][]string{
		"nao_existe":  {"ctrl+b"},
		"toggle_list": {"ctrl+b", "nao-e-tecla"},
		"toggle_chat": {"x"}, // imprimível em modo global
		"mute":        {"f"}, // f já é favorito no modo normal; mute é modeNormal
	}
	// No classic, mute conflita com favorite (f) no mesmo contexto.
	overrides, notices := ResolveKeyOverrides(true, raw, cat)
	if _, ok := overrides[actionID("action.nao_existe")]; ok {
		t.Fatal("ação desconhecida entrou nos overrides")
	}
	// ctrl+b vale; a inválida foi descartada. Sem conflito no classic (ctrl+b livre).
	if got := overrides[actToggleList]; !slices.Equal(got, []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v, quer [ctrl+b]", got)
	}
	if _, ok := overrides[actToggleChat]; ok {
		t.Fatal("tecla imprimível em ação global não deve aplicar")
	}
	if _, ok := overrides[actMute]; ok {
		t.Fatal("conflito com favorito deveria manter o padrão do mute")
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{"nao_existe", "nao-e-tecla", "x", "mute"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("aviso sem %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "action.mute") || strings.Contains(joined, "action.nao_existe") {
		t.Fatalf("avisos ainda usam forma longa:\n%s", joined)
	}
}

func TestResolveKeyOverridesListaVaziaDesliga(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"help": {},
	}, cat)
	if got := overrides[actHelp]; got == nil || len(got) != 0 {
		t.Fatalf("help = %#v, quer desligada", got)
	}
	if len(notices) != 0 {
		t.Fatalf("avisos inesperados: %v", notices)
	}
	reg, _ := EffectiveKeymap(false, map[string][]string{"help": {}}, cat)
	if reg.actionFor(modeNormal, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}) != "" {
		t.Fatal("? ainda dispara a ajuda desligada")
	}
}

func TestResolveKeyOverridesShortNameRemap(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"toggle_list": {"ctrl+b"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	if !slices.Equal(overrides[actToggleList], []string{"ctrl+b"}) {
		t.Fatalf("overrides = %v", overrides[actToggleList])
	}
	bindings, _ := KeymapReport(true, map[string][]string{"toggle_list": {"ctrl+b"}}, cat)
	found := false
	for _, b := range bindings {
		if b.Action == "toggle_list" {
			found = true
			if !slices.Equal(b.Keys, []string{"ctrl+b"}) {
				t.Fatalf("report keys = %v", b.Keys)
			}
		}
		if strings.HasPrefix(b.Action, "action.") {
			t.Fatalf("doctor ainda mostra forma longa: %q", b.Action)
		}
	}
	if !found {
		t.Fatal("report sem toggle_list")
	}
}

func TestResolveKeyOverridesAceitaFormaLongaComAvisoDeDuplicata(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"action.toggle_list": {"ctrl+l"},
		"toggle_list":        {"ctrl+b"},
	}, cat)
	if !slices.Equal(overrides[actToggleList], []string{"ctrl+b"}) {
		t.Fatalf("curta deveria vencer: %v", overrides[actToggleList])
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "toggle_list") {
		t.Fatalf("sem aviso de duplicata:\n%s", joined)
	}
}

func TestResolveKeyOverridesConflitoEntreContextos(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	// No default, ctrl+b é window_list (modeWindow); toggle_list é modeGlobal.
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"toggle_list": {"ctrl+b"},
	}, cat)
	if _, ok := overrides[actToggleList]; ok {
		t.Fatal("toggle_list=ctrl+b deveria conflitar com window_list")
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "window_list") || !strings.Contains(joined, "toggle_list") {
		t.Fatalf("aviso de conflito incompleto:\n%s", joined)
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"toggle_list": {"ctrl+q"},
	}, cat)
	if _, ok := overrides[actToggleList]; ok {
		t.Fatal("toggle_list=ctrl+q deveria conflitar com quit")
	}
	if !strings.Contains(strings.Join(notices, "\n"), "quit") {
		t.Fatalf("sem conflito com quit:\n%s", strings.Join(notices, "\n"))
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"up": {"ctrl+x"},
	}, cat)
	if _, ok := overrides[actUp]; ok {
		t.Fatal("up=ctrl+x deveria conflitar com stop_video")
	}
	if !strings.Contains(strings.Join(notices, "\n"), "stop_video") {
		t.Fatalf("sem conflito com stop_video:\n%s", strings.Join(notices, "\n"))
	}
}

func TestResolveKeyOverridesOrdemIndependente(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	// Troca j/k no classic (up=k, down=j por padrão).
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"up":   {"j"},
		"down": {"k"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos na troca: %v", notices)
	}
	if !slices.Equal(overrides[actUp], []string{"j"}) || !slices.Equal(overrides[actDown], []string{"k"}) {
		t.Fatalf("up=%v down=%v", overrides[actUp], overrides[actDown])
	}

	// Lista vazia libera a tecla para outra ação no mesmo config.
	overrides, notices = ResolveKeyOverrides(true, map[string][]string{
		"favorite":    {},
		"mute":        {"f"},
		"toggle_list": {"ctrl+b"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	if got := overrides[actFavorite]; got == nil || len(got) != 0 {
		t.Fatalf("favorite não desligou: %#v", got)
	}
	if !slices.Equal(overrides[actMute], []string{"f"}) {
		t.Fatalf("mute não pegou f liberado: %v", overrides[actMute])
	}

	// No default, mover window_list libera ctrl+b para toggle_list.
	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"toggle_list": {"ctrl+b"},
		"window_list": {"ctrl+g"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	if !slices.Equal(overrides[actToggleList], []string{"ctrl+b"}) {
		t.Fatalf("toggle_list = %v", overrides[actToggleList])
	}
	if !slices.Equal(overrides[actWindowList], []string{"ctrl+g"}) {
		t.Fatalf("window_list = %v", overrides[actWindowList])
	}
}

func TestResolveKeyOverridesPrefixosESequencias(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"help": {"g h"},
	}, cat)
	if _, ok := overrides[actHelp]; ok {
		t.Fatal("classic não deveria aceitar sequência")
	}
	if !strings.Contains(strings.Join(notices, "\n"), "classic") {
		t.Fatalf("sem aviso de sequência no classic:\n%s", strings.Join(notices, "\n"))
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"mute": {"g"},
	}, cat)
	if _, ok := overrides[actMute]; ok {
		t.Fatal("g isolado deveria ser recusado enquanto há sequências g *")
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "prefix") && !strings.Contains(joined, "live prefix") {
		t.Fatalf("sem aviso de prefixo:\n%s", joined)
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"toggle_list": {"ctrl+w ctrl+h"},
	}, cat)
	if _, ok := overrides[actToggleList]; ok {
		t.Fatal("ctrl+letra como segunda tecla deveria ser recusado")
	}
	if len(notices) == 0 {
		t.Fatal("sem aviso de ctrl+ segunda tecla")
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"help": {"g h"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("help=g h deveria ser aceito: %v", notices)
	}
	if !slices.Equal(overrides[actHelp], []string{"g h"}) {
		t.Fatalf("help = %v", overrides[actHelp])
	}
}

func TestCategoriesSequenciaNaoEImprimivel(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	// O próprio padrão do default: categories = ["g c", "ctrl+r"].
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"categories": {"g c", "ctrl+r"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("padrão de categories recusado: %v", notices)
	}
	if !slices.Equal(overrides[actCategories], []string{"g c", "ctrl+r"}) {
		t.Fatalf("categories = %v", overrides[actCategories])
	}
}

func TestCanonicalKeyNameSequenciaComSpace(t *testing.T) {
	msg, ok := parseKeyName("ctrl+w space")
	if !ok {
		t.Fatal("parseKeyName falhou")
	}
	if got := canonicalKeyName("ctrl+w space", msg); got != "ctrl+w space" {
		t.Fatalf("canonical = %q", got)
	}
	en, _ := i18n.New(i18n.LangEN)
	pt, _ := i18n.New(i18n.LangPTBR)
	if got := keyLabel("ctrl+w space", en); got != "Ctrl+W Space" {
		t.Fatalf("label en = %q", got)
	}
	if got := keyLabel("ctrl+w space", pt); got != "Ctrl+W Espaço" {
		t.Fatalf("label pt = %q", got)
	}
}

func TestKeymapNoticeSanitizadoNaView(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m.keymapNotice = "bad \x1b[2J toggle_list"
	view := m.View()
	if strings.Contains(view, "\x1b[2J") {
		t.Fatalf("escape chegou cru à View:\n%q", view)
	}
	if !strings.Contains(ansi.Strip(view), "toggle_list") {
		t.Fatalf("aviso sumiu:\n%s", ansi.Strip(view))
	}
}

func TestDicasSomemComAcaoDesligada(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, _ := ResolveKeyOverrides(false, map[string][]string{
		"help":          {},
		"help_anywhere": {},
		"quit":          {},
	}, cat)
	m, _ := newScenario(t, 120, 40, WithKeymapOverrides(false, overrides, ""))
	bar := ansi.Strip(m.StatusBar())
	if strings.Contains(bar, " help") || strings.Contains(bar, "help") {
		t.Fatalf("dica de help sobrou: %q", bar)
	}
	if strings.Contains(bar, "quit") {
		t.Fatalf("dica de quit sobrou: %q", bar)
	}
	footer := ansi.Strip(m.Footer())
	if strings.Contains(footer, " help") || strings.HasSuffix(strings.TrimSpace(footer), "help") {
		t.Fatalf("rodapé com help sobrando: %q", footer)
	}
}

func TestDicasParciaisHelpEHelpAnywhere(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	onlyAnywhere, _ := ResolveKeyOverrides(false, map[string][]string{
		"help": {},
	}, cat)
	m, _ := newScenario(t, 120, 40, WithKeymapOverrides(false, onlyAnywhere, ""))
	bar := ansi.Strip(m.StatusBar())
	if strings.Contains(bar, " help") && !strings.Contains(bar, "F1") {
		t.Fatalf("dica parcial sem F1: %q", bar)
	}
	if !strings.Contains(bar, "F1") {
		t.Fatalf("status sem F1 com help_anywhere ativo: %q", bar)
	}
	m.list.search.active = true
	footer := ansi.Strip(m.Footer())
	if !strings.Contains(footer, "F1") {
		t.Fatalf("rodapé de campo sem F1: %q", footer)
	}

	onlyHelp, _ := ResolveKeyOverrides(false, map[string][]string{
		"help_anywhere": {},
	}, cat)
	m, _ = newScenario(t, 120, 40, WithKeymapOverrides(false, onlyHelp, ""))
	bar = ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "?") {
		t.Fatalf("status sem ? com help ativo: %q", bar)
	}
	m.list.search.active = true
	footer = ansi.Strip(m.Footer())
	if strings.Contains(footer, "F1") || strings.Contains(footer, " help") {
		t.Fatalf("rodapé de campo com help_anywhere desligado: %q", footer)
	}
}

func TestPrefixoDespachaAcaoForaDaListaFixa(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"help": {"g h"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	m, _ := newScenario(t, 80, 24, WithKeymapOverrides(false, overrides, ""))
	if m.helpOpen() {
		t.Fatal("ajuda já aberta")
	}
	m = play(t, m, "g")
	if m.prefix != "g" {
		t.Fatalf("prefixo = %q", m.prefix)
	}
	m = play(t, m, "h")
	if !m.helpOpen() {
		t.Fatal("g h não abriu a ajuda")
	}
}

// Com toggle_list = ["ctrl+b"] e window_list em outra tecla, a ajuda e o rodapé
// mostram Ctrl+B e a tecla esconde a lista no keymap default.
func TestCenarioTeclaCustomToggleList(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][]string{
		"toggle_list": {"ctrl+b"},
		"window_list": {"ctrl+g"},
	}
	overrides, notices := ResolveKeyOverrides(false, raw, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}

	m, _ := newScenario(t, 120, 40, WithKeymapOverrides(false, overrides, ""))
	if !slices.Equal(m.keys.keys[actToggleList], []string{"ctrl+b"}) {
		t.Fatalf("teclas efetivas = %v", m.keys.keys[actToggleList])
	}

	m = play(t, m, "enter enter")

	m = play(t, m, "?")
	for i := 0; i < len(helpSections) && m.layout.helpSection != sectionWindow; i++ {
		m = play(t, m, "tab")
	}
	if m.layout.helpSection != sectionWindow {
		t.Fatalf("seção da ajuda = %v, quer janelas", m.layout.helpSection)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Ctrl+B") {
		t.Fatalf("ajuda sem Ctrl+B:\n%s", view)
	}
	m = play(t, m, "q")

	for i := 0; i < 4 && m.LayoutMode() != LayoutFocus; i++ {
		m = play(t, m, "ctrl+w w")
	}
	if m.LayoutMode() != LayoutFocus {
		t.Fatalf("layout = %v, quer foco", m.LayoutMode())
	}
	footer := m.Footer()
	if !strings.Contains(footer, "Ctrl+B") {
		t.Fatalf("rodapé sem Ctrl+B: %q", footer)
	}

	for i := 0; i < 4 && m.LayoutMode() != LayoutSplit; i++ {
		m = play(t, m, "ctrl+w w")
	}
	if !m.layout.showList {
		t.Fatal("lista já escondida antes de Ctrl+B")
	}
	m = play(t, m, "ctrl+b")
	if m.layout.showList {
		t.Fatal("Ctrl+B não escondeu a lista")
	}
}

func TestCenarioTeclaCustomToggleListClassic(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	overrides, notices := ResolveKeyOverrides(true, map[string][]string{
		"toggle_list": {"ctrl+b"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}

	m, _ := newScenarioClassic(t, 120, 40, WithKeymapOverrides(true, overrides, ""))
	m = play(t, m, "enter enter")
	m = play(t, m, "?")
	for i := 0; i < len(helpSections) && m.layout.helpSection != sectionWindow; i++ {
		m = play(t, m, "tab")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Ctrl+B") {
		t.Fatalf("ajuda sem Ctrl+B:\n%s", view)
	}
	m = play(t, m, "q")
	if !m.layout.showList {
		t.Fatal("lista já escondida")
	}
	m = play(t, m, "ctrl+b")
	if m.layout.showList {
		t.Fatal("Ctrl+B não escondeu a lista no classic")
	}
}

func TestResolveKeyOverridesReversaoAtePontoFixo(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"toggle_list": {"ctrl+q"},
		"mute":        {"ctrl+w h"},
	}, cat)
	if _, ok := overrides[actToggleList]; ok {
		t.Fatalf("toggle_list não deveria sobreviver após colidir no padrão: %v", overrides[actToggleList])
	}
	if _, ok := overrides[actMute]; ok {
		t.Fatalf("mute deveria reverter ao colidir com o padrão de toggle_list: %v", overrides[actMute])
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "quit") || !strings.Contains(joined, "toggle_list") {
		t.Fatalf("sem aviso do conflito inicial:\n%s", joined)
	}

	overrides, notices = ResolveKeyOverrides(false, map[string][]string{
		"categories": {"ctrl+q"},
		"favorites":  {"f2"},
		"recents":    {"f3"},
		"home":       {"home"},
		"mute":       {"g"},
	}, cat)
	if _, ok := overrides[actMute]; ok {
		t.Fatalf("mute=g deveria reverter quando categories volta com g c: %v", overrides[actMute])
	}
	if _, ok := overrides[actCategories]; ok {
		t.Fatalf("categories=ctrl+q deveria conflitar com quit: %v", overrides[actCategories])
	}
	if !slices.Equal(overrides[actFavorites], []string{"f2"}) {
		t.Fatalf("favorites = %v", overrides[actFavorites])
	}
	joined = strings.Join(notices, "\n")
	if !strings.Contains(joined, "prefix") && !strings.Contains(joined, "live prefix") {
		t.Fatalf("sem aviso de prefixo após reversão:\n%s", joined)
	}
}

func TestResolveKeyOverridesMuteGSemPrefixoVivo(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	raw := map[string][]string{
		"categories": {"f4"},
		"favorites":  {"f2"},
		"recents":    {"f3"},
		"home":       {"home"},
		"mute":       {"g"},
	}
	overrides, notices := ResolveKeyOverrides(false, raw, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	if !slices.Equal(overrides[actMute], []string{"g"}) {
		t.Fatalf("mute = %v", overrides[actMute])
	}
	reg, _ := EffectiveKeymap(false, raw, cat)
	if reg.livePrefixes["g"] {
		t.Fatal("g ainda é prefixo vivo")
	}
	m, _ := newScenario(t, 80, 24, WithKeymapOverrides(false, overrides, ""))
	if m.keys.actionFor(modeNormal, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}) != actMute {
		t.Fatal("g não mapeia mute no modo normal")
	}
	m = play(t, m, "g")
	if m.prefix != "" {
		t.Fatalf("entrou em modo prefixo com g: %q", m.prefix)
	}
}

func TestF1AposCtrlWComMenuFiltroAberto(t *testing.T) {
	m, _ := newScenario(t, 80, 24)
	m = m.openChannelFilterMenu()
	if !m.list.prefs.menuOpen {
		t.Fatal("menu de filtros fechado")
	}
	m = play(t, m, "ctrl+w")
	if m.prefix != "ctrl+w" {
		t.Fatalf("prefixo = %q", m.prefix)
	}
	m = play(t, m, "f1")
	if !m.helpOpen() {
		t.Fatal("F1 depois de ctrl+w com menu aberto não abriu a ajuda")
	}
	if m.prefix != "" {
		t.Fatalf("prefixo sobrou: %q", m.prefix)
	}
}

func TestSequenciaEmAcaoSoDeHelpOuConfirm(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	overrides, notices := ResolveKeyOverrides(false, map[string][]string{
		"help_next_section": {"g n"},
		"confirm":           {"g y"},
	}, cat)
	if _, ok := overrides[actHelpNext]; ok {
		t.Fatal("help_next_section não deveria aceitar sequência")
	}
	if _, ok := overrides[actConfirm]; ok {
		t.Fatal("confirm não deveria aceitar sequência")
	}
	joined := strings.Join(notices, "\n")
	if !strings.Contains(joined, "help_next_section") || !strings.Contains(joined, "confirm") {
		t.Fatalf("sem aviso de sequência modal:\n%s", joined)
	}
}

func TestFormatKeyWarningsLocalizados(t *testing.T) {
	warnings := []config.KeyWarning{
		{Line: 3, Kind: config.KeyWarningMissingEquals},
		{Line: 4, Kind: config.KeyWarningNotArray, Key: "bad"},
		{Line: 5, Kind: config.KeyWarningDuplicateShort, Key: "toggle_list"},
		{Line: 6, Kind: config.KeyWarningDuplicateForm, Key: "search"},
	}
	en, _ := i18n.New(i18n.LangEN)
	pt, _ := i18n.New(i18n.LangPTBR)
	enLines := FormatKeyWarnings(warnings, en)
	ptLines := FormatKeyWarnings(warnings, pt)
	if len(enLines) != 4 || len(ptLines) != 4 {
		t.Fatalf("en=%d pt=%d", len(enLines), len(ptLines))
	}
	if !strings.Contains(enLines[0], "missing '='") || !strings.Contains(ptLines[0], "falta de '='") {
		t.Fatalf("missing equals: en=%q pt=%q", enLines[0], ptLines[0])
	}
	if !strings.Contains(enLines[1], "not a string array") || !strings.Contains(ptLines[1], "não é um array") {
		t.Fatalf("not array: en=%q pt=%q", enLines[1], ptLines[1])
	}
	if !strings.Contains(enLines[2], "last line wins") || !strings.Contains(ptLines[2], "vale a última linha") {
		t.Fatalf("duplicate short: en=%q pt=%q", enLines[2], ptLines[2])
	}
	if !strings.Contains(enLines[3], "short form") || !strings.Contains(ptLines[3], "forma curta") {
		t.Fatalf("duplicate form: en=%q pt=%q", enLines[3], ptLines[3])
	}
}

func TestEffectiveKeymapInvariantAcrossSampleConfigs(t *testing.T) {
	cat, _ := i18n.New(i18n.LangEN)
	samples := []map[string][]string{
		nil,
		{"toggle_list": {"ctrl+b"}, "window_list": {"ctrl+g"}},
		{"help": {}, "help_anywhere": {"f1"}},
		{"mute": {"g"}, "categories": {"f4"}, "favorites": {"f2"}, "recents": {"f3"}, "home": {"home"}},
		{"toggle_list": {"ctrl+q"}, "mute": {"ctrl+w h"}},
		{"ctrl_c": {"x"}},
		{"help_next_section": {"g n"}},
		{"up": {"k"}, "down": {"j"}},
	}
	assertInvariant := func(t *testing.T, reg keyRegistry) {
		t.Helper()
		for _, modes := range keyContexts {
			owner := map[string]actionID{}
			for _, a := range reg.actions {
				if !slices.ContainsFunc(a.Modes, func(m keyMode) bool { return slices.Contains(modes, m) }) {
					continue
				}
				for _, name := range reg.keys[a.ID] {
					name = indexName(name)
					if other, taken := owner[name]; taken && other != a.ID {
						t.Errorf("modos %v: %q dispara %s e %s", modes, name, other, a.ID)
					}
					owner[name] = a.ID
				}
			}
		}
	}
	for i, raw := range samples {
		reg, _ := EffectiveKeymap(false, raw, cat)
		assertInvariant(t, reg)
		reg, _ = EffectiveKeymap(true, raw, cat)
		assertInvariant(t, reg)
		if i == 0 {
			continue
		}
	}
}
