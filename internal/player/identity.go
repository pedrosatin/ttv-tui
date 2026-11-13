package player

import (
	"fmt"
	"strings"
)

// ipcServerFlag é o argumento que o mpv recebe com o endpoint da sessão.
const ipcServerFlag = "--input-ipc-server="

// argvCarriesEndpoint diz se um argv é de um processo que esta sessão iniciou
// com endpoint: o mpv, que tem o argumento exato --input-ipc-server=<endpoint>,
// ou o streamlink, que tem esse mesmo argumento como palavra do valor de
// --player-args, depois do shlex. O endpoint em outro argumento (--title, um
// caminho de arquivo, um texto qualquer) não conta: um processo alheio pode
// carregá-lo sem ser o player.
func argvCarriesEndpoint(argv []string, endpoint string) bool {
	if endpoint == "" {
		return false
	}
	want := ipcServerFlag + endpoint
	for i, arg := range argv {
		if arg == want {
			return true
		}
		value, ok := playerArgsValue(argv, i)
		if !ok {
			continue
		}
		words, err := posixShlexSplit(value)
		if err != nil {
			continue
		}
		for _, w := range words {
			if w == want {
				return true
			}
		}
	}
	return false
}

// playerArgsValue devolve o valor de --player-args quando argv[i] abre essa
// opção, nas duas formas que o argparse do streamlink aceita.
func playerArgsValue(argv []string, i int) (string, bool) {
	const flag = "--player-args"
	switch arg := argv[i]; {
	case arg == flag && i+1 < len(argv):
		return argv[i+1], true
	case strings.HasPrefix(arg, flag+"="):
		return arg[len(flag)+1:], true
	}
	return "", false
}

// posixShlexSplit mirrors Python's shlex.split(s) (posix=True, comments=False),
// which is what streamlink applies to --player-args.
func posixShlexSplit(s string) ([]string, error) {
	var (
		out    []string
		cur    []byte
		inWord bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\r', '\n':
			if inWord {
				out = append(out, string(cur))
				cur, inWord = nil, false
			}
		case '\'':
			inWord = true
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("no closing quotation")
			}
			cur = append(cur, s[i+1:i+1+end]...)
			i += end + 1
		case '"':
			inWord = true
			closed := false
			for i++; i < len(s); i++ {
				d := s[i]
				if d == '"' {
					closed = true
					break
				}
				if d == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					i++
					d = s[i]
				}
				cur = append(cur, d)
			}
			if !closed {
				return nil, fmt.Errorf("no closing quotation")
			}
		case '\\':
			inWord = true
			if i+1 >= len(s) {
				return nil, fmt.Errorf("no escaped character")
			}
			i++
			cur = append(cur, s[i])
		default:
			inWord = true
			cur = append(cur, c)
		}
	}
	if inWord {
		out = append(out, string(cur))
	}
	return out, nil
}
