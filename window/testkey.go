//go:build windowtest

package main

import "strings"

// testKey é uma tecla do comando key do socket de teste: um nome com
// modificadores na frente, em qualquer ordem, como ctrl+alt+Right ou shift+Tab.
type testKey struct {
	name             string
	ctrl, alt, shift bool
}

// parseTestKey lê "ctrl+alt+Right". O nome que sobra não pode ser vazio.
func parseTestKey(spec string) (testKey, bool) {
	var k testKey
	rest := strings.TrimSpace(spec)
	for {
		mod, after, found := strings.Cut(rest, "+")
		if !found || after == "" {
			break
		}
		switch strings.ToLower(mod) {
		case "ctrl":
			k.ctrl = true
		case "alt":
			k.alt = true
		case "shift":
			k.shift = true
		default:
			return testKey{}, false
		}
		rest = after
	}
	// Um "+" que sobra no nome é modificador sem tecla, como em "ctrl+".
	if rest == "" || strings.ContainsAny(rest, " \t") || (rest != "+" && strings.Contains(rest, "+")) {
		return testKey{}, false
	}
	k.name = rest
	return k, true
}

// text devolve o que a tecla digita quando ela é texto: um caractere visível, ou
// Space, sem Ctrl nem Alt. No SDL o texto chega por SDL_EVENT_TEXT_INPUT, e uma
// tecla dessas pelo SDL_EVENT_KEY_DOWN não escreve nada.
func (k testKey) text() (string, bool) {
	if k.ctrl || k.alt {
		return "", false
	}
	if k.name == "Space" {
		return " ", true
	}
	r := []rune(k.name)
	if len(r) != 1 || r[0] < 0x20 || r[0] == 0x7f {
		return "", false
	}
	if k.shift {
		return strings.ToUpper(k.name), true
	}
	return k.name, true
}
