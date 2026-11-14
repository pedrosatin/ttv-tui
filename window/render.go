package main

import (
	"image/color"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type cellKey struct {
	content string
	fg, bg  uint32
	attrs   uint8
	under   bool
	width   int
	cursor  bool
	hole    bool
	glass   bool // chat sobre o vídeo: fundo translúcido, texto opaco
	veil    uint8
}

func toRGB(c color.Color) uint32 {
	switch v := c.(type) {
	case nil:
		return defaultColor
	case ansi.BasicColor:
		return palette[v&15]
	case ansi.IndexedColor:
		if v < 16 {
			return palette[v]
		}
	}
	r, g, b, _ := c.RGBA()
	return (r>>8)<<16 | (g>>8)<<8 | b>>8
}

func (a *app) keyAt(x, y, cx, cy int, showCur bool) cellKey {
	c := a.term.CellAt(x, y)
	hole := a.holeOn && x >= a.host.Col-1 && x < a.host.Col-1+a.host.Cols && y >= a.host.Row-1 && y < a.host.Row-1+a.host.Rows
	inChat := x >= a.host.Col-1+a.host.Cols-a.host.OverlayCols
	if a.host.OverlayLeft {
		inChat = x < a.host.Col-1+a.host.OverlayCols
	}
	glass := hole && a.host.OverlayCols > 0 && inChat
	hole = hole && !glass
	veil := uint8(0)
	if glass {
		veil = uint8(a.host.OverlayOpacity * 255 / 100)
	}
	if c == nil {
		return cellKey{content: " ", fg: defaultColor, bg: defaultColor, width: 1, hole: hole, glass: glass, veil: veil}
	}
	return cellKey{
		hole:    hole,
		glass:   glass,
		veil:    veil,
		content: c.Content,
		fg:      toRGB(c.Style.Fg),
		bg:      toRGB(c.Style.Bg),
		attrs:   c.Style.Attrs,
		under:   c.Style.Underline != 0,
		width:   c.Width,
		cursor:  showCur && x == cx && y == cy,
	}
}

// paint redesenha só as células que mudaram e devolve o intervalo de linhas sujo.
func (a *app) paint() (int, int) {
	t0 := time.Now()
	y0, y1 := a.rows, -1
	x0, x1 := a.cols, -1
	pos := a.term.CursorPosition()
	show := !a.term.CursorHidden()
	for y := 0; y < a.rows; y++ {
		textEnd := a.glassRowEnd(y)
		for x := 0; x < a.cols; x++ {
			k := a.keyAt(x, y, pos.X, pos.Y, show)
			if k.glass && (textEnd < 0 || x > textEnd+1) { // o fundo só existe atrás do texto
				k.glass, k.hole, k.veil = false, true, 0
			}
			i := y*a.cols + x
			if a.prev[i] == k {
				continue
			}
			a.prev[i] = k
			a.drawCell(x, y, k)
			if y < y0 {
				y0 = y
			}
			if y > y1 {
				y1 = y
			}
			// Uma célula de folga: glifo largo e traço que passa do limite da célula.
			x0, x1 = min(x0, max(x-1, 0)), max(x1, min(x+k.width, a.cols-1))
		}
	}
	a.dirtyX0, a.dirtyX1 = min(a.dirtyX0, x0), max(a.dirtyX1, x1)
	a.mu.Lock()
	a.paintNs += time.Since(t0).Nanoseconds()
	if y1 >= 0 {
		a.panelPaints++
	}
	a.mu.Unlock()
	return y0, y1
}

// glassRowEnd é a última coluna com texto do chat sobreposto nesta linha, ou -1.
func (a *app) glassRowEnd(y int) int {
	if !a.holeOn || a.host.OverlayCols == 0 || y < a.host.Row-1 || y >= a.host.Row-1+a.host.Rows {
		return -1
	}
	start := a.host.Col - 1
	if !a.host.OverlayLeft {
		start += a.host.Cols - a.host.OverlayCols
	}
	for x := start + a.host.OverlayCols - 1; x >= start; x-- {
		c := a.term.CellAt(x, y)
		if c == nil {
			continue
		}
		if (c.Content != "" && c.Content != " ") || c.Width == 0 || toRGB(c.Style.Bg) != defaultColor {
			return x
		}
	}
	return -1
}

// outlineGlyph escurece 1 px em volta do glifo, para o texto se destacar sobre o
// vídeo claro. Fica dentro da célula: a seguinte repintaria o que passasse dela.
func (a *app) outlineGlyph(g *glyph, px0, py0, span int) {
	cw, ch := a.fs.cellW, a.fs.cellH
	mw, mh := g.mask.Rect.Dx(), g.mask.Rect.Dy()
	for my := 0; my < mh; my++ {
		for mx := 0; mx < mw; mx++ {
			al := uint32(g.mask.Pix[g.mask.PixOffset(mx, my)])
			if al < 96 {
				continue
			}
			for ny := -1; ny <= 1; ny++ {
				for nx := -1; nx <= 1; nx++ {
					tx, ty := px0+g.x+mx+nx, py0+a.fs.base+g.y+my+ny
					if (nx == 0 && ny == 0) || tx < px0 || ty < py0 || tx >= px0+cw*span || ty >= py0+ch || tx >= a.pw || ty >= a.ph {
						continue
					}
					off := ty*a.pw*4 + tx*4
					for i := range 3 {
						a.buf[off+i] = byte(uint32(a.buf[off+i]) * (255 - al) / 255)
					}
					a.buf[off+3] = byte(al + uint32(a.buf[off+3])*(255-al)/255)
				}
			}
		}
	}
}

func (a *app) drawCell(x, y int, k cellKey) {
	cw, ch := a.fs.cellW, a.fs.cellH
	if k.hole { // transparente: o vídeo aparece por baixo do painel
		for yy := y * ch; yy < (y+1)*ch && yy < a.ph; yy++ {
			off := yy*a.pw*4 + x*cw*4
			clear(a.buf[off : off+cw*4])
		}
		return
	}
	fr, fg, fb := uint32(fgR), uint32(fgG), uint32(fgB)
	br, bg, bb := uint32(bgR), uint32(bgG), uint32(bgB)
	if k.fg != defaultColor {
		fr, fg, fb = k.fg>>16&255, k.fg>>8&255, k.fg&255
	}
	if k.bg != defaultColor {
		br, bg, bb = k.bg>>16&255, k.bg>>8&255, k.bg&255
	}
	if k.attrs&uv.AttrReverse != 0 || k.cursor {
		fr, br, fg, bg, fb, bb = br, fr, bg, fg, bb, fb
	}
	if k.attrs&uv.AttrFaint != 0 {
		fr, fg, fb = fr*2/3, fg*2/3, fb*2/3
	}
	if k.width == 0 { // continuação de um caractere largo, já desenhado pela célula anterior
		return
	}
	span := 1
	if k.width == 2 {
		span = 2
	}
	stride := a.pw * 4
	px0, py0 := x*cw, y*ch
	bgA := uint32(255)
	if k.glass { // a textura é pré-multiplicada, então a cor cai junto com o alfa
		bgA = uint32(k.veil)
		if k.bg == defaultColor && k.attrs&uv.AttrReverse == 0 && !k.cursor {
			br, bg, bb = 0, 0, 0
		} else {
			bgA = max(bgA, glassSolidAlpha)
		}
		br, bg, bb = br*bgA/255, bg*bgA/255, bb*bgA/255
	}
	for yy := py0; yy < py0+ch && yy < a.ph; yy++ {
		off := yy*stride + px0*4
		for xx := 0; xx < cw*span && px0+xx < a.pw; xx++ {
			a.buf[off+xx*4] = byte(bb)
			a.buf[off+xx*4+1] = byte(bg)
			a.buf[off+xx*4+2] = byte(br)
			a.buf[off+xx*4+3] = byte(bgA)
		}
	}
	if k.content == "" || k.content == " " {
		return
	}
	r := []rune(k.content)[0]
	g := a.fs.get(r, k.attrs&uv.AttrBold != 0, span)
	if g == nil { // sem glifo: caixa marca a lacuna de fonte
		for xx := 1; xx < cw*span-1; xx++ {
			a.setPx(px0+xx, py0+2, fr, fg, fb)
			a.setPx(px0+xx, py0+ch-3, fr, fg, fb)
		}
		for yy := 2; yy < ch-2; yy++ {
			a.setPx(px0+1, py0+yy, fr, fg, fb)
			a.setPx(px0+cw*span-2, py0+yy, fr, fg, fb)
		}
		return
	}
	if g.color != nil {
		a.drawColorGlyph(g, px0, py0)
	}
	if g.mask != nil {
		if k.glass {
			a.outlineGlyph(g, px0, py0, span)
		}
		mw, mh := g.mask.Rect.Dx(), g.mask.Rect.Dy()
		for my := 0; my < mh; my++ {
			for mx := 0; mx < mw; mx++ {
				al := uint32(g.mask.Pix[g.mask.PixOffset(mx, my)])
				if al == 0 {
					continue
				}
				dx, dy := px0+g.x+mx, py0+a.fs.base+g.y+my
				if dx < 0 || dy < 0 || dx >= a.pw || dy >= a.ph {
					continue
				}
				off := dy*stride + dx*4
				a.buf[off] = byte((uint32(a.buf[off])*(255-al) + fb*al) / 255)
				a.buf[off+1] = byte((uint32(a.buf[off+1])*(255-al) + fg*al) / 255)
				a.buf[off+2] = byte((uint32(a.buf[off+2])*(255-al) + fr*al) / 255)
				a.buf[off+3] = byte((uint32(a.buf[off+3])*(255-al) + 255*al) / 255)
			}
		}
	}
	if k.under {
		for xx := 0; xx < cw*span; xx++ {
			a.setPx(px0+xx, py0+a.fs.base+1, fr, fg, fb)
		}
	}
}

// drawColorGlyph compõe a imagem de um emoji sobre a célula. As duas são
// pré-multiplicadas: o pixel novo é a fonte mais o fundo que ela deixa passar.
func (a *app) drawColorGlyph(g *glyph, px0, py0 int) {
	img := g.color
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for y := range h {
		dy := py0 + g.y + y
		if dy < 0 || dy >= a.ph {
			continue
		}
		src := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := range w {
			sa := uint32(src[x*4+3])
			dx := px0 + g.x + x
			if sa == 0 || dx < 0 || dx >= a.pw {
				continue
			}
			off := dy*a.pw*4 + dx*4
			keep := 255 - sa
			a.buf[off] = byte(uint32(src[x*4+2]) + uint32(a.buf[off])*keep/255)
			a.buf[off+1] = byte(uint32(src[x*4+1]) + uint32(a.buf[off+1])*keep/255)
			a.buf[off+2] = byte(uint32(src[x*4]) + uint32(a.buf[off+2])*keep/255)
			a.buf[off+3] = byte(sa + uint32(a.buf[off+3])*keep/255)
		}
	}
}

func (a *app) setPx(x, y int, r, g, b uint32) {
	if x < 0 || y < 0 || x >= a.pw || y >= a.ph {
		return
	}
	off := y*a.pw*4 + x*4
	a.buf[off], a.buf[off+1], a.buf[off+2], a.buf[off+3] = byte(b), byte(g), byte(r), 255
}
