package main

import (
	"bytes"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

type ptyChunks []string

func (r *ptyChunks) Read(b []byte) (int, error) {
	if len(*r) == 0 {
		return 0, io.EOF
	}
	n := copy(b, (*r)[0])
	*r = (*r)[1:]
	return n, nil
}

func TestPTYFlagAcrossReads(t *testing.T) {
	term := vt.NewEmulator(20, 2)
	defer term.Close()
	chunks := ptyChunks{"🇧", "🇷 "}
	readPTY(&chunks, func(b []byte) { _, _ = term.Write(b) })
	cell := term.CellAt(0, 0)
	if cell.Content != "🇧🇷" || cell.Width != 2 {
		t.Fatalf("célula = %q, largura = %d; esperado bandeira inteira com largura 2", cell.Content, cell.Width)
	}
}

func TestPTYUnicodeSplits(t *testing.T) {
	for _, text := range []string{"🇧🇷", "👩‍💻", "क़", "한"} {
		for split := 1; split < len(text); split++ {
			term := vt.NewEmulator(40, 2)
			chunks := ptyChunks{text[:split], text[split:] + " "}
			readPTY(&chunks, func(b []byte) { _, _ = term.Write(b) })
			cell := term.CellAt(0, 0)
			if cell.Content != text {
				t.Errorf("%q split %d: célula = %q", text, split, cell.Content)
			}
			term.Close()
		}
	}
}

func TestPTYDelimitersAndEscapes(t *testing.T) {
	for _, chunks := range []ptyChunks{
		{"🇧", "🇷x"},
		{"🇧", "🇷\r"},
		{"🇧", "🇷\x1b[", "31mX"},
		{"\x1b[", "31m", "🇧", "🇷 "},
	} {
		term := vt.NewEmulator(40, 2)
		want := vt.NewEmulator(40, 2)
		var input []byte
		for _, b := range chunks {
			input = append(input, b...)
		}
		want.Write(input)
		var writes [][]byte
		readPTY(&chunks, func(b []byte) { writes = append(writes, bytes.Clone(b)); term.Write(b) })
		if !bytes.Equal(bytes.Join(writes, nil), input) {
			t.Fatal("bytes alterados")
		}
		for x := 0; x < 40; x++ {
			if !reflect.DeepEqual(term.CellAt(x, 0), want.CellAt(x, 0)) {
				t.Fatalf("célula %d difere para %q", x, input)
			}
		}
		term.Close()
		want.Close()
	}
}

func TestPTYTailStart(t *testing.T) {
	for _, tc := range []struct{ input, ready, tail string }{
		{"abc", "abc", ""},
		{"a\xf0\x9f", "a", "\xf0\x9f"},
		{"abc\xf0\x9f", "abc", "\xf0\x9f"},
		{"\r\n\xf0\x9f", "\r\n", "\xf0\x9f"},
		{"🇧x", "🇧x", ""},
		{"🇧\r", "🇧\r", ""},
		{"🇧\x1b[", "🇧\x1b[", ""},
		{"αβ", "α", "β"},
		{"αβ\xf0\x9f", "α", "β\xf0\x9f"},
		{"é", "", "é"},
	} {
		n := ptyTailStart([]byte(tc.input))
		if tc.input[:n] != tc.ready || tc.input[n:] != tc.tail {
			t.Errorf("%q: prefixo %q, cauda %q", tc.input, tc.input[:n], tc.input[n:])
		}
	}
}

func TestPTYTailTimeout(t *testing.T) {
	r, w := io.Pipe()
	delivered := make(chan string, 4)
	done := make(chan struct{})
	go func() { readPTY(r, func(b []byte) { delivered <- string(b) }); close(done) }()
	defer func() { w.Close(); <-done; r.Close() }()
	w.Write([]byte("abcα"))
	select {
	case got := <-delivered:
		if got != "abc" {
			t.Fatalf("prefixo = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("prefixo ASCII ficou retido")
	}
	select {
	case got := <-delivered:
		if got != "α" {
			t.Fatalf("cauda = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cauda não saiu no prazo")
	}
	w.Write([]byte("β"))
	w.Close()
	<-done
	if got := <-delivered; got != "β" {
		t.Fatalf("cauda no EOF = %q", got)
	}
}
