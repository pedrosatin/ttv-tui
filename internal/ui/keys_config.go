package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// ResolveKeyOverrides valida o mapa [keys] do config sobre a base (default ou
// classic) e devolve os overrides aceitos e os avisos localizados. Ação
// desconhecida, tecla inválida, conflito no mesmo modo e tecla imprimível em
// ação que vale na escrita geram aviso; nesses casos a ação fica com o padrão
// (ou, na lista vazia explícita, desliga).
func ResolveKeyOverrides(classic bool, raw map[string][]string, cat *i18n.Catalog) (map[actionID][]string, []string) {
	if len(raw) == 0 {
		return nil, nil
	}
	actions := defaultActions()
	if classic {
		actions = classicActions()
	}
	return resolveKeyOverrides(actions, raw, cat)
}

func resolveKeyOverrides(actions []action, raw map[string][]string, cat *i18n.Catalog) (map[actionID][]string, []string) {
	byID := make(map[actionID]action, len(actions))
	occupied := make(map[keyMode]map[string]actionID)
	current := make(map[actionID][]string, len(actions))
	for _, a := range actions {
		byID[a.ID] = a
		current[a.ID] = slices.Clone(a.Default)
		claimKeys(occupied, a.ID, a.Modes, a.Default)
	}

	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	overrides := make(map[actionID][]string)
	var notices []string
	for _, idStr := range ids {
		id := actionID(idStr)
		a, ok := byID[id]
		if !ok {
			notices = append(notices, fmt.Sprintf(cat.T("keymap.unknown_action"), idStr))
			continue
		}
		want := raw[idStr]
		if len(want) == 0 {
			releaseKeys(occupied, id, a.Modes, current[id])
			current[id] = nil
			overrides[id] = []string{}
			continue
		}

		var valid []string
		seen := map[string]struct{}{}
		for _, k := range want {
			msg, ok := parseKeyName(k)
			if !ok {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.invalid_key"), k, idStr))
				continue
			}
			canon := canonicalKeyName(k, msg)
			if actionWorksWhileTyping(a) && isPrintableKeyName(canon) {
				notices = append(notices, fmt.Sprintf(cat.T("keymap.printable_while_typing"), k, idStr))
				continue
			}
			name := indexName(canon)
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			valid = append(valid, canon)
		}
		// Sem tecla válida restante, a ação fica com o padrão (lista vazia
		// explícita já saiu acima e desliga de propósito).
		if len(valid) == 0 {
			continue
		}

		releaseKeys(occupied, id, a.Modes, current[id])
		conflict := false
		for _, mode := range a.Modes {
			for _, k := range valid {
				name := indexName(k)
				if owner, taken := occupied[mode][name]; taken && owner != id {
					notices = append(notices, fmt.Sprintf(cat.T("keymap.conflict"), k, idStr, owner))
					conflict = true
				}
			}
		}
		if conflict {
			claimKeys(occupied, id, a.Modes, current[id])
			continue
		}
		claimKeys(occupied, id, a.Modes, valid)
		current[id] = slices.Clone(valid)
		overrides[id] = slices.Clone(valid)
	}
	if len(overrides) == 0 {
		return nil, notices
	}
	return overrides, notices
}

// EffectiveKeymap monta o registro efetivo (base + [keys]) e os avisos da
// validação. O doctor e os testes usam o mapa e a lista de conflitos.
func EffectiveKeymap(classic bool, raw map[string][]string, cat *i18n.Catalog) (keyRegistry, []string) {
	actions := defaultActions()
	if classic {
		actions = classicActions()
	}
	overrides, notices := resolveKeyOverrides(actions, raw, cat)
	return newKeyRegistry(actions, overrides), notices
}

// KeyBindingLine é uma linha do mapa efetivo para o doctor: ação e teclas.
type KeyBindingLine struct {
	Action string
	Keys   []string
}

// KeymapReport lista o mapa efetivo e os avisos da validação de [keys].
func KeymapReport(classic bool, raw map[string][]string, cat *i18n.Catalog) (bindings []KeyBindingLine, notices []string) {
	reg, notices := EffectiveKeymap(classic, raw, cat)
	for _, a := range reg.actions {
		bindings = append(bindings, KeyBindingLine{
			Action: string(a.ID),
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
// de texto: modos de escrita e os modos consultados junto deles (app, global,
// window).
func actionWorksWhileTyping(a action) bool {
	for _, m := range a.Modes {
		switch m {
		case modeApp, modeGlobal, modeWindow, modeCompose, modeFilter, modeSearch:
			return true
		}
	}
	return false
}

// isPrintableKeyName é uma tecla que o campo de texto engoliria: um caractere
// sem modificador, espaço, ou sequência que começa assim (g c).
func isPrintableKeyName(name string) bool {
	if prefix, _, ok := strings.Cut(name, " "); ok && slices.Contains(prefixKeys, prefix) {
		return isPrintableKeyName(prefix)
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
			// Sequência sintética ("g c"): o primeiro pedaço decide.
			if parts := strings.SplitN(string(msg.Runes), " ", 2); len(parts) == 2 {
				return isPrintableKeyName(parts[0])
			}
			return false
		}
		r := msg.Runes[0]
		return r != '\t' && (unicode.IsPrint(r) || r == ' ')
	default:
		return false
	}
}

func canonicalKeyName(raw string, msg tea.KeyMsg) string {
	if name := keyName(msg); name == " " {
		return "space"
	}
	if name := keyName(msg); name != "" {
		return name
	}
	return raw
}

// JoinKeyNotices junta o aviso de migração e os da seção [keys] numa só faixa
// de início. Linhas vazias saem fora.
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
