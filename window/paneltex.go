package main

import "strings"

// glCaps é o que o contexto OpenGL aceita para a textura do painel. O genérico do
// Windows (OpenGL 1.1) não tem npot e limita a textura a 1024 pixels de lado.
type glCaps struct {
	npot   bool // lados fora de potência de 2: OpenGL 2.0 ou GL_ARB_texture_non_power_of_two
	bgra   bool // pixels em GL_BGRA: OpenGL 1.2 ou GL_EXT_bgra
	maxTex int  // GL_MAX_TEXTURE_SIZE
}

// minMaxTex é o menor GL_MAX_TEXTURE_SIZE que a especificação permite.
const minMaxTex = 64

// parseGLCaps lê GL_VERSION ("1.1.0", "4.6 (Compatibility Profile) Mesa 26.2.5") e
// GL_EXTENSIONS. Uma versão ilegível conta como 1.1, cujo caminho funciona em qualquer OpenGL.
func parseGLCaps(version, extensions string, maxTex int) glCaps {
	major, minor := glVersion(version)
	c := glCaps{
		npot:   major >= 2,
		bgra:   major > 1 || major == 1 && minor >= 2,
		maxTex: max(maxTex, minMaxTex),
	}
	for ext := range strings.FieldsSeq(extensions) {
		switch ext {
		case "GL_ARB_texture_non_power_of_two":
			c.npot = true
		case "GL_EXT_bgra":
			c.bgra = true
		}
	}
	return c
}

func glVersion(s string) (major, minor int) {
	num := func(s string) (n int, rest string, ok bool) {
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' && i < 4 {
			n = n*10 + int(s[i]-'0')
			i++
		}
		return n, s[i:], i > 0
	}
	major, rest, ok := num(s)
	if !ok || !strings.HasPrefix(rest, ".") {
		return 1, 1
	}
	minor, _, ok = num(rest[1:])
	if !ok {
		return 1, 1
	}
	return major, minor
}

// panelTile é um pedaço do painel guardado em uma textura.
type panelTile struct {
	x, y, w, h int     // retângulo dentro do painel, em pixels
	tw, th     int     // lados da textura alocada, que pode sobrar à direita e embaixo
	u, v       float32 // coordenadas de textura da borda direita e da borda de baixo
}

// panelTiles divide um painel de pw por ph pixels nas texturas que o OpenGL aceita. Com
// npot e o painel dentro do limite, sai uma textura do tamanho exato e coordenadas de 0 a
// 1. Sem npot, cada textura tem lados potência de 2 e as coordenadas param onde o painel
// acaba. Um painel maior que o limite vira uma grade de ladrilhos.
func panelTiles(pw, ph int, c glCaps) []panelTile {
	if pw <= 0 || ph <= 0 {
		return nil
	}
	side := max(c.maxTex, minMaxTex)
	if !c.npot {
		side = floorPow2(side)
	}
	alloc := func(n int) int {
		if c.npot {
			return n
		}
		return ceilPow2(n)
	}
	var tiles []panelTile
	for y := 0; y < ph; y += side {
		h := min(side, ph-y)
		for x := 0; x < pw; x += side {
			w := min(side, pw-x)
			tw, th := alloc(w), alloc(h)
			tiles = append(tiles, panelTile{
				x: x, y: y, w: w, h: h, tw: tw, th: th,
				u: float32(w) / float32(tw), v: float32(h) / float32(th),
			})
		}
	}
	return tiles
}

// clip devolve a parte do retângulo do painel que cai neste ladrilho.
func (t panelTile) clip(x, y, w, h int) (cx, cy, cw, ch int, ok bool) {
	x0, y0 := max(x, t.x), max(y, t.y)
	x1, y1 := min(x+w, t.x+t.w), min(y+h, t.y+t.h)
	if x0 >= x1 || y0 >= y1 {
		return 0, 0, 0, 0, false
	}
	return x0, y0, x1 - x0, y1 - y0, true
}

func floorPow2(n int) int {
	p := 1
	for p*2 <= n {
		p *= 2
	}
	return p
}

func ceilPow2(n int) int {
	p := 1
	for p < n {
		p *= 2
	}
	return p
}

// rgbaRect copia um retângulo do buffer BGRA do painel com azul e vermelho trocados, para
// o OpenGL que não recebe GL_BGRA. stride é a largura do buffer em pixels.
func rgbaRect(buf []byte, stride, x, y, w, h int) []byte {
	out := make([]byte, w*h*4)
	for row := range h {
		src := buf[((y+row)*stride+x)*4:][:w*4]
		dst := out[row*w*4:][:w*4]
		for i := 0; i < len(src); i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
		}
	}
	return out
}
