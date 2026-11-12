package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestParseGLCaps(t *testing.T) {
	const gdi = "GL_WIN_swap_hint GL_EXT_bgra GL_EXT_paletted_texture"
	for _, tc := range []struct {
		name, version, ext string
		maxTex             int
		want               glCaps
	}{
		{"genérico do Windows", "1.1.0", gdi, 1024, glCaps{npot: false, bgra: true, maxTex: 1024}},
		{"1.1 sem extensão", "1.1.0", "", 1024, glCaps{maxTex: 1024}},
		{"1.2 já tem BGRA", "1.2", "", 2048, glCaps{bgra: true, maxTex: 2048}},
		{"1.5 com NPOT por extensão", "1.5 Mesa", "GL_ARB_texture_non_power_of_two", 4096, glCaps{npot: true, bgra: true, maxTex: 4096}},
		{"2.0 já tem NPOT", "2.0", "", 4096, glCaps{npot: true, bgra: true, maxTex: 4096}},
		{"Mesa atual", "4.6 (Compatibility Profile) Mesa 26.2.5", "GL_EXT_bgra", 16384, glCaps{npot: true, bgra: true, maxTex: 16384}},
		{"macOS legado", "2.1 Metal - 89.3", "", 16384, glCaps{npot: true, bgra: true, maxTex: 16384}},
		{"nome parecido não vale", "1.1.0", "GL_ARB_texture_non_power_of_two_x GL_EXT_bgra_y", 1024, glCaps{maxTex: 1024}},
		{"versão ilegível conta como 1.1", "OpenGL ES 3.2", "", 8192, glCaps{maxTex: 8192}},
		{"versão vazia", "", "", 0, glCaps{maxTex: minMaxTex}},
		{"limite absurdo", "1.1.0", gdi, -1, glCaps{bgra: true, maxTex: minMaxTex}},
	} {
		if got := parseGLCaps(tc.version, tc.ext, tc.maxTex); got != tc.want {
			t.Errorf("%s: parseGLCaps(%q, %q, %d) = %+v, want %+v", tc.name, tc.version, tc.ext, tc.maxTex, got, tc.want)
		}
	}
}

func TestPanelTiles(t *testing.T) {
	modern := glCaps{npot: true, bgra: true, maxTex: 16384}
	gdi := glCaps{npot: false, bgra: true, maxTex: 1024}
	for _, tc := range []struct {
		name   string
		pw, ph int
		caps   glCaps
		want   []panelTile
	}{
		{
			"OpenGL moderno: uma textura do tamanho do painel", 1274, 714, modern,
			[]panelTile{{x: 0, y: 0, w: 1274, h: 714, tw: 1274, th: 714, u: 1, v: 1}},
		},
		{
			"1.1 com o painel dentro do limite: lados arredondados para cima", 800, 600, gdi,
			[]panelTile{{x: 0, y: 0, w: 800, h: 600, tw: 1024, th: 1024, u: 800.0 / 1024, v: 600.0 / 1024}},
		},
		{
			// A janela padrão de 1280x720, com célula de 7x17, no genérico do Windows.
			"1.1 com o painel mais largo que o limite: dois ladrilhos", 1274, 714, gdi,
			[]panelTile{
				{x: 0, y: 0, w: 1024, h: 714, tw: 1024, th: 1024, u: 1, v: 714.0 / 1024},
				{x: 1024, y: 0, w: 250, h: 714, tw: 256, th: 1024, u: 250.0 / 256, v: 714.0 / 1024},
			},
		},
		{
			"1.1 em tela cheia de 1920x1080: grade de 2 por 2", 1920, 1080, gdi,
			[]panelTile{
				{x: 0, y: 0, w: 1024, h: 1024, tw: 1024, th: 1024, u: 1, v: 1},
				{x: 1024, y: 0, w: 896, h: 1024, tw: 1024, th: 1024, u: 896.0 / 1024, v: 1},
				{x: 0, y: 1024, w: 1024, h: 56, tw: 1024, th: 64, u: 1, v: 56.0 / 64},
				{x: 1024, y: 1024, w: 896, h: 56, tw: 1024, th: 64, u: 896.0 / 1024, v: 56.0 / 64},
			},
		},
		{
			"lado já potência de 2 não cresce", 512, 256, gdi,
			[]panelTile{{x: 0, y: 0, w: 512, h: 256, tw: 512, th: 256, u: 1, v: 1}},
		},
		{
			"limite fora de potência de 2 desce para a potência anterior", 1200, 100, glCaps{maxTex: 1000},
			[]panelTile{
				{x: 0, y: 0, w: 512, h: 100, tw: 512, th: 128, u: 1, v: 100.0 / 128},
				{x: 512, y: 0, w: 512, h: 100, tw: 512, th: 128, u: 1, v: 100.0 / 128},
				{x: 1024, y: 0, w: 176, h: 100, tw: 256, th: 128, u: 176.0 / 256, v: 100.0 / 128},
			},
		},
		{
			"OpenGL moderno com o painel maior que o limite: ladrilhos exatos", 5000, 100, glCaps{npot: true, maxTex: 4096},
			[]panelTile{
				{x: 0, y: 0, w: 4096, h: 100, tw: 4096, th: 100, u: 1, v: 1},
				{x: 4096, y: 0, w: 904, h: 100, tw: 904, th: 100, u: 1, v: 1},
			},
		},
		{"painel vazio", 0, 714, gdi, nil},
	} {
		got := panelTiles(tc.pw, tc.ph, tc.caps)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: panelTiles(%d, %d) =\n %+v, want\n %+v", tc.name, tc.pw, tc.ph, got, tc.want)
		}
	}
}

// Qualquer que seja o OpenGL, os ladrilhos cobrem cada pixel do painel uma única vez, cabem
// na textura alocada e a textura respeita o que o contexto aceita.
func TestPanelTilesCoverThePanel(t *testing.T) {
	isPow2 := func(n int) bool { return n > 0 && n&(n-1) == 0 }
	for _, caps := range []glCaps{
		{npot: false, maxTex: 64}, {npot: false, maxTex: 1024}, {npot: false, maxTex: 1000},
		{npot: true, maxTex: 1024}, {npot: true, maxTex: 16384}, {npot: false, maxTex: 0},
	} {
		limit := max(caps.maxTex, minMaxTex)
		for _, size := range [][2]int{{1274, 714}, {1, 1}, {64, 64}, {65, 63}, {1920, 1080}, {3836, 2142}, {1025, 1023}} {
			pw, ph := size[0], size[1]
			area := 0
			for _, tile := range panelTiles(pw, ph, caps) {
				area += tile.w * tile.h
				if tile.w <= 0 || tile.h <= 0 || tile.x < 0 || tile.y < 0 || tile.x+tile.w > pw || tile.y+tile.h > ph {
					t.Fatalf("%+v %dx%d: ladrilho fora do painel: %+v", caps, pw, ph, tile)
				}
				if tile.tw < tile.w || tile.th < tile.h || tile.tw > limit || tile.th > limit {
					t.Fatalf("%+v %dx%d: textura não comporta o ladrilho ou passa do limite: %+v", caps, pw, ph, tile)
				}
				if !caps.npot && (!isPow2(tile.tw) || !isPow2(tile.th)) {
					t.Fatalf("%+v %dx%d: textura fora de potência de 2: %+v", caps, pw, ph, tile)
				}
				if tile.u != float32(tile.w)/float32(tile.tw) || tile.v != float32(tile.h)/float32(tile.th) {
					t.Fatalf("%+v %dx%d: coordenadas não param no fim do painel: %+v", caps, pw, ph, tile)
				}
				// Um vizinho à direita ou embaixo começa onde este termina: sem sobreposição.
				for _, other := range panelTiles(pw, ph, caps) {
					if other == tile {
						continue
					}
					if _, _, _, _, ok := tile.clip(other.x, other.y, other.w, other.h); ok {
						t.Fatalf("%+v %dx%d: ladrilhos sobrepostos: %+v e %+v", caps, pw, ph, tile, other)
					}
				}
			}
			if area != pw*ph {
				t.Fatalf("%+v %dx%d: os ladrilhos cobrem %d pixels, want %d", caps, pw, ph, area, pw*ph)
			}
		}
	}
}

func TestPanelTileClip(t *testing.T) {
	tile := panelTile{x: 1024, y: 0, w: 250, h: 714, tw: 256, th: 1024}
	for _, tc := range []struct {
		name       string
		x, y, w, h int
		want       [4]int
		ok         bool
	}{
		{"retângulo dentro do ladrilho", 1100, 17, 70, 34, [4]int{1100, 17, 70, 34}, true},
		{"retângulo que atravessa a emenda fica com a parte da direita", 973, 0, 91, 17, [4]int{1024, 0, 40, 17}, true},
		{"painel inteiro vira o ladrilho inteiro", 0, 0, 1274, 714, [4]int{1024, 0, 250, 714}, true},
		{"retângulo só no vizinho", 0, 0, 1024, 714, [4]int{}, false},
		{"retângulo que encosta na borda não entra", 1017, 0, 7, 17, [4]int{}, false},
	} {
		cx, cy, cw, ch, ok := tile.clip(tc.x, tc.y, tc.w, tc.h)
		if got := [4]int{cx, cy, cw, ch}; got != tc.want || ok != tc.ok {
			t.Errorf("%s: clip = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRGBARectSwapsBlueAndRed(t *testing.T) {
	// Buffer BGRA de 3x2 pixels; cada pixel é {B, G, R, A} = {10n, 10n+1, 10n+2, 10n+3}.
	buf := make([]byte, 3*2*4)
	for i := range buf {
		buf[i] = byte(i/4*10 + i%4)
	}
	got := rgbaRect(buf, 3, 1, 0, 2, 2) // as duas colunas da direita
	want := []byte{
		12, 11, 10, 13, 22, 21, 20, 23,
		42, 41, 40, 43, 52, 51, 50, 53,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("rgbaRect = %v, want %v", got, want)
	}
}
