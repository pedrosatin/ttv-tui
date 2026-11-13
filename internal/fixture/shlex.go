package fixture

import (
	"errors"
	"strings"
)

// SplitArgs separa uma linha de argumentos como o shlex.split do Python (modo
// POSIX), que é como o streamlink lê --player-args. Basta para o que
// internal/player escreve: palavras soltas, aspas simples, aspas duplas e barra
// invertida fora de aspas simples.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inWord  bool
		quote   byte
		escaped bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case quote == '"':
			switch {
			case c == '"':
				quote = 0
			case c == '\\' && i+1 < len(s) && strings.IndexByte("\\\"$`", s[i+1]) >= 0:
				i++
				cur.WriteByte(s[i])
			default:
				cur.WriteByte(c)
			}
		case c == '\\':
			escaped, inWord = true, true
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("aspas ou escape sem fim nos argumentos do player")
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
