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
			_, _ = a.ptmx.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
