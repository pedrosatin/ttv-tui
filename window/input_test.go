package main

import (
	"bytes"
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

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

func TestWriteInputDoesNotWaitForReader(t *testing.T) {
	w, r, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := term.MakeRaw(r.Fd()); err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	a := &app{ptmx: w, inputq: make(chan []byte, 1)}
	done := make(chan struct{})
	go func() {
		a.inputq <- []byte("fila cheia")
		a.writeInput(bytes.Repeat([]byte("x"), 1024*1024))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		// Drena o PTY para liberar a escrita síncrona da versão anterior.
		_, _ = io.CopyN(io.Discard, r, 1024*1024)
		<-done
		t.Fatal("writeInput bloqueou sem leitor no PTY")
	}
}

func TestInputWriterPreservesBytes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	a := &app{ptmx: w, inputq: make(chan []byte, 2)}
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
func TestBoundKeysHaveWindowTranslation(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/ui/keys.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	aliases := map[string]string{"enter": "Return", "esc": "Escape", "tab": "Tab", "up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home", "end": "End", "pgup": "PageUp", "pgdown": "PageDown", "space": " "}
	seen := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WithKeys" {
			return true
		}
		for _, arg := range call.Args {
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
				mod := uint16(0)
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
				b := keyBytes(k, 0)
				if mod == 0x40 {
					b = keyBytes(k, 0x40)
				}
				if mod == 0x100 {
					b = keyBytes(k, 0x100)
				}
				if !ok || len(b) == 0 {
					t.Errorf("tecla ligada na TUI sem tradução em keyBytes: %s", name)
				}
			})
		}
		return true
	})
}

// O tipo do keycode vem do cgo, que não pode ser importado em _test.go.
func asciiKey[T ~uint32](b byte, _ T) T { return T(b) }

func TestFunctionKeyBytes(t *testing.T) {
	for i, want := range []string{"\x1bOP", "\x1bOQ", "\x1bOR", "\x1bOS", "\x1b[15~", "\x1b[17~", "\x1b[18~", "\x1b[19~", "\x1b[20~", "\x1b[21~", "\x1b[23~", "\x1b[24~"} {
		name := "F" + strconv.Itoa(i+1)
		t.Run(name, func(t *testing.T) {
			if got := string(keyBytes(keyNames[name], 0)); got != want {
				t.Fatalf("bytes = %q, want %q", got, want)
			}
		})
	}
}
