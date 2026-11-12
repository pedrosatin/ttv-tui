//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// conptyChildEnv faz o TestConPTYChild agir como a TUI que o startPTY abre.
const conptyChildEnv = "TTV_TEST_CONPTY_CHILD"

// A linha sai sem espaços: o conhost pode trocar espaços por saltos do cursor.
var conptyCheckLine = regexp.MustCompile(`conpty-check:stdin=([^,\s]+),stdout=([^;\s]+);`)

// O go test redireciona os handles deste processo, como o ttv-tui faz com os da
// janela. Mesmo assim o filho precisa receber o console do pseudoconsole: o Bubble
// Tea chama GetConsoleMode no stdin, e só pelo console a saída chega à janela.
func TestPTYGivesTheChildTheConsole(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestConPTYChild$")
	cmd.Env = append(os.Environ(), conptyChildEnv+"=1")
	p, err := startPTY(cmd, 120, 30, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(exited) }()
	t.Cleanup(func() {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
		}
		closed := make(chan struct{})
		go func() { _ = p.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Error("o pseudoconsole não fechou em 10 s")
		}
	})

	// A leitura continua até o fim: em builds antigos o ClosePseudoConsole espera
	// a última saída ser lida.
	result := make(chan []string, 1)
	go func() {
		var seen strings.Builder
		sent := false
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if !sent {
				seen.Write(buf[:n])
				if m := conptyCheckLine.FindStringSubmatch(seen.String()); m != nil {
					result <- m
					sent = true
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case m := <-result:
		if m[1] != "ok" || m[2] != "ok" {
			t.Fatalf("o filho não recebeu o console: stdin=%s stdout=%s", m[1], m[2])
		}
	case <-time.After(15 * time.Second):
		t.Fatal("em 15 s nada chegou pelo pseudoconsole: o filho escreveu fora dele")
	}
}

// TestConPTYChild só roda como o filho do teste acima.
func TestConPTYChild(t *testing.T) {
	if os.Getenv(conptyChildEnv) != "1" {
		t.Skip("só roda dentro do pseudoconsole")
	}
	fmt.Printf("conpty-check:stdin=%s,stdout=%s;\n",
		consoleCheck(windows.STD_INPUT_HANDLE), consoleCheck(windows.STD_OUTPUT_HANDLE))
}

// consoleCheck repete o que o Bubble Tea faz no stdin: GetStdHandle e GetConsoleMode.
func consoleCheck(std uint32) string {
	h, err := windows.GetStdHandle(std)
	if err == nil {
		var mode uint32
		err = windows.GetConsoleMode(h, &mode)
	}
	if err != nil {
		return "falha(" + strings.Join(strings.Fields(err.Error()), "_") + ")"
	}
	return "ok"
}
