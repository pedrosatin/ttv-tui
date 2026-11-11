package main

func (a *app) pumpPTY() {
	buf := make([]byte, 64*1024)
	for {
		n, err := a.ptmx.Read(buf)
		if n > 0 {
			_, _ = a.term.Write(buf[:n])
			a.tuiDirty.Store(true)
			a.kickPaint()
		}
		if err != nil {
			return
		}
	}
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
