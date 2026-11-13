//go:build !windows

package main

import (
	"bytes"
	"io"
	"testing"
	"time"

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
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	a := &app{ptmx: mockPTYDevice{w}, inputq: make(chan []byte, 1)}
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
