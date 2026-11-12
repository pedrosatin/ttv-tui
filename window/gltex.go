package main

/*
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"unsafe"
)

// readGL guarda o que o contexto aceita. TTV_WINDOW_MAX_TEX só reduz o limite de textura,
// para exercitar a divisão em ladrilhos numa máquina cujo OpenGL aceita o painel inteiro.
func (a *app) readGL() {
	version := C.GoString(C.gl_str(C.GL_VERSION))
	a.gl = parseGLCaps(version, C.GoString(C.gl_str(C.GL_EXTENSIONS)), int(C.gl_max_tex()))
	if n, err := strconv.Atoi(os.Getenv("TTV_WINDOW_MAX_TEX")); err == nil && n > 0 {
		a.gl.maxTex = max(min(a.gl.maxTex, n), minMaxTex)
	}
	a.glName = version + " / " + C.GoString(C.gl_str(C.GL_RENDERER))
	C.gl_error() // a leitura acima não pode contar como erro do painel
}

// allocPanel recria as texturas para o tamanho atual do painel e envia o buffer inteiro.
// Roda com paintMu, na thread de render.
func (a *app) allocPanel() {
	a.tiles = panelTiles(a.pw, a.ph, a.gl)
	for len(a.texs) < len(a.tiles) {
		a.texs = append(a.texs, C.make_tex())
	}
	for len(a.texs) > len(a.tiles) {
		C.free_tex(a.texs[len(a.texs)-1])
		a.texs = a.texs[:len(a.texs)-1]
	}
	for i, t := range a.tiles {
		C.tex_alloc(a.texs[i], C.int(t.tw), C.int(t.th), a.bgraFlag())
	}
	a.uploadRect(0, 0, a.pw, a.ph)
	a.noteGLError(fmt.Sprintf("criar %d textura(s) para o painel de %dx%d", len(a.tiles), a.pw, a.ph))
	a.glCheck = true
}

// uploadRect manda às texturas um retângulo do buffer do painel, em pixels do painel.
func (a *app) uploadRect(x, y, w, h int) {
	for i, t := range a.tiles {
		cx, cy, cw, ch, ok := t.clip(x, y, w, h)
		if !ok {
			continue
		}
		tex, tx, ty := a.texs[i], C.int(cx-t.x), C.int(cy-t.y)
		if a.gl.bgra {
			C.tex_rect(tex, tx, ty, C.int(cw), C.int(ch), C.int(a.pw), unsafe.Pointer(&a.buf[(cy*a.pw+cx)*4]), 1)
			continue
		}
		px := rgbaRect(a.buf, a.pw, cx, cy, cw, ch)
		C.tex_rect(tex, tx, ty, C.int(cw), C.int(ch), C.int(cw), unsafe.Pointer(&px[0]), 0)
	}
}

func (a *app) bgraFlag() C.int {
	if a.gl.bgra {
		return 1
	}
	return 0
}

// noteGLError registra um erro do OpenGL em vez de deixar o painel sumir calado: uma
// textura recusada é desenhada como um retângulo branco.
func (a *app) noteGLError(doing string) {
	code := int(C.gl_error())
	if code == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "ttv-tui-window: OpenGL %s: erro 0x%04x ao %s\n", a.glName, code, doing)
	a.mu.Lock()
	a.glErrors++
	a.glLastError = code
	a.mu.Unlock()
}
