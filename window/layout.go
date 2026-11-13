package main

/*
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
)

// relayout recalcula painel, PTY e emulador a partir do tamanho da janela.
func (a *app) relayout() { a.relayoutFonts(nil) }

// relayoutFonts troca a fonte e a grade sob o mesmo lock usado pelo pintor.
func (a *app) relayoutFonts(fs *fontSet) {
	a.paintMu.Lock()
	defer a.paintMu.Unlock()
	var w, h C.int
	C.SDL_GetWindowSizeInPixels(a.win, &w, &h)
	if w == 0 || h == 0 {
		if fs != nil {
			fs.close()
		}
		return
	}
	if fs != nil {
		a.fs.close()
		a.fs = fs
	}
	a.W, a.H = int(w), int(h)
	a.px = 0
	cols := (a.W - a.px) / a.fs.cellW
	rows := a.H / a.fs.cellH
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	pw, ph := cols*a.fs.cellW, rows*a.fs.cellH
	if fs != nil || cols != a.cols || rows != a.rows {
		a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1 = rows, -1, cols, -1
		a.cols, a.rows, a.pw, a.ph = cols, rows, pw, ph
		a.buf = make([]byte, pw*ph*4)
		for i := 0; i < len(a.buf); i += 4 {
			a.buf[i], a.buf[i+1], a.buf[i+2], a.buf[i+3] = bgB, bgG, bgR, 255
		}
		a.prev = make([]cellKey, cols*rows)
		a.allocPanel()
		a.term.Resize(cols, rows)
		if a.ptmx != nil {
			_ = a.ptmx.Resize(cols, rows, pw, ph)
		}
		a.tuiDirty.Store(true)
		a.kickPaint()
	}
	a.applyMargins()
	a.layoutDirty = true
}

// applyMargins põe o vídeo no retângulo publicado pelo TUI.
func (a *app) applyMargins() {
	if a.sess == nil {
		return
	}
	W, H := float64(a.W), float64(a.H)
	ratio := func(name string, px int) {
		a.sess.setProperty("video-margin-ratio-"+name, fmt.Sprintf("%.5f", max(float64(px), 0)/map[bool]float64{true: W, false: H}[name == "left" || name == "right"]))
	}
	if !a.host.Visible {
		return // sobreposição na frente: mantém a geometria para quando ela sair
	}
	cw, ch := a.fs.cellW, a.fs.cellH
	x, y := (a.host.Col-1)*cw, (a.host.Row-1)*ch
	w, h := a.host.Cols*cw, a.host.Rows*ch
	ratio("left", x)
	ratio("right", a.W-x-w)
	ratio("top", y)
	ratio("bottom", a.H-y-h)
	// O OSD do mpv é relativo à janela: a margem o traz para dentro do vídeo, e ele fica
	// no canto oposto ao chat sobreposto. A margem é limitada a 300 unidades pelo mpv.
	osdSide, edge := "left", x
	if a.host.OverlayLeft {
		osdSide, edge = "right", a.W-x-w
	}
	margin := max(edge+16, 0) * 720 / a.H
	if margin > 300 {
		osdSide, margin = "center", 0
	}
	a.sess.setProperty("osd-align-x", osdSide)
	a.sess.setProperty("osd-margin-x", strconv.Itoa(margin))
}

// refreshHole recalcula se as células do vídeo ficam transparentes agora.
func (a *app) refreshHole() {
	a.paintMu.Lock()
	a.holeOn = a.haveHost && a.host.Visible && a.sess != nil
	a.paintMu.Unlock()
	a.tuiDirty.Store(true)
	a.kickPaint()
}

func (a *app) setHost(p hostPanel) {
	a.paintMu.Lock()
	a.haveHost, a.host = true, p
	a.paintMu.Unlock()
	a.refreshHole()
	a.relayout()
}

// layoutMsg é uma linha do socket de layout: um painel, ou o resumo do estado
// que a TUI manda nos cenários de teste (TTV_TUI_TEST_STATE).
type layoutMsg struct {
	hostPanel
	TestState json.RawMessage `json:"testState"`
}

// noteTestState guarda o resumo para o comando state; fora dos testes descarta.
func (a *app) noteTestState(raw json.RawMessage) {
	if !a.testing {
		return
	}
	a.mu.Lock()
	a.testState = append(a.testState[:0], raw...)
	a.mu.Unlock()
}

func (a *app) serveLayout(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = c.Close() }()
			dec := json.NewDecoder(c)
			for {
				var m layoutMsg
				if dec.Decode(&m) != nil {
					return
				}
				if m.TestState != nil {
					a.noteTestState(m.TestState)
					continue
				}
				a.ctlq <- ctlMsg{kind: "layout", host: m.hostPanel}
				C.push_ev(C.ev_ctl())
			}
		}()
	}
}
