package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/termtext"
)

// ResolveKeyOverrides valida o mapa [keys] do config sobre a base default ou
// classic e devolve os overrides aceitos e os avisos localizados. A chave do
// config é o ID sem o prefixo action. e a forma longa também é aceita. Ação
// desconhecida, tecla inválida, conflito no mesmo contexto e tecla imprimível
// em ação que vale na escrita geram aviso. Nesses casos a ação fica com o
// padrão e, na lista vazia explícita, desliga.
func ResolveKeyOverrides(classic bool, raw map[string][]string, cat *i18n.Catalog) (map[actionID][]string, []string) {
	if len(raw) == 0 {
		return nil, nil
	}
	actions := defaultActions()
	if classic {
		actions = classicActions()
	}
	return resolveKeyOverrides(actions, classic, raw, cat)
}

func resolveKeyOverrides(actions []action, classic bool, raw map[string][]string, cat *i18n.Catalog) (map[actionID][]string, []string) {
	byID := make(map[actionID]action, len(actions))
	occupied := make(map[keyMode]map[string]actionID)
	current := make(map[actionID][]string, len(actions))
	defaults := make(map[actionID][]string, len(actions))
	for _, a := range actions {
		byID[a.ID] = a
		defaults[a.ID] = slices.Clone(a.Default)
		current[a.ID] = slices.Clone(a.Default)
		claimKeys(occupied, a.ID, a.Modes, a.Default)
	}

	normalized, notices := normalizeRawActionKeys(raw, cat)

	shorts := make([]string, 0, len(normalized))
	for short := range normalized {
		shorts = append(shorts, short)
	}
	slices.Sort(shorts)

	overrides := make(map[actionID][]string)

	// Fase 1: listas vazias liberam as teclas padrão antes das demais trocas.
	for _, short := range shorts {
		if len(normalized[short]) != 0 {
			continue
		}
		id := actionID("action." + short)
		a, ok := byID[id]
		if !ok {
			notices = append(notices, fmt.Sprintf(cat.T("keymap.unknown_action"), sanitizeScreenText(short)))
			continue
		}
		releaseKeys(occupied, id, a.Modes, current[id])
		current[id] = nil
		overrides[id] = []string{}
	}

	type candidate struct {
		id    actionID
		short string
		a     action
		keys  []string
	}
	var candidates []candidate

	// Fase 2: valida teclas de cada override não vazio (ainda sem conflito de mapa).
	for _, short := range shorts {
		want := normalized[short]
		if len(want) == 0 {
			continue
		}
		id := actionID("action." + short)
		a, ok := byID[id]
		if !ok {
			notices = append(notices, fmt.Sprintf(cat.T("keymap.unknown_action"), sanitizeScreenText(short)))
			continue
		}
		var valid []string
		seen := map[string]struct{}{}
		for _, k := range want {
			msg, ok := parseKeyName(k)
			if !ok {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.invalid_key"), sanitizeScreenText(k), sanitizeScreenText(short)))
				continue
			}
			canon := canonicalKeyName(k, msg)
			if classic && strings.Contains(canon, " ") {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.sequence_classic"), sanitizeScreenText(k), sanitizeScreenText(short)))
				continue
			}
			if strings.Contains(canon, " ") && actionOnlyHelpOrConfirm(a) {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.sequence_modal"), sanitizeScreenText(k), sanitizeScreenText(short)))
				continue
			}
			if prefix, second, ok := strings.Cut(canon, " "); ok && slices.Contains(prefixKeys, prefix) {
				if letter, has := strings.CutPrefix(second, "ctrl+"); has && len(letter) == 1 && letter[0] >= 'a' && letter[0] <= 'z' {
					notices = append(notices, fmt.Sprintf(cat.T("keymap.ctrl_second"), sanitizeScreenText(k), sanitizeScreenText(short)))
					continue
				}
			}
			if actionWorksWhileTyping(a) && isPrintableKeyName(canon) {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.printable_while_typing"), sanitizeScreenText(k), sanitizeScreenText(short)))
				continue
			}
			name := indexName(canon)
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			valid = append(valid, canon)
		}
		if len(valid) == 0 {
			continue
		}
		candidates = append(candidates, candidate{id: id, short: short, a: a, keys: valid})
	}

	// Fase 3: todas as ações sobrescritas liberam as teclas padrão delas.
	for _, c := range candidates {
		releaseKeys(occupied, c.id, c.a.Modes, current[c.id])
		current[c.id] = nil
	}

	// Prefixo ainda em uso no mapa projetado (restante + overrides candidatos).
	prefixInUse := livePrefixes(current)
	for _, c := range candidates {
		markLivePrefixes(c.keys, prefixInUse)
	}

	var surviving []candidate
	for _, c := range candidates {
		keys, droppedPrefix := filterReservedPrefixKeys(c.keys, prefixInUse)
		for _, k := range droppedPrefix {
			notices = append(notices, fmt.Sprintf(cat.T("keymap.prefix_reserved"), sanitizeScreenText(k), sanitizeScreenText(c.short)))
		}
		if len(keys) == 0 {
			current[c.id] = slices.Clone(defaults[c.id])
			claimKeys(occupied, c.id, c.a.Modes, current[c.id])
			continue
		}
		c.keys = keys
		surviving = append(surviving, c)
	}

	// Fase 4: conflitos no mapa final, com reversão até ponto fixo. Quem volta
	// ao padrão pode colidir com um override sobrevivente ou reativar um
	// prefixo vivo; nesse caso o sobrevivente também reverte.
	conflictNoted := map[[3]string]bool{}
	noteConflict := func(key, short, other string) {
		sig := [3]string{key, short, other}
		if conflictNoted[sig] {
			return
		}
		conflictNoted[sig] = true
		notices = append(notices, fmt.Sprintf(cat.T("keymap.conflict"), sanitizeScreenText(key), sanitizeScreenText(short), sanitizeScreenText(other)))
	}
	prefixNoted := map[[2]string]bool{}
	notePrefix := func(key, short string) {
		sig := [2]string{key, short}
		if prefixNoted[sig] {
			return
		}
		prefixNoted[sig] = true
		notices = append(notices, fmt.Sprintf(cat.T("keymap.prefix_reserved"), sanitizeScreenText(key), sanitizeScreenText(short)))
	}

	for {
		type claim struct {
			id    actionID
			short string
		}
		wanted := map[keyMode]map[string][]claim{}
		for _, c := range surviving {
			for _, mode := range modesInSharedContexts(c.a.Modes) {
				if wanted[mode] == nil {
					wanted[mode] = make(map[string][]claim)
				}
				for _, k := range c.keys {
					name := indexName(k)
					wanted[mode][name] = append(wanted[mode][name], claim{id: c.id, short: c.short})
				}
			}
		}

		conflicted := map[actionID]bool{}
		for _, c := range surviving {
			for _, mode := range modesInSharedContexts(c.a.Modes) {
				for _, k := range c.keys {
					name := indexName(k)
					if name == "ctrl+c" && c.id != actCtrlC {
						noteConflict(k, c.short, shortActionName(actCtrlC))
						conflicted[c.id] = true
					}
					if owner, taken := occupied[mode][name]; taken && owner != c.id {
						noteConflict(k, c.short, shortActionName(owner))
						conflicted[c.id] = true
					}
					for _, other := range wanted[mode][name] {
						if other.id == c.id {
							continue
						}
						noteConflict(k, c.short, other.short)
						conflicted[c.id] = true
						conflicted[other.id] = true
					}
				}
			}
		}

		if len(conflicted) == 0 {
			break
		}

		var next []candidate
		for _, c := range surviving {
			if !conflicted[c.id] {
				next = append(next, c)
				continue
			}
			current[c.id] = slices.Clone(defaults[c.id])
			claimKeys(occupied, c.id, c.a.Modes, current[c.id])
		}
		surviving = next

		prefixInUse = livePrefixes(current)
		for _, c := range surviving {
			markLivePrefixes(c.keys, prefixInUse)
		}

		next = next[:0]
		for _, c := range surviving {
			keys, droppedPrefix := filterReservedPrefixKeys(c.keys, prefixInUse)
			for _, k := range droppedPrefix {
				notePrefix(k, c.short)
			}
			if len(keys) == 0 {
				current[c.id] = slices.Clone(defaults[c.id])
				claimKeys(occupied, c.id, c.a.Modes, current[c.id])
				continue
			}
			c.keys = keys
			next = append(next, c)
		}
		surviving = next
	}

	for _, c := range surviving {
		current[c.id] = slices.Clone(c.keys)
		claimKeys(occupied, c.id, c.a.Modes, c.keys)
		overrides[c.id] = slices.Clone(c.keys)
	}

	if len(overrides) == 0 {
		return nil, notices
	}
	return overrides, notices
}

// filterReservedPrefixKeys tira teclas iguais a um prefixo ainda vivo nas
// sequências. Devolve as teclas aceitas e as recusadas.
func filterReservedPrefixKeys(keys []string, prefixInUse map[string]bool) (kept, dropped []string) {
	for _, k := range keys {
		name := indexName(k)
		if slices.Contains(prefixKeys, name) && prefixInUse[name] {
			dropped = append(dropped, k)
			continue
		}
		kept = append(kept, k)
	}
	return kept, dropped
}

// normalizeRawActionKeys aceita toggle_list e action.toggle_list. A forma curta
// vence quando as duas aparecem e gera aviso.
func normalizeRawActionKeys(raw map[string][]string, cat *i18n.Catalog) (map[string][]string, []string) {
	type forms struct {
		shortVals []string
		fullVals  []string
		hasShort  bool
		hasFull   bool
	}
	byShort := make(map[string]*forms, len(raw))
	for id, vals := range raw {
		short := strings.TrimPrefix(id, "action.")
		if short == "" {
			continue
		}
		f := byShort[short]
		if f == nil {
			f = &forms{}
			byShort[short] = f
		}
		if strings.HasPrefix(id, "action.") && id != short {
			f.fullVals = vals
			f.hasFull = true
			continue
		}
		f.shortVals = vals
		f.hasShort = true
	}
	shorts := make([]string, 0, len(byShort))
	for short := range byShort {
		shorts = append(shorts, short)
	}
	slices.Sort(shorts)

	out := make(map[string][]string, len(shorts))
	var notices []string
	for _, short := range shorts {
		f := byShort[short]
		if f.hasShort && f.hasFull {
			notices = append(notices, fmt.Sprintf(cat.T("keymap.duplicate_action_form"), sanitizeScreenText(short), sanitizeScreenText(short)))
			out[short] = f.shortVals
			continue
		}
		if f.hasShort {
			out[short] = f.shortVals
			continue
		}
		out[short] = f.fullVals
	}
	return out, notices
}

func shortActionName(id actionID) string {
	return strings.TrimPrefix(string(id), "action.")
}

// actionOnlyHelpOrConfirm é verdadeira quando a ação só vive na ajuda ou na
// confirmação. Nesses modos o despacho acontece antes do prefixo, então uma
// sequência nunca dispara.
func actionOnlyHelpOrConfirm(a action) bool {
	if len(a.Modes) == 0 {
		return false
	}
	for _, m := range a.Modes {
		if m != modeHelp && m != modeConfirm {
			return false
		}
	}
	return true
}

// modesInSharedContexts devolve todos os modos que aparecem num keyContext junto
// com algum modo da ação. Assim um conflito entre modeGlobal e modeWindow conta.
func modesInSharedContexts(actionModes []keyMode) []keyMode {
	seen := map[keyMode]bool{}
	var out []keyMode
	for _, ctx := range keyContexts {
		shares := false
		for _, m := range actionModes {
			if slices.Contains(ctx, m) {
				shares = true
				break
			}
		}
		if !shares {
			continue
		}
		for _, m := range ctx {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func livePrefixes(current map[actionID][]string) map[string]bool {
	out := map[string]bool{}
	for _, keys := range current {
		markLivePrefixes(keys, out)
	}
	return out
}

func markLivePrefixes(keys []string, out map[string]bool) {
	for _, k := range keys {
		prefix, _, ok := strings.Cut(indexName(k), " ")
		if ok && slices.Contains(prefixKeys, prefix) {
			out[prefix] = true
		}
	}
}

// EffectiveKeymap monta o registro efetivo da base mais [keys] e os avisos da
// validação. O doctor e os testes usam o mapa e a lista de conflitos.
func EffectiveKeymap(classic bool, raw map[string][]string, cat *i18n.Catalog) (keyRegistry, []string) {
	actions := defaultActions()
	if classic {
		actions = classicActions()
	}
	overrides, notices := resolveKeyOverrides(actions, classic, raw, cat)
	return newKeyRegistry(actions, overrides), notices
}

// KeyBindingLine é uma linha do mapa efetivo para o doctor: ação e teclas.
type KeyBindingLine struct {
	Action string
	Keys   []string
}

// KeymapReport lista o mapa efetivo e os avisos da validação de [keys].
// Action sai no nome curto do config, como toggle_list, sem o prefixo action.
func KeymapReport(classic bool, raw map[string][]string, cat *i18n.Catalog) (bindings []KeyBindingLine, notices []string) {
	reg, notices := EffectiveKeymap(classic, raw, cat)
	for _, a := range reg.actions {
		bindings = append(bindings, KeyBindingLine{
			Action: shortActionName(a.ID),
			Keys:   slices.Clone(reg.keys[a.ID]),
		})
	}
	return bindings, notices
}

func claimKeys(occupied map[keyMode]map[string]actionID, id actionID, modes []keyMode, keys []string) {
	for _, mode := range modes {
		if occupied[mode] == nil {
			occupied[mode] = make(map[string]actionID)
		}
		for _, k := range keys {
			name := indexName(k)
			if _, taken := occupied[mode][name]; !taken {
				occupied[mode][name] = id
			}
		}
	}
}

func releaseKeys(occupied map[keyMode]map[string]actionID, id actionID, modes []keyMode, keys []string) {
	for _, mode := range modes {
		slot := occupied[mode]
		if slot == nil {
			continue
		}
		for _, k := range keys {
			name := indexName(k)
			if slot[name] == id {
				delete(slot, name)
			}
		}
	}
}

// actionWorksWhileTyping diz se a ação entra no despacho com o foco num campo
// de texto. Inclui os modos de escrita e os modos consultados junto deles,
// app, global e window.
func actionWorksWhileTyping(a action) bool {
	for _, m := range a.Modes {
		switch m {
		case modeApp, modeGlobal, modeWindow, modeCompose, modeFilter, modeSearch:
			return true
		}
	}
	return false
}

// isPrintableKeyName é uma tecla que o campo de texto engoliria, um caractere
// sem modificador ou espaço. Sequências com prefixo como g c e ctrl+w h ficam
// de fora, porque o prefixo não entra no campo durante a escrita.
func isPrintableKeyName(name string) bool {
	if prefix, _, ok := strings.Cut(name, " "); ok && slices.Contains(prefixKeys, prefix) {
		return false
	}
	msg, ok := parseKeyName(name)
	if !ok || msg.Alt {
		return false
	}
	switch msg.Type {
	case tea.KeySpace:
		return true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return false
		}
		r := msg.Runes[0]
		return r != '\t' && (unicode.IsPrint(r) || r == ' ')
	default:
		return false
	}
}

func canonicalKeyName(raw string, msg tea.KeyMsg) string {
	raw = strings.TrimSpace(raw)
	if prefix, rest, ok := strings.Cut(raw, " "); ok && slices.Contains(prefixKeys, prefix) {
		second, ok := parseKeyName(rest)
		if !ok {
			return raw
		}
		return prefix + " " + canonicalKeyName(rest, second)
	}
	if name := keyName(msg); name == " " {
		return "space"
	}
	if name := keyName(msg); name != "" {
		return name
	}
	return raw
}

// JoinKeyNotices junta o aviso de migração e os da seção [keys] numa só faixa
// de início. Linhas vazias ficam de fora.
func JoinKeyNotices(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n")
}

// FormatKeyWarnings localiza e sanitiza os avisos estruturados do parser de
// [keys] para a faixa de início e para o doctor.
func FormatKeyWarnings(warnings []config.KeyWarning, cat *i18n.Catalog) []string {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		key := sanitizeScreenText(w.Key)
		var msg string
		switch w.Kind {
		case config.KeyWarningMissingEquals:
			msg = fmt.Sprintf(cat.T("keymap.parse_missing_equals"), w.Line)
		case config.KeyWarningInvalidKey:
			msg = fmt.Sprintf(cat.T("keymap.parse_invalid_key"), w.Line, key)
		case config.KeyWarningEmptyKey:
			msg = fmt.Sprintf(cat.T("keymap.parse_empty_key"), w.Line)
		case config.KeyWarningNotArray:
			msg = fmt.Sprintf(cat.T("keymap.parse_not_array"), w.Line, key)
		case config.KeyWarningMalformedArray:
			msg = fmt.Sprintf(cat.T("keymap.parse_malformed_array"), w.Line, key)
		case config.KeyWarningDuplicateShort:
			msg = fmt.Sprintf(cat.T("keymap.parse_duplicate_short"), w.Line, key)
		case config.KeyWarningDuplicateForm:
			msg = fmt.Sprintf(cat.T("keymap.parse_duplicate_form"), w.Line, key)
		default:
			msg = fmt.Sprintf(cat.T("keymap.parse_invalid_key"), w.Line, key)
		}
		out = append(out, termtext.Line(msg))
	}
	return out
}
