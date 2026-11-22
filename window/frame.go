package main

/*
#include "bridge.h"
*/
import "C"

import "time"

func (a *app) present(mainVideo, previewVideo bool) {
	W, H := C.int(a.W), C.int(a.H)
	v := a.activeVideo()
	previewRendered := false
	if v != nil {
		C.rc_render(v.rc, W, H)
		// Sessão do painel inteiro: principal, ou prévia sozinha sem PiP.
		fullFrame := mainVideo
		if v == a.preview {
			fullFrame = previewVideo
		}
		a.noteFirstFrame(v, fullFrame)
		// Prévia em FBO W×H próprio e composição só do retângulo: mpv_render apaga o FBO 0.
		if a.previewPIP() && a.preview != nil && a.preview.rc != nil && a.preview != v {
			px, py, pw, ph := a.previewPixelRect()
			if pw > 0 && ph > 0 {
				switch st := C.pip_fbo_ensure(W, H); {
				case st < 0:
					// Sem FBO: a prévia fica de fora neste quadro.
				default:
					if previewVideo || st > 0 {
						C.rc_render_to_pip(a.preview.rc)
						previewRendered = true
						a.noteFirstFrame(a.preview, previewVideo)
					}
					if a.preview.gotFirst.Load() {
						C.pip_compose(C.int(px), C.int(py), C.int(pw), C.int(ph), W, H)
					}
				}
			}
		}
	} else {
		C.clear_to(W, H, C.float(float32(bgR)/255), C.float(float32(bgG)/255), C.float(float32(bgB)/255))
	}
	if a.buf != nil {
		C.panel_begin(W, H)
		for i, t := range a.tiles {
			C.panel_tile(a.texs[i], C.int(a.px+t.x), C.int(t.y), C.int(t.w), C.int(t.h), C.float(t.u), C.float(t.v))
		}
		C.panel_end()
	}
	if a.glCheck {
		a.glCheck = false
		a.noteGLError("desenhar o painel")
	}
	a.answerFB()
	C.SDL_GL_SwapWindow(a.win)
	if v != nil {
		C.mpv_render_context_report_swap(v.rc)
	}
	if previewRendered && a.preview != nil && a.preview.rc != nil {
		C.mpv_render_context_report_swap(a.preview.rc)
	}
	now := time.Now()
	a.lastPresent = now
	a.mu.Lock()
	a.presents++
	if mainVideo || previewVideo {
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

func (a *app) noteFirstFrame(s *session, video bool) {
	if s == nil || !video {
		return
	}
	if !s.gotFirst.Swap(true) {
		s.ttffMs.Store(time.Since(s.started).Milliseconds())
		a.mu.Lock()
		a.lastTtff = s.ttffMs.Load()
		a.mu.Unlock()
		a.refreshHole() // o primeiro quadro abre o buraco do vídeo
	}
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
	a.uploadRect(x0*cw, y0*ch, (x1-x0+1)*cw, (y1-y0+1)*ch)
	a.mu.Lock()
	a.uploadNs += time.Since(t0).Nanoseconds()
	a.mu.Unlock()
	a.panelChanged = true
}

func (a *app) frame() {
	a.uploadPanel()
	v := a.activeVideo()
	switch {
	case a.mainDirty || a.previewDirty:
		a.present(a.mainDirty, a.previewDirty)
		a.mainDirty = false
		// Só limpa o aviso da prévia depois de poder compor (Preview* no HostPanel)
		// ou quando a sessão sumiu; senão rc_has_frame já consumiu o quadro.
		if a.previewPIP() || a.preview == nil {
			a.previewDirty = false
		}
		a.layoutDirty, a.panelChanged = false, false
	case a.layoutDirty, a.panelChanged && (v == nil || !v.gotFirst.Load() || time.Since(a.lastPresent) > 33*time.Millisecond):
		// Painel sem quadro de vídeo: pausado, antes do primeiro quadro, ou sem sessão.
		a.present(false, false)
		a.layoutDirty, a.panelChanged = false, false
	}
}
