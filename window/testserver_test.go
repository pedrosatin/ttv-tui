package main

import "testing"

func TestStatsDuringFontReplacement(t *testing.T) {
	a := &app{fs: &fontSet{cellW: 1, cellH: 2}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			a.paintMu.Lock()
			a.fs.close()
			a.fs = &fontSet{cellW: i + 1, cellH: i + 2, tofu: i}
			a.paintMu.Unlock()
		}
	}()
	for i := 0; i < 10000; i++ {
		out := a.stats()
		if out["cellH"].(int) != out["cellW"].(int)+1 {
			t.Error("stats misturou métricas de fontes diferentes")
		}
	}
	<-done
}
