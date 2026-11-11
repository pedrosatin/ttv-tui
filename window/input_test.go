package main

import (
	"bytes"
	"io"
	"os"
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
