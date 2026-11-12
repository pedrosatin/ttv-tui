package main

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// pumpChunks passa cada trecho ao pumpPTY numa leitura própria.
func pumpChunks(t *testing.T, cols int, chunks ...string) *vt.SafeEmulator {
	t.Helper()
	r, w := seqpacketPair(t)
	for _, c := range chunks {
		if _, err := io.WriteString(w, c); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	a := &app{term: vt.NewSafeEmulator(cols, 2), ptmx: r}
	t.Cleanup(func() { _ = a.term.Close() })
	a.pumpPTY()
	return a.term
}

func TestPTYFlagAcrossReads(t *testing.T) {
	term := pumpChunks(t, 20, "🇧", "🇷 ")
	if c := term.CellAt(0, 0); c.Content != "🇧🇷" || c.Width != 2 {
		t.Fatalf("célula = %q, largura = %d; esperado a bandeira numa célula dupla", c.Content, c.Width)
	}
}

// Cada corte de um grafema e cada escape partido chegam ao emulador como se
// viessem numa leitura só.
func TestPTYSplitsMatchSingleRead(t *testing.T) {
	cases := [][]string{
		{"🇧", "🇷x"},
		{"🇧", "🇷\r\n"},
		{"🇧", "🇷\x1b[", "31mX"},
		{"\x1b[", "31m", "🇧", "🇷 "},
		{"\x1b]0;tí", "tulo\x07🇧", "🇷"},
	}
	for _, text := range []string{"🇧🇷", "👩\u200d💻", "क\u093c", "한"} {
		for split := 1; split < len(text); split++ {
			cases = append(cases, []string{text[:split], text[split:] + " "})
		}
	}
	for _, chunks := range cases {
		got := pumpChunks(t, 20, chunks...)
		want := vt.NewSafeEmulator(20, 2)
		_, _ = want.WriteString(strings.Join(chunks, ""))
		for y := range 2 {
			for x := range 20 {
				if g, w := got.CellAt(x, y), want.CellAt(x, y); !reflect.DeepEqual(g, w) {
					t.Errorf("%q, célula %d,%d = %+v; esperado %+v", chunks, x, y, g, w)
				}
			}
		}
		_ = want.Close()
	}
}

func TestPTYTailStart(t *testing.T) {
	for _, tc := range []struct{ input, ready string }{
		{"", ""},
		{"abc", "abc"},
		{"🇧x", "🇧x"},
		{"🇧\r", "🇧\r"},
		{"🇧\x1b[", "🇧\x1b["},
		{"αβ", "α"},
		{"αβ\xf0\x9f", "α"},
		{"\xf0\x9f", ""},
		{"x👩\u200d", "x"},
		{"é", ""},
		{"a\xf0\x9f", "a"},
		{"abc\xf0\x9f", "abc"},
		{"\r\n\xf0\x9f", "\r\n"},
	} {
		if n := ptyTailStart([]byte(tc.input)); tc.input[:n] != tc.ready {
			t.Errorf("%q: entregue %q, retido %q; esperado entregar %q", tc.input, tc.input[:n], tc.input[n:], tc.ready)
		}
	}
}

// Sem leitura seguinte, a cauda sai depois do prazo, sem esperar o EOF.
func TestPTYTailDeadline(t *testing.T) {
	r, w := seqpacketPair(t)
	a := &app{term: vt.NewSafeEmulator(20, 2), ptmx: r, paintReq: make(chan struct{}, 2)}
	t.Cleanup(func() { _ = a.term.Close() })
	done := make(chan struct{})
	go func() { a.pumpPTY(); close(done) }()
	if _, err := io.WriteString(w, "abcα"); err != nil {
		t.Fatal(err)
	}
	// Um aviso de pintura para "abc" e outro para a cauda, ambos antes do EOF.
	for range 2 {
		select {
		case <-a.paintReq:
		case <-time.After(time.Second):
			t.Fatal("o prefixo e a cauda não saíram em duas pinturas antes do EOF")
		}
	}
	_ = w.Close()
	<-done
	if c := a.term.CellAt(3, 0); c.Content != "α" {
		t.Fatalf("célula = %q; esperado α", c.Content)
	}
}
