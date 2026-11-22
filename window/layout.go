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

// applyMargins põe cada sessão no retângulo publicado pelo TUI. A principal
// usa o painel inteiro; a prévia, com principal ativa e Preview* preenchido,
// usa o quadro menor (PiP).
func (a *app) applyMargins() {
	if !a.host.Visible || a.fs == nil || a.W <= 0 || a.H <= 0 {
		return // sobreposição na frente ou janela sem grade: mantém a geometria para quando voltar
	}
	if a.sess == nil && a.preview == nil {
		return
	}
	if a.sess != nil {
		a.applySessionMargins(a.sess, a.host.Col, a.host.Row, a.host.Cols, a.host.Rows)
	}
	if a.preview != nil {
		col, row, cols, rows := a.host.Col, a.host.Row, a.host.Cols, a.host.Rows
		if a.sess != nil && a.host.PreviewCols > 0 && a.host.PreviewRows > 0 {
			col, row, cols, rows = a.host.PreviewCol, a.host.PreviewRow, a.host.PreviewCols, a.host.PreviewRows
		}
		a.applySessionMargins(a.preview, col, row, cols, rows)
	}
}

func (a *app) applySessionMargins(s *session, col, row, cols, rows int) {
	if s == nil || s.setProperty == nil {
		return
	}
	W, H := float64(a.W), float64(a.H)
	cw, ch := a.fs.cellW, a.fs.cellH
	x, y := (col-1)*cw, (row-1)*ch
	w, h := cols*cw, rows*ch
	left := fmt.Sprintf("%.5f", max(float64(x), 0)/W)
	right := fmt.Sprintf("%.5f", max(float64(a.W-x-w), 0)/W)
	top := fmt.Sprintf("%.5f", max(float64(y), 0)/H)
	bottom := fmt.Sprintf("%.5f", max(float64(a.H-y-h), 0)/H)
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
	marginStr := strconv.Itoa(margin)
	s.setProperty("video-margin-ratio-left", left)
	s.setProperty("video-margin-ratio-right", right)
	s.setProperty("video-margin-ratio-top", top)
	s.setProperty("video-margin-ratio-bottom", bottom)
	s.setProperty("osd-align-x", osdSide)
	s.setProperty("osd-margin-x", marginStr)
}

// previewPIP diz se a prévia desenha no retângulo menor do HostPanel.
func (a *app) previewPIP() bool {
	return a.sess != nil && a.preview != nil && a.host.PreviewCols > 0 && a.host.PreviewRows > 0
}

// previewPixelRect é o quadro do PiP em pixels da janela (origem em cima à
// esquerda), ou zeros.
func (a *app) previewPixelRect() (x, y, w, h int) {
	if !a.previewPIP() || a.fs == nil {
		return 0, 0, 0, 0
	}
	cw, ch := a.fs.cellW, a.fs.cellH
	x = (a.host.PreviewCol - 1) * cw
	y = (a.host.PreviewRow - 1) * ch
	w = a.host.PreviewCols * cw
	h = a.host.PreviewRows * ch
	return x, y, w, h
}

// refreshHole recalcula se as células do vídeo ficam transparentes agora. Só
// depois do primeiro quadro: até lá a TUI escreve ali o canal que está abrindo,
// e o buraco mostraria só o fundo do mpv.
func (a *app) refreshHole() {
	v := a.activeVideo()
	a.paintMu.Lock()
	a.holeOn = a.haveHost && a.host.Visible && v != nil && v.gotFirst.Load()
	a.paintMu.Unlock()
	a.tuiDirty.Store(true)
	a.kickPaint()
}

func (a *app) setHost(p hostPanel) {
	a.paintMu.Lock()
	a.haveHost, a.host = true, p
	a.hostSeq++
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
