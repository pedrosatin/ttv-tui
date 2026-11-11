package main

/*
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"os"
	"time"
)

func keyBytes(key C.SDL_Keycode, mod C.SDL_Keymod) []byte {
	ctrl := mod&C.SDL_KMOD_CTRL != 0
	alt := mod&C.SDL_KMOD_ALT != 0
	shift := mod&C.SDL_KMOD_SHIFT != 0
	arrow := func(c byte) []byte {
		switch {
		case ctrl:
			return []byte{0x1b, '[', '1', ';', '5', c}
		case shift:
			return []byte{0x1b, '[', '1', ';', '2', c}
		case alt:
			return []byte{0x1b, '[', '1', ';', '3', c}
		}
		return []byte{0x1b, '[', c}
	}
	switch key {
	case C.SDLK_RETURN, C.SDLK_KP_ENTER:
		return []byte{'\r'}
	case C.SDLK_ESCAPE:
		return []byte{0x1b}
	case C.SDLK_BACKSPACE:
		if ctrl {
			return []byte{0x08}
		}
		return []byte{0x7f}
	case C.SDLK_TAB:
		if shift {
			return []byte{0x1b, '[', 'Z'}
		}
		return []byte{'\t'}
	case C.SDLK_UP:
		return arrow('A')
	case C.SDLK_DOWN:
		return arrow('B')
	case C.SDLK_RIGHT:
		return arrow('C')
	case C.SDLK_LEFT:
		return arrow('D')
	case C.SDLK_F1:
		return []byte("\x1bOP")
	case C.SDLK_F2:
		return []byte("\x1bOQ")
	case C.SDLK_F3:
		return []byte("\x1bOR")
	case C.SDLK_F4:
		return []byte("\x1bOS")
	case C.SDLK_F5:
		return []byte("\x1b[15~")
	case C.SDLK_F6:
		return []byte("\x1b[17~")
	case C.SDLK_F7:
		return []byte("\x1b[18~")
	case C.SDLK_F8:
		return []byte("\x1b[19~")
	case C.SDLK_F9:
		return []byte("\x1b[20~")
	case C.SDLK_F10:
		return []byte("\x1b[21~")
	case C.SDLK_F11:
		return []byte("\x1b[23~")
	case C.SDLK_F12:
		return []byte("\x1b[24~")
	case C.SDLK_HOME:
		return []byte("\x1b[H")
	case C.SDLK_END:
		return []byte("\x1b[F")
	case C.SDLK_PAGEUP:
		return []byte("\x1b[5~")
	case C.SDLK_PAGEDOWN:
		return []byte("\x1b[6~")
	case C.SDLK_DELETE:
		return []byte("\x1b[3~")
	}
	if ctrl && key >= 'a' && key <= 'z' {
		return []byte{byte(key-'a') + 1}
	}
	if alt && key >= 0x20 && key < 0x7f {
		return []byte{0x1b, byte(key)}
	}
	return nil
}

func (a *app) writeInput(b []byte) {
	if a.ptmx == nil || len(b) == 0 {
		return
	}
	a.mu.Lock()
	a.inputAt = time.Now()
	a.mu.Unlock()
	// A fila cheia descarta o evento inteiro, sem cortar sequências de tecla.
	select {
	case a.inputq <- append([]byte(nil), b...):
	default:
	}
}

func (a *app) handle(ev *C.SDL_Event) {
	t := C.e_type(ev)
	switch {
	case t == C.SDL_EVENT_QUIT || t == C.SDL_EVENT_WINDOW_CLOSE_REQUESTED:
		a.quit = true
	case t == C.SDL_EVENT_WINDOW_PIXEL_SIZE_CHANGED || t == C.SDL_EVENT_WINDOW_RESIZED:
		a.relayout()
	case t == C.SDL_EVENT_WINDOW_DISPLAY_SCALE_CHANGED:
		fs, err := scaledFontSet(a.pt, float64(C.SDL_GetWindowDisplayScale(a.win)))
		if err != nil {
			fmt.Fprintln(os.Stderr, "display scale:", err)
			return
		}
		a.relayoutFonts(fs)
	case t == C.SDL_EVENT_WINDOW_EXPOSED:
		a.layoutDirty = true
	case t == C.SDL_EVENT_KEY_DOWN:
		a.writeInput(keyBytes(C.e_key(ev), C.e_mod(ev)))
	case t == C.SDL_EVENT_TEXT_INPUT:
		a.writeInput([]byte(C.GoString(C.e_text(ev))))
	case t == C.ev_mpv():
		if a.sess != nil && C.rc_has_frame(a.sess.rc) != 0 {
			a.videoDirty = true
		}
	case t == C.ev_tui():
		a.tuiPending.Store(false)
	case t == C.ev_ctl():
		for len(a.ctlq) > 0 {
			msg := <-a.ctlq
			switch msg.kind {
			case "layout":
				a.setHost(msg.host)
			case "start":
				a.startSession(msg)
			case "ended":
				if a.sess == msg.s {
					a.stopSession()
				}
			}
		}
	}
}
