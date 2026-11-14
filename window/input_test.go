package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestInputWriterPreservesBytes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	a := &app{ptmx: mockPTYDevice{w}, inputq: make(chan []byte, 2)}
	done := make(chan struct{})
	go func() { a.pumpInput(); close(done) }()
	b := []byte("abc")
	a.writeInput(b)
	b[0] = 'x'
	a.writeInput([]byte("\x1b[A"))
	got := make([]byte, 6)
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc\x1b[A" {
		t.Fatalf("entrada = %q", got)
	}
	close(a.inputq)
	<-done
}

func TestEndedNotificationDoesNotHoldSessionOnFullQueue(t *testing.T) {
	a := &app{ctlq: make(chan ctlMsg, 1)}
	a.ctlq <- ctlMsg{kind: "layout"}
	s := &session{stopping: make(chan struct{}), done: make(chan struct{})}
	entered := make(chan struct{})
	go func() {
		defer close(s.done)
		close(entered)
		a.notifyEnded(s)
	}()
	<-entered
	close(s.stopping)
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("sessão ficou presa na fila de controle")
	}
}

func TestWaitTUIAllowsCleanup(t *testing.T) {
	done := make(chan struct{})
	entered := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		close(entered)
		waitTUI(done, tuiStopWait, func() { t.Error("matou a TUI antes da limpeza") })
		close(returned)
	}()
	<-entered
	close(done)
	<-returned
}

func TestWaitTUIKillsAndReapsAfterDeadline(t *testing.T) {
	done := make(chan struct{})
	killed := false
	waitTUI(done, 0, func() { killed = true; close(done) })
	if !killed {
		t.Fatal("TUI não foi encerrada ao vencer o prazo")
	}
}

// As teclas imprimíveis chegam por TEXT_INPUT; as demais passam por keyBytes.
// As teclas são as padrão do registro de ações da TUI (o campo Default).
func TestBoundKeysHaveWindowTranslation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/ui/actions.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{"enter": "Return", "esc": "Escape", "tab": "Tab", "up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home", "end": "End", "pgup": "PageUp", "pgdown": "PageDown", "space": " "}
	seen := map[string]bool{}
	defer func() {
		if len(seen) == 0 {
			t.Fatal("nenhuma tecla padrão encontrada no registro de ações")
		}
	}()
	ast.Inspect(file, func(n ast.Node) bool {
		field, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if name, ok := field.Key.(*ast.Ident); !ok || name.Name != "Default" {
			return true
		}
		keys, ok := field.Value.(*ast.CompositeLit)
		if !ok {
			t.Fatal("teclas padrão fora de uma lista literal")
		}
		for _, arg := range keys.Elts {
			lit, ok := arg.(*ast.BasicLit)
			if !ok {
				t.Fatal("binding não literal")
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			t.Run(name, func(t *testing.T) {
				base := name
				// SDL_KMOD_LCTRL e SDL_KMOD_LALT.
				mod := keyModifier(keyBytes, 0)
				if strings.HasPrefix(base, "ctrl+") {
					mod = 0x40
					base = strings.TrimPrefix(base, "ctrl+")
				}
				if strings.HasPrefix(base, "alt+") {
					mod = 0x100
					base = strings.TrimPrefix(base, "alt+")
				}
				if alias, ok := aliases[base]; ok {
					base = alias
				}
				if mod == 0 && utf8.RuneCountInString(base) == 1 {
					return
				}
				k, ok := keyNames[base]
				if !ok {
					k, ok = keyNames[strings.ToUpper(base)]
				}
				if !ok && len(base) == 1 {
					k, ok = asciiKey(base[0], keyNames["Return"]), true
				}
				b := keyBytes(k, mod)
				if !ok || len(b) == 0 {
					t.Errorf("tecla ligada na TUI sem tradução em keyBytes: %s", name)
				}
			})
		}
		return true
	})
}

func asciiKey[T ~uint32](b byte, _ T) T { return T(b) }

// Modificadores na ordem do parâmetro do xterm: o índice mais 1 é o parâmetro.
var xtermMods = [...]struct {
	name             string
	shift, alt, ctrl bool
}{
	{"", false, false, false},
	{"shift+", true, false, false},
	{"alt+", false, true, false},
	{"shift+alt+", true, true, false},
	{"ctrl+", false, false, true},
	{"ctrl+shift+", true, false, true},
	{"ctrl+alt+", false, true, true},
	{"ctrl+shift+alt+", true, true, true},
}

// Os dois lados do teclado: SDL_KMOD_LSHIFT, LALT e LCTRL; RSHIFT, RALT e RCTRL.
var modSides = [...]struct {
	name             string
	shift, alt, ctrl uint16
}{
	{"esquerdo", 0x1, 0x100, 0x40},
	{"direito", 0x2, 0x200, 0x80},
}

func TestKeyBytesWithModifiers(t *testing.T) {
	csi := func(c string) func(int) string {
		return func(m int) string {
			if m == 1 {
				return "\x1b[" + c
			}
			return "\x1b[1;" + strconv.Itoa(m) + c
		}
	}
	tilde := func(n int) func(int) string {
		return func(m int) string {
			if m == 1 {
				return "\x1b[" + strconv.Itoa(n) + "~"
			}
			return "\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(m) + "~"
		}
	}
	// Bubble Tea 1.3.10 reconhece só Alt+F1 a F12 e Shift+F1 a F8; nas demais
	// combinações a janela não manda nada.
	fn := func(i int, seq func(int) string) func(int) string {
		return func(m int) string {
			switch {
			case m == 1 && i < 4:
				return "\x1bO" + string(byte('P'+i))
			case m == 3, m == 2 && i < 8, m == 1:
				return seq(m)
			}
			return ""
		}
	}
	keys := []struct {
		name string
		want func(m int) string
	}{
		{"Up", csi("A")}, {"Down", csi("B")}, {"Right", csi("C")}, {"Left", csi("D")},
		{"Home", csi("H")}, {"End", csi("F")},
		{"PageUp", tilde(5)}, {"PageDown", tilde(6)},
	}
	for i, n := range []int{0, 0, 0, 0, 15, 17, 18, 19, 20, 21, 23, 24} {
		seq := csi(string(byte('P' + i)))
		if i >= 4 {
			seq = tilde(n)
		}
		keys = append(keys, struct {
			name string
			want func(m int) string
		}{"F" + strconv.Itoa(i+1), fn(i, seq)})
	}
	for _, k := range keys {
		for p, mods := range xtermMods {
			for _, side := range modSides {
				var mod uint16
				if mods.shift {
					mod |= side.shift
				}
				if mods.alt {
					mod |= side.alt
				}
				if mods.ctrl {
					mod |= side.ctrl
				}
				want := k.want(p + 1)
				t.Run(mods.name+k.name+"/"+side.name, func(t *testing.T) {
					got := keyBytes(keyNames[k.name], keyModifier(keyBytes, mod))
					if string(got) != want || want == "" && got != nil {
						t.Errorf("bytes = %q, want %q", got, want)
					}
				})
			}
		}
	}
}

// Teclas que mudaram de bytes ou que não podem mudar: Ctrl+Backspace não é
// Ctrl+H, e o Ctrl+H de verdade continua 0x08.
func TestKeyBytesEditingAndControl(t *testing.T) {
	const lshift, lalt, lctrl, ralt, rctrl = 0x1, 0x100, 0x40, 0x200, 0x80
	cases := []struct {
		name string
		key  string
		mod  uint16
		want string
	}{
		{"Backspace", "Backspace", 0, "\x7f"},
		{"Shift+Backspace", "Backspace", lshift, "\x7f"},
		{"Ctrl+Backspace apaga a palavra", "Backspace", lctrl, "\x1b\x7f"},
		{"Ctrl direito+Backspace", "Backspace", rctrl, "\x1b\x7f"},
		{"Alt+Backspace", "Backspace", lalt, "\x1b\x7f"},
		{"Ctrl+Alt+Backspace", "Backspace", lctrl | lalt, "\x1b\x7f"},
		{"Ctrl+H segue Ctrl+H", "h", lctrl, "\x08"},
		{"Ctrl direito+H", "h", rctrl, "\x08"},
		{"Ctrl+Shift+H", "h", lctrl | lshift, "\x08"},
		{"Ctrl+Alt+H leva o Alt", "h", lctrl | lalt, "\x1b\x08"},
		{"Ctrl+Alt direito+W", "w", lctrl | ralt, "\x1b\x17"},
		{"Alt+H", "h", lalt, "\x1bh"},
		{"Ctrl+Alt+→ não é Ctrl+→", "Right", lctrl | lalt, "\x1b[1;7C"},
		{"Ctrl+→", "Right", lctrl, "\x1b[1;5C"},
		{"Alt+←", "Left", lalt, "\x1b[1;3D"},
		{"Shift+Tab", "Tab", lshift, "\x1b[Z"},
		{"Delete", "Delete", 0, "\x1b[3~"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, ok := keyNames[c.key]
			if !ok {
				k = asciiKey(c.key[0], keyNames["Return"])
			}
			if got := string(keyBytes(k, keyModifier(keyBytes, c.mod))); got != c.want {
				t.Fatalf("bytes = %q, want %q", got, c.want)
			}
		})
	}
}

func keyModifier[K ~uint32, M ~uint16](_ func(K, M) []byte, mod uint16) M {
	return M(mod)
}
