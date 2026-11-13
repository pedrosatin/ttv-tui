package platform

import (
	"strings"
	"unicode"
)

// Tamanhos máximos dos identificadores, pela gramática de cada plataforma.
const (
	maxTwitchLogin = 25
	maxKickSlug    = 64
)

// ValidTwitchLogin informa se s é um login da Twitch: de 1 a 25 caracteres
// entre a-z, 0-9 e _. A checagem vale para o valor já decodificado e não
// normaliza: maiúscula, espaço e qualquer outro byte reprovam.
func ValidTwitchLogin(s string) bool {
	return validIdentifier(s, maxTwitchLogin, false)
}

// ValidKickSlug informa se s é o slug de um canal da Kick: de 1 a 64
// caracteres entre a-z, 0-9, _ e -. Ponto não entra, de modo que "." e ".."
// nunca viram um segmento de caminho. Como em ValidTwitchLogin, o valor é o
// decodificado e nada é normalizado.
func ValidKickSlug(s string) bool {
	return validIdentifier(s, maxKickSlug, true)
}

// ValidIdentifier informa se s segue a gramática do identificador de canal de
// plat. Plataforma sem gramática própria aceita qualquer texto não vazio sem
// caractere de controle.
func ValidIdentifier(plat, s string) bool {
	switch Normalize(plat) {
	case Twitch:
		return ValidTwitchLogin(s)
	case Kick:
		return ValidKickSlug(s)
	default:
		return s != "" && !HasControl(s)
	}
}

// LowerASCII troca só as maiúsculas ASCII e deixa o resto como está, para que
// a gramática veja o texto original fora do ASCII. strings.ToLower levaria o
// sinal Kelvin (U+212A) a "k" e faria um caractere de fora passar.
func LowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// HasControl informa se s tem caractere de controle (C0, DEL ou C1), o que
// inclui CR, LF, NUL e ESC.
func HasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

func validIdentifier(s string, maxLen int, hyphen bool) bool {
	if s == "" || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
		case c == '-' && hyphen:
		default:
			return false
		}
	}
	return true
}
