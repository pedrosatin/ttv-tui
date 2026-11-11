package main

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// ptyTailWait limita a um quadro a espera pela continuação de um grafema.
const ptyTailWait = 16 * time.Millisecond

// pumpPTY segura o último grafema de cada leitura até a seguinte, porque o
// emulador fecha o cluster no fim de cada Write e uma bandeira partida viraria
// duas células duplas.
func (a *app) pumpPTY() {
	var mu sync.Mutex
	var tail []byte
	write := func(b []byte) {
		if len(b) > 0 {
			_, _ = a.term.Write(b)
			a.tuiDirty.Store(true)
			a.kickPaint()
		}
	}
	timer := time.AfterFunc(ptyTailWait, func() {
		mu.Lock()
		write(tail)
		tail = tail[:0]
		mu.Unlock()
	})
	timer.Stop()
	buf := make([]byte, 64*1024)
	for {
		n, err := a.ptmx.Read(buf)
		mu.Lock()
		timer.Stop()
		tail = append(tail, buf[:n]...)
		k := len(tail)
		if err == nil {
			k = ptyTailStart(tail)
		}
		write(tail[:k])
		tail = append(tail[:0], tail[k:]...)
		if len(tail) > 0 {
			timer.Reset(ptyTailWait)
		}
		mu.Unlock()
		if err != nil {
			return
		}
	}
}

// ptyTailStart devolve onde começa o trecho que a próxima leitura ainda pode
// continuar: o último grafema depois do último byte ASCII e um UTF-8 partido.
// Nenhum byte ASCII estende o cluster anterior, então ele encerra o que veio antes.
func ptyTailStart(b []byte) int {
	start := len(b) - 1
	for start >= 0 && b[start] >= utf8.RuneSelf {
		start--
	}
	if start == len(b)-1 {
		return len(b)
	}
	start++ // o byte em start é ASCII e já fecha o cluster anterior
	end := start
	for end < len(b) && utf8.FullRune(b[end:]) {
		_, n := utf8.DecodeRune(b[end:])
		end += n
	}
	for start < end {
		cluster, _ := ansi.FirstGraphemeCluster(b[start:end], ansi.GraphemeWidth)
		if len(cluster) == 0 || start+len(cluster) == end {
			break
		}
		start += len(cluster)
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
