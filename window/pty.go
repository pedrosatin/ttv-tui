package main

import (
	"io"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func (a *app) pumpPTY() {
	readPTY(a.ptmx, func(b []byte) {
		_, _ = a.term.Write(b)
		a.tuiDirty.Store(true)
		a.kickPaint()
	})
}

// ptyTailWait limita a espera pela continuação de um grafema ao tempo de um quadro.
const ptyTailWait = 16 * time.Millisecond

func readPTY(r io.Reader, write func([]byte)) {
	chunks := make(chan []byte)
	go func() {
		defer close(chunks)
		buf := make([]byte, 64*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(ptyTailWait)
	defer timer.Stop()
	timer.Stop()
	var tick <-chan time.Time
	var pending []byte
	flush := func(n int) {
		if n > 0 {
			write(pending[:n])
			pending = append(pending[:0], pending[n:]...)
		}
	}
	for {
		select {
		case b, ok := <-chunks:
			if !ok {
				flush(len(pending))
				return
			}
			pending = append(pending, b...)
			flush(ptyTailStart(pending))
			timer.Stop()
			tick = nil
			if len(pending) > 0 {
				timer.Reset(ptyTailWait)
				tick = timer.C
			}
		case <-tick:
			flush(len(pending))
			tick = nil
		}
	}
}

// ptyTailStart retém só o último cluster não ASCII e seu possível UTF-8 partido.
// ASCII e controles delimitam o trecho que ainda pode continuar na próxima leitura.
func ptyTailStart(b []byte) int {
	start := 0
	for i, c := range b {
		if c < utf8.RuneSelf {
			start = i
			if i == len(b)-1 {
				return len(b)
			}
		}
	}
	end := start
	for end < len(b) && utf8.FullRune(b[end:]) {
		_, n := utf8.DecodeRune(b[end:])
		end += n
	}
	for start < end {
		cluster, _ := ansi.FirstGraphemeCluster(b[start:end], ansi.GraphemeWidth)
		next := start + len(cluster)
		if next == end {
			break
		}
		start = next
	}
	return start
}

func (a *app) pumpReplies() {
	buf := make([]byte, 4096)
	for {
		n, err := a.term.Read(buf)
		if n > 0 && a.ptmx != nil {
			a.writeInput(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// pumpInput é o único escritor do PTY.
func (a *app) pumpInput() {
	for b := range a.inputq {
		for len(b) > 0 {
			n, err := a.ptmx.Write(b)
			if err != nil || n == 0 {
				return
			}
			b = b[n:]
		}
	}
}
