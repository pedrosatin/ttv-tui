package main

/*
#include "bridge.h"
*/
import "C"

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
)

// screenText devolve o texto atual da grade (usado só nos testes de fluxo).
func (a *app) screenText() string {
	var sb strings.Builder
	rows, cols := a.term.Height(), a.term.Width()
	for y := 0; y < rows; y++ {
		var line strings.Builder
		for x := 0; x < cols; x++ {
			c := a.term.CellAt(x, y)
			if c == nil || c.Content == "" {
				if c == nil || c.Width != 0 {
					line.WriteByte(' ')
				}
				continue
			}
			line.WriteString(c.Content)
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

var keyNames = map[string]C.SDL_Keycode{
	"Return": C.SDLK_RETURN, "Escape": C.SDLK_ESCAPE, "Tab": C.SDLK_TAB, "Up": C.SDLK_UP,
	"Down": C.SDLK_DOWN, "Left": C.SDLK_LEFT, "Right": C.SDLK_RIGHT, "Backspace": C.SDLK_BACKSPACE,
}

func (a *app) serveTest(path string) {
	l, err := net.Listen("unix", path)
	if err != nil {
		panic(err)
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 4096)
			n, _ := c.Read(buf)
			line := strings.TrimSpace(string(buf[:n]))
			cmd, rest, _ := strings.Cut(line, " ")
			switch cmd {
			case "key":
				var mod C.SDL_Keymod
				name := rest
				if strings.HasPrefix(name, "ctrl+") {
					mod, name = C.SDL_KMOD_LCTRL, name[5:]
				} else if strings.HasPrefix(name, "alt+") {
					mod, name = C.SDL_KMOD_LALT, name[4:]
				}
				k, ok := keyNames[name]
				if !ok && len(name) == 1 {
					k, ok = C.SDL_Keycode(name[0]), true
				}
				if ok {
					C.inject_key(k, mod)
					fmt.Fprintln(c, "ok")
				} else {
					fmt.Fprintln(c, "tecla?")
				}
			case "text":
				a.writeInput([]byte(rest))
				fmt.Fprintln(c, "ok")
			case "raw": // bytes em hex, direto no PTY (tecla imprimível, espaço)
				if b, err := hex.DecodeString(rest); err == nil {
					a.writeInput(b)
					fmt.Fprintln(c, "ok")
				}
			case "pixel": // BGRA do canto da célula e o maior alfa dentro dela
				var x, y int
				_, _ = fmt.Sscan(rest, &x, &y)
				a.paintMu.Lock()
				cw, ch := a.fs.cellW, a.fs.cellH
				off := y*ch*a.pw*4 + x*cw*4
				top := byte(0)
				for yy := y * ch; yy < (y+1)*ch; yy++ {
					for xx := x * cw; xx < (x+1)*cw; xx++ {
						top = max(top, a.buf[yy*a.pw*4+xx*4+3])
					}
				}
				fmt.Fprintf(c, "%d %d %d %d %d\n", a.buf[off], a.buf[off+1], a.buf[off+2], a.buf[off+3], top)
				a.paintMu.Unlock()
			case "quit": // mesmo caminho de fechar a janela
				C.push_quit()
				fmt.Fprintln(c, "ok")
			case "screen":
				fmt.Fprint(c, a.screenText())
			case "stats":
				a.mu.Lock()
				out := map[string]any{
					"pid": os.Getpid(), "presents": a.presents, "videoFrames": a.videoFrames,
					"panelPaints": a.panelPaints, "paintMs": a.paintNs / 1e6, "uploadMs": a.uploadNs / 1e6,
					"sessions": a.sessions, "ttffMs": a.lastTtff, "ipc": a.lastIpc, "cols": a.cols,
					"rows": a.rows, "W": a.W, "H": a.H, "cellW": a.fs.cellW, "cellH": a.fs.cellH,
					"hole": []int{a.host.Col, a.host.Row, a.host.Cols, a.host.Rows}, "holeOn": a.holeOn, "overlay": a.host.OverlayCols, "overlayLeft": a.host.OverlayLeft, "overlayOpacity": a.host.OverlayOpacity, "haveHost": a.haveHost, "tofu": a.fs.tofu, "stalls": a.stalls, "maxGapMs": a.maxMs, "active": a.sess != nil, "goroutines": runtime.NumGoroutine(),
				}
				if len(a.latMs) > 0 {
					out["latMs"] = a.latMs
				}
				a.mu.Unlock()
				_ = json.NewEncoder(c).Encode(out)
			}
		}(c)
	}
}
