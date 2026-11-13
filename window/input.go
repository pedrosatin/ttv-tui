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

// keyBytes converte uma tecla do SDL nos bytes que um xterm mandaria à TUI. As
// teclas de navegação levam os modificadores inteiros no parâmetro do xterm,
// 1 + Shift + 2·Alt + 4·Ctrl: Ctrl+Alt+→ chega como Ctrl+Alt+→, não como Ctrl+→.
func keyBytes(key C.SDL_Keycode, mod C.SDL_Keymod) []byte {
	ctrl := mod&C.SDL_KMOD_CTRL != 0
	alt := mod&C.SDL_KMOD_ALT != 0
	shift := mod&C.SDL_KMOD_SHIFT != 0
	m := 1
	if shift {
		m++
	}
	if alt {
		m += 2
	}
	if ctrl {
		m += 4
	}
	// CSI 1;m X, como em ESC[1;7C; sem modificador, ESC[X.
	csi := func(c byte) []byte {
		if m == 1 {
			return []byte{0x1b, '[', c}
		}
		return fmt.Appendf(nil, "\x1b[1;%d%c", m, c)
	}
	// CSI n;m ~, como em ESC[5;5~; sem modificador, ESC[n~.
	tilde := func(n int) []byte {
		if m == 1 {
			return fmt.Appendf(nil, "\x1b[%d~", n)
		}
		return fmt.Appendf(nil, "\x1b[%d;%d~", n, m)
	}
	if key >= C.SDLK_F1 && key <= C.SDLK_F12 {
		i := int(key - C.SDLK_F1)
		// Bubble Tea 1.3.10 só reconhece Alt+F1 a F12 e Shift+F1 a F8.
		if ctrl || shift && (alt || i >= 8) {
			return nil
		}
		if i < 4 {
			if m == 1 {
				return []byte{0x1b, 'O', byte('P' + i)}
			}
			return csi(byte('P' + i))
		}
		return tilde([...]int{15, 17, 18, 19, 20, 21, 23, 24}[i-4])
	}
	switch key {
	case C.SDLK_RETURN, C.SDLK_KP_ENTER:
		return []byte{'\r'}
	case C.SDLK_ESCAPE:
		return []byte{0x1b}
	case C.SDLK_BACKSPACE:
		// Ctrl+Backspace apaga a palavra, como Alt+Backspace: ESC DEL. O 0x08
		// é Ctrl+H, que na TUI esconde a lista.
		if ctrl || alt {
			return []byte{0x1b, 0x7f}
		}
		return []byte{0x7f}
	case C.SDLK_TAB:
		if shift {
			return []byte{0x1b, '[', 'Z'}
		}
		return []byte{'\t'}
	case C.SDLK_UP:
		return csi('A')
	case C.SDLK_DOWN:
		return csi('B')
	case C.SDLK_RIGHT:
		return csi('C')
	case C.SDLK_LEFT:
		return csi('D')
	case C.SDLK_HOME:
		return csi('H')
	case C.SDLK_END:
		return csi('F')
	case C.SDLK_PAGEUP:
		return tilde(5)
	case C.SDLK_PAGEDOWN:
		return tilde(6)
	case C.SDLK_DELETE:
		return []byte("\x1b[3~")
	}
	if ctrl && key >= 'a' && key <= 'z' {
		b := byte(key-'a') + 1
		// Ctrl+Alt+letra é o controle com ESC na frente; sem Alt, Ctrl+H segue 0x08.
		if alt {
			return []byte{0x1b, b}
		}
		return []byte{b}
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
	case t == C.ev_input(): // socket de teste: text, raw e letras do key
		p := C.input_data(ev)
		a.writeInput(C.GoBytes(p, C.input_len(ev)))
		C.SDL_free(p)
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
			case "resize":
				a.resizeForTest(msg)
			}
		}
		if len(a.fbq) > 0 {
			a.layoutDirty = true // a leitura acontece no próximo quadro, antes da troca de buffers
		}
	}
}
