package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// keyTypesByName inverte os nomes que o bubbletea dá às teclas em
// tea.KeyMsg.String(): "enter", "ctrl+f", "pgdown", "shift+tab"... Sai da própria
// biblioteca, para que a notação acompanhe a versão do bubbletea.
var keyTypesByName = func() map[string]tea.KeyType {
	names := make(map[string]tea.KeyType)
	// As teclas de controle vão de 0 a 127 e as especiais são negativas,
	// começando em KeyRunes (-1). A faixa cobre as duas com folga.
	for t := tea.KeyType(-256); t <= 127; t++ {
		name := t.String()
		if name == "" || t == tea.KeyRunes {
			continue
		}
		if _, dup := names[name]; dup {
			continue
		}
		names[name] = t
	}
	// KeySpace se chama " ", que não sobrevive a uma notação separada por
	// espaços. "space" é o nome que se escreve.
	delete(names, " ")
	names["space"] = tea.KeySpace
	return names
}()

// parseKeyName converte um nome de tecla no formato de tea.KeyMsg.String() na
// mensagem que o terminal entregaria: "ctrl+f", "alt+up", "enter", "pgdown",
// "space" ou um caractere só ("g", "?", "alt+j"). O mapa de teclas do config usa
// o mesmo formato. ok é falso para um nome que não é tecla; quem mostra o erro
// ao usuário escreve a mensagem pelo catálogo de i18n.
func parseKeyName(name string) (tea.KeyMsg, bool) {
	if t, ok := keyTypesByName[name]; ok {
		return keyMsgOf(t, false), true
	}
	alt := false
	rest := name
	if after, ok := strings.CutPrefix(name, "alt+"); ok && after != "" {
		alt, rest = true, after
		if t, ok := keyTypesByName[rest]; ok {
			return keyMsgOf(t, true), true
		}
	}
	if utf8.RuneCountInString(rest) == 1 {
		r, _ := utf8.DecodeRuneInString(rest)
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}

// keyMsgOf monta a mensagem de uma tecla com nome. O espaço chega com a runa,
// como no terminal.
func keyMsgOf(t tea.KeyType, alt bool) tea.KeyMsg {
	if t == tea.KeySpace {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}
	}
	return tea.KeyMsg{Type: t, Alt: alt}
}

// keyName é o nome de msg no formato de tea.KeyMsg.String(), o mesmo do registro
// de ações. Alguns terminais mandam o Tab como a runa '\t'; ela se chama "tab".
func keyName(msg tea.KeyMsg) string {
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '\t' {
		return tea.KeyTab.String()
	}
	return msg.String()
}

// keyModifiers são os prefixos que o bubbletea põe no nome de uma tecla, na
// ordem em que ele os escreve: "alt+ctrl+left", "ctrl+shift+up".
var keyModifiers = []string{"alt+", "ctrl+", "shift+"}

// keySymbols são as teclas que a tela mostra por símbolo ou abreviação.
var keySymbols = map[string]string{
	"up":     "↑",
	"down":   "↓",
	"left":   "←",
	"right":  "→",
	"pgup":   "PgUp",
	"pgdown": "PgDn",
}

// keyLabel é o nome de tecla como a tela o mostra: "ctrl+q" vira Ctrl+Q,
// "alt+up" vira Alt+↑, "pgdown" vira PgDn e "enter" vira Enter. Um caractere sai
// como é, e o espaço, único nome traduzido, vem do catálogo.
func keyLabel(name string, cat *i18n.Catalog) string {
	if name == " " || name == "space" {
		return cat.T("key.space")
	}
	var b strings.Builder
	rest, ctrl := name, false
	for _, mod := range keyModifiers {
		if after, ok := strings.CutPrefix(rest, mod); ok && after != "" {
			b.WriteString(capitalize(mod))
			ctrl = ctrl || mod == "ctrl+"
			rest = after
		}
	}
	switch {
	case keySymbols[rest] != "":
		b.WriteString(keySymbols[rest])
	case utf8.RuneCountInString(rest) == 1 && ctrl:
		// Ctrl não distingue maiúscula: Ctrl+Q, como no teclado.
		b.WriteString(strings.ToUpper(rest))
	case utf8.RuneCountInString(rest) == 1:
		b.WriteString(rest)
	default:
		b.WriteString(capitalize(rest))
	}
	return b.String()
}

// capitalize põe em maiúscula a primeira letra de um nome de tecla.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
