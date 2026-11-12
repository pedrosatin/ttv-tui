package main

/*
#include "bridge.h"
*/
import "C"

import (
	"time"
	"unsafe"
)

func (a *app) present(video bool) {
	W, H := C.int(a.W), C.int(a.H)
	if a.sess != nil {
		C.rc_render(a.sess.rc, W, H)
		if video {
			if !a.sess.gotFirst.Swap(true) {
				a.sess.ttffMs.Store(time.Since(a.sess.started).Milliseconds())
				a.mu.Lock()
				a.lastTtff = a.sess.ttffMs.Load()
				a.mu.Unlock()
			}
		}
	} else {
		C.clear_to(W, H, C.float(float32(bgR)/255), C.float(float32(bgG)/255), C.float(float32(bgB)/255))
	}
	if a.buf != nil {
		C.draw_panel(a.tex, W, H, C.int(a.px), 0, C.int(a.pw), C.int(a.ph))
	}
	a.answerFB()
	C.SDL_GL_SwapWindow(a.win)
	if a.sess != nil {
		C.mpv_render_context_report_swap(a.sess.rc)
	}
	now := time.Now()
	a.lastPresent = now
	a.mu.Lock()
	a.presents++
	if video {
		a.videoFrames++
		if !a.lastVideo.IsZero() {
			gap := now.Sub(a.lastVideo).Milliseconds()
			if gap > 40 {
				a.stalls++
			}
			if gap > a.maxMs {
				a.maxMs = gap
			}
		}
		a.lastVideo = now
	}
	if !a.inputAt.IsZero() && a.panelChanged {
		ms := float64(now.Sub(a.inputAt).Microseconds()) / 1000
		a.latMs = append(a.latMs, ms)
		a.inputAt = time.Time{}
	}
	a.mu.Unlock()
}

// answerFB responde às leituras pendentes com o quadro que acabou de ser desenhado.
func (a *app) answerFB() {
	for len(a.fbq) > 0 {
		r := <-a.fbq
		var px [4]C.uchar
		if r.x >= 0 && r.x < a.W && r.y >= 0 && r.y < a.H {
			C.fb_pixel(C.int(r.x), C.int(a.H-1-r.y), &px[0])
		}
		r.reply <- [3]byte{byte(px[0]), byte(px[1]), byte(px[2])}
	}
}

// painter roda em goroutine própria, no máximo a ~30 Hz.
func (a *app) painter() {
	var last time.Time
	for range a.paintReq {
		if d := time.Since(last); d < 16*time.Millisecond { // um quadro de 60 Hz
			time.Sleep(16*time.Millisecond - d)
		}
		for a.tuiDirty.Swap(false) {
			a.paintMu.Lock()
			y0, y1 := a.paint()
			if y1 >= 0 {
				if y0 < a.dirtyY0 {
					a.dirtyY0 = y0
				}
				if y1 > a.dirtyY1 {
					a.dirtyY1 = y1
				}
			}
			a.paintMu.Unlock()
			last = time.Now()
			C.push_ev(C.ev_tui())
		}
	}
}

func (a *app) kickPaint() {
	select {
	case a.paintReq <- struct{}{}:
	default:
	}
}

// uploadPanel manda à GPU as linhas prontas, sem nunca esperar o painter.
func (a *app) uploadPanel() {
	if !a.paintMu.TryLock() {
		return
	}
	defer a.paintMu.Unlock()
	if a.dirtyY1 < 0 {
		return
	}
	t0 := time.Now()
	y0, y1, x0, x1 := a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1
	a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1 = a.rows, -1, a.cols, -1
	cw, ch := a.fs.cellW, a.fs.cellH
	off := y0*ch*a.pw*4 + x0*cw*4
	C.tex_rect(a.tex, C.int(x0*cw), C.int(y0*ch), C.int((x1-x0+1)*cw), C.int((y1-y0+1)*ch), C.int(a.pw), unsafe.Pointer(&a.buf[off]))
	a.mu.Lock()
	a.uploadNs += time.Since(t0).Nanoseconds()
	a.mu.Unlock()
	a.panelChanged = true
}

func (a *app) frame() {
	a.uploadPanel()
	switch {
	case a.videoDirty:
		a.present(true)
		a.videoDirty, a.layoutDirty, a.panelChanged = false, false, false
	case a.layoutDirty, a.panelChanged && (a.sess == nil || time.Since(a.lastPresent) > 33*time.Millisecond):
		// Painel sem quadro de vídeo: pausado, ou sem sessão.
		a.present(false)
		a.layoutDirty, a.panelChanged = false, false
	}
}
