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
	"time"
	"unsafe"
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
	"F1": C.SDLK_F1, "F2": C.SDLK_F2, "F3": C.SDLK_F3, "F4": C.SDLK_F4,
	"F5": C.SDLK_F5, "F6": C.SDLK_F6, "F7": C.SDLK_F7, "F8": C.SDLK_F8,
	"F9": C.SDLK_F9, "F10": C.SDLK_F10, "F11": C.SDLK_F11, "F12": C.SDLK_F12,
	"Home": C.SDLK_HOME, "End": C.SDLK_END, "PageUp": C.SDLK_PAGEUP, "PageDown": C.SDLK_PAGEDOWN,
	"Return": C.SDLK_RETURN, "Escape": C.SDLK_ESCAPE, "Tab": C.SDLK_TAB, "Up": C.SDLK_UP,
	"Down": C.SDLK_DOWN, "Left": C.SDLK_LEFT, "Right": C.SDLK_RIGHT, "Backspace": C.SDLK_BACKSPACE,
	"Delete": C.SDLK_DELETE, "Space": C.SDLK_SPACE,
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
			defer func() { _ = c.Close() }()
			buf := make([]byte, 4096)
			n, _ := c.Read(buf)
			line := strings.TrimSpace(string(buf[:n]))
			cmd, rest, _ := strings.Cut(line, " ")
			switch cmd {
			case "key":
				k, ok := parseTestKey(rest)
				if !ok {
					_, _ = fmt.Fprintln(c, "tecla?")
					return
				}
				if text, ok := k.text(); ok {
					injectInput([]byte(text)) // o que o SDL_EVENT_TEXT_INPUT escreveria
					_, _ = fmt.Fprintln(c, "ok")
					return
				}
				code, ok := keyNames[k.name]
				if !ok && len(k.name) == 1 {
					code, ok = C.SDL_Keycode(strings.ToLower(k.name)[0]), true
				}
				if !ok {
					_, _ = fmt.Fprintln(c, "tecla?")
					return
				}
				var mod C.SDL_Keymod
				if k.ctrl {
					mod |= C.SDL_KMOD_LCTRL
				}
				if k.alt {
					mod |= C.SDL_KMOD_LALT
				}
				if k.shift {
					mod |= C.SDL_KMOD_LSHIFT
				}
				C.inject_key(code, mod)
				_, _ = fmt.Fprintln(c, "ok")
			case "text":
				injectInput([]byte(rest))
				_, _ = fmt.Fprintln(c, "ok")
			case "raw": // bytes em hex para o PTY, na mesma fila das teclas
				if b, err := hex.DecodeString(rest); err == nil {
					injectInput(b)
					_, _ = fmt.Fprintln(c, "ok")
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
				_, _ = fmt.Fprintf(c, "%d %d %d %d %d\n", a.buf[off], a.buf[off+1], a.buf[off+2], a.buf[off+3], top)
				a.paintMu.Unlock()
			case "fbpixel": // RGB do pixel da janela no quadro desenhado: o que a GPU pôs na tela
				var x, y int
				_, _ = fmt.Sscan(rest, &x, &y)
				r := fbReq{x: x, y: y, reply: make(chan [3]byte, 1)}
				select {
				case a.fbq <- r:
					C.push_ev(C.ev_ctl())
				default:
					_, _ = fmt.Fprintln(c, "ocupado")
					return
				}
				select {
				case px := <-r.reply:
					_, _ = fmt.Fprintf(c, "%d %d %d\n", px[0], px[1], px[2])
				case <-time.After(2 * time.Second):
					_, _ = fmt.Fprintln(c, "sem quadro")
				}
			case "quit": // mesmo caminho de fechar a janela
				C.push_quit()
				_, _ = fmt.Fprintln(c, "ok")
			case "screen":
				_, _ = fmt.Fprint(c, a.screenText())
			case "stats":
				if rest == "reset" {
					a.resetStats()
					_, _ = fmt.Fprintln(c, "ok")
					return
				}
				_ = json.NewEncoder(c).Encode(a.stats())
			case "state":
				_ = json.NewEncoder(c).Encode(a.testStateView())
			case "resize": // W H em pixels; responde com a grade nova, "colunas linhas"
				var w, h int
				if n, _ := fmt.Sscan(rest, &w, &h); n != 2 || w < 200 || h < 120 || w > 8192 || h > 8192 {
					_, _ = fmt.Fprintln(c, "tamanho?")
					return
				}
				msg := ctlMsg{kind: "resize", w: w, h: h, resized: make(chan [2]int, 1)}
				a.ctlq <- msg
				C.push_ev(C.ev_ctl())
				select {
				case g := <-msg.resized:
					_, _ = fmt.Fprintf(c, "%d %d\n", g[0], g[1])
				case <-time.After(5 * time.Second):
					_, _ = fmt.Fprintln(c, "sem resposta")
				}
			default:
				_, _ = fmt.Fprintln(c, "comando?")
			}
		}(c)
	}
}

func (a *app) stats() map[string]any {
	a.paintMu.Lock()
	defer a.paintMu.Unlock()
	a.mu.Lock()
	out := map[string]any{
		"pid": os.Getpid(), "presents": a.presents, "videoFrames": a.videoFrames,
		"panelPaints": a.panelPaints, "paintMs": a.paintNs / 1e6, "uploadMs": a.uploadNs / 1e6,
		"sessions": a.sessions, "ttffMs": a.lastTtff, "ipc": a.lastIpc, "cols": a.cols,
		"rows": a.rows, "W": a.W, "H": a.H, "cellW": a.fs.cellW, "cellH": a.fs.cellH,
		"hole": []int{a.host.Col, a.host.Row, a.host.Cols, a.host.Rows}, "holeOn": a.holeOn, "overlay": a.host.OverlayCols, "overlayLeft": a.host.OverlayLeft, "overlayOpacity": a.host.OverlayOpacity, "haveHost": a.haveHost, "tofu": a.fs.tofu, "stalls": a.stalls, "maxGapMs": a.maxMs, "active": a.sess != nil, "goroutines": runtime.NumGoroutine(),
	}
	out["gl"] = map[string]any{
		"version": a.glName, "npot": a.gl.npot, "bgra": a.gl.bgra, "maxTex": a.gl.maxTex,
		"tiles": len(a.tiles), "errors": a.glErrors, "lastError": fmt.Sprintf("0x%04x", a.glLastError),
	}
	if len(a.latMs) > 0 {
		out["latMs"] = a.latMs
	}
	a.mu.Unlock()
	return out
}

// injectInput põe bytes na fila de eventos do SDL, atrás das teclas já pedidas.
func injectInput(b []byte) {
	if len(b) == 0 {
		return
	}
	C.inject_input(unsafe.Pointer(&b[0]), C.int(len(b)))
}

// resizeForTest muda o tamanho da janela na thread do SDL e responde com a grade.
func (a *app) resizeForTest(msg ctlMsg) {
	density := float64(C.SDL_GetWindowPixelDensity(a.win))
	if density <= 0 {
		density = 1
	}
	C.SDL_SetWindowSize(a.win, C.int(float64(msg.w)/density), C.int(float64(msg.h)/density))
	C.SDL_SyncWindow(a.win)
	a.relayout()
	msg.resized <- [2]int{a.cols, a.rows}
}

// resetStats zera as medidas de desempenho, para cada cenário medir só a própria
// sessão. Sessões, tofu e erros de GL ficam: contam a vida inteira da janela.
func (a *app) resetStats() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.presents, a.videoFrames, a.panelPaints = 0, 0, 0
	a.paintNs, a.uploadNs = 0, 0
	a.latMs, a.maxMs, a.stalls = nil, 0, 0
	a.lastVideo, a.inputAt = time.Time{}, time.Time{}
}

// testStateView junta a geometria da janela e o último resumo da TUI.
func (a *app) testStateView() map[string]any {
	a.paintMu.Lock()
	win := map[string]any{
		"cols": a.cols, "rows": a.rows, "W": a.W, "H": a.H, "cellW": a.fs.cellW, "cellH": a.fs.cellH,
		"hole": []int{a.host.Col, a.host.Row, a.host.Cols, a.host.Rows}, "holeOn": a.holeOn,
		"overlay": a.host.OverlayCols, "overlayLeft": a.host.OverlayLeft, "overlayOpacity": a.host.OverlayOpacity,
	}
	a.paintMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]any{"window": win, "tui": nil}
	if len(a.testState) > 0 {
		out["tui"] = json.RawMessage(append([]byte(nil), a.testState...))
	}
	return out
}
