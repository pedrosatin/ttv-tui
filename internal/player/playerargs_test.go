package player

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

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

var playerArgsCases = []struct {
	name string
	argv []string
}{
	{"caminho simples", []string{"--no-config", "--input-ipc-server=/run/user/1000/ttv-tui/1/i"}},
	{"caminho com espaço", []string{"--input-ipc-server=/home/a b/x/i", "--input-conf=/home/a b/x/mpv-input.conf"}},
	{"caminho Windows", []string{`--input-conf=C:\Users\Joao\AppData\Local\ttv-tui\1\mpv-input.conf`}},
	{"caminho Windows com espaço", []string{`--input-conf=C:\Users\João Silva\ttv\mpv-input.conf`}},
	{"pipe nomeado", []string{`--input-ipc-server=\\.\pipe\ttv-1`}},
	{"aspas simples", []string{"--input-ipc-server=/home/o'brien/i", "it's"}},
	{"aspas duplas e metacaracteres", []string{`a"b`, `$HOME;ls|x`, "tab\there", "{input}"}},
	{"argumento vazio", []string{"--a", "", "--b"}},
}

func TestBuildPlayerArgsRoundTrip(t *testing.T) {
	for _, tc := range playerArgsCases {
		t.Run(tc.name, func(t *testing.T) {
			quoted := buildPlayerArgs(tc.argv)
			got, err := posixShlexSplit(quoted)
			if err != nil {
				t.Fatalf("split(%q): %v", quoted, err)
			}
			if !reflect.DeepEqual(got, tc.argv) {
				t.Fatalf("round trip of %q\n got %q\nwant %q", quoted, got, tc.argv)
			}
		})
	}
}

func TestBuildPlayerArgsCommonCaseStaysReadable(t *testing.T) {
	argv := []string{"--no-config", "--volume-max=100", "--input-ipc-server=/run/user/1000/ttv-tui/1/i"}
	want := "--no-config --volume-max=100 --input-ipc-server=/run/user/1000/ttv-tui/1/i"
	if got := buildPlayerArgs(argv); got != want {
		t.Fatalf("buildPlayerArgs = %q, want %q", got, want)
	}
}

func TestQuotePlayerArgForms(t *testing.T) {
	for in, want := range map[string]string{
		"":                         "''",
		"/home/a b/x/i":            "'/home/a b/x/i'",
		`\\.\pipe\ttv-1`:           `'\\.\pipe\ttv-1'`,
		"it's":                     `'it'\''s'`,
		"--input-ipc-server=/a/b":  "--input-ipc-server=/a/b",
		"--input-ipc-server=/a b/": "'--input-ipc-server=/a b/'",
	} {
		if got := quotePlayerArg(in); got != want {
			t.Errorf("quotePlayerArg(%q) = %q, want %q", in, got, want)
		}
	}
}

// A socket path with a space must reach mpv as a single argument.
func TestDefaultCommandPlayerArgsSocketWithSpace(t *testing.T) {
	socket := "/home/a b/x/i"
	cmd := DefaultCommand("gaules", socket)
	var playerArgs string
	for i, a := range cmd.Args {
		if a == "--player-args" && i+1 < len(cmd.Args) {
			playerArgs = cmd.Args[i+1]
		}
	}
	if playerArgs == "" {
		t.Fatalf("no --player-args in %q", cmd.Args)
	}
	t.Logf("--player-args = %s", playerArgs)
	got, err := posixShlexSplit(playerArgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--input-ipc-server=/home/a b/x/i",
		"--input-conf=/home/a b/x/mpv-input.conf",
	} {
		found := false
		for _, g := range got {
			found = found || g == want
		}
		if !found {
			t.Fatalf("argv %q missing %q (string %q)", got, want, playerArgs)
		}
	}
}

// Cross-checks the Go implementation against the real Python shlex.split.
func TestBuildPlayerArgsAgainstPythonShlex(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	if out, err := exec.Command(py, "-I", "-c", "import shlex").CombinedOutput(); err != nil {
		t.Skipf("python3 shlex unavailable: %v: %s", err, out)
	}
	const script = "import shlex,sys,json;print(json.dumps(shlex.split(sys.argv[1])))"
	for _, name := range []string{"caminho com espaço", "caminho Windows", "pipe nomeado", "aspas simples"} {
		for _, tc := range playerArgsCases {
			if tc.name != name {
				continue
			}
			t.Run(tc.name, func(t *testing.T) {
				quoted := buildPlayerArgs(tc.argv)
				out, err := exec.Command(py, "-I", "-c", script, quoted).Output()
				if err != nil {
					t.Fatalf("python shlex.split(%q): %v", quoted, err)
				}
				want := mustJSONStrings(t, tc.argv)
				if strings.TrimSpace(string(out)) != want {
					t.Fatalf("python got %s want %s (string %q)", strings.TrimSpace(string(out)), want, quoted)
				}
			})
		}
	}
}

// mustJSONStrings renders argv like Python's json.dumps(ensure_ascii=True).
func mustJSONStrings(t *testing.T, argv []string) string {
	t.Helper()
	parts := make([]string, len(argv))
	for i, a := range argv {
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range a {
			switch {
			case r == '"' || r == '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case r == '\t':
				b.WriteString(`\t`)
			case r < 0x20:
				fmt.Fprintf(&b, `\u%04x`, r)
			case r > 0x7e:
				fmt.Fprintf(&b, `\u%04x`, r)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		parts[i] = b.String()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
