package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// notoColorEmoji é onde o Arch e o Debian instalam a fonte de emoji do Google.
var notoColorEmoji = []string{"/usr/share/fonts/noto/NotoColorEmoji.ttf", "/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf"}

// systemColorFont abre a Noto Color Emoji instalada e devolve também o caminho.
func systemColorFont(t *testing.T) (*colorFont, string) {
	t.Helper()
	for _, p := range notoColorEmoji {
		if _, err := os.Stat(p); err == nil {
			if cf := loadColorFont([]string{p}); cf != nil {
				return cf, p
			}
			t.Fatalf("%s existe e não abriu como fonte de emoji colorida", p)
		}
	}
	t.Skip("sem Noto Color Emoji nesta máquina")
	return nil, ""
}

// O 🔥 da Noto Color Emoji sai colorido e do tamanho pedido; um rune que a fonte
// não tem sai nil.
func TestColorFontDrawsEmojiInColor(t *testing.T) {
	cf, _ := systemColorFont(t)
	var buf sfnt.Buffer
	g := cf.glyph(&buf, '🔥', 20, 19)
	if g == nil || g.color == nil {
		t.Fatal("sem glifo para 🔥")
	}
	b := g.color.Bounds()
	if b.Dx() > 20 || b.Dy() > 19 || (b.Dx() != 20 && b.Dy() != 19) {
		t.Errorf("🔥 em %dx%d, quer caber justo em 20x19", b.Dx(), b.Dy())
	}
	if g.x < 0 || g.y < 0 || g.x+b.Dx() > 20 || g.y+b.Dy() > 19 {
		t.Errorf("🔥 fora da célula: deslocamento %d,%d", g.x, g.y)
	}
	orange := false
	for i := 0; i < len(g.color.Pix); i += 4 {
		r, gr, bl, a := g.color.Pix[i], g.color.Pix[i+1], g.color.Pix[i+2], g.color.Pix[i+3]
		if a > 200 && r > 200 && gr < 180 && bl < 100 {
			orange = true
			break
		}
	}
	if !orange {
		t.Error("o 🔥 não tem nenhum pixel laranja")
	}
	if g := cf.glyph(&buf, 'ж', 20, 19); g != nil {
		t.Error("a fonte de emoji desenhou ж")
	}
}

// Sem fonte de emoji o rune conta como tofu; com ela, não.
func TestFontSetFallsBackToColorEmoji(t *testing.T) {
	_, path := systemColorFont(t)
	plain := &fontSet{cache: map[uint64]*glyph{}, cellW: 10, cellH: 20}
	if g := plain.get('😂', false, 2); g != nil || plain.tofu != 1 {
		t.Fatalf("sem fonte de emoji: glifo %v, tofu %d", g, plain.tofu)
	}
	withEmoji := &fontSet{cache: map[uint64]*glyph{}, cellW: 10, cellH: 20, emojiPaths: []string{"", "/não/existe.ttf", path}}
	g := withEmoji.get('😂', false, 2)
	if g == nil || g.color == nil || withEmoji.tofu != 0 {
		t.Fatalf("com fonte de emoji: glifo %v, tofu %d", g, withEmoji.tofu)
	}
	if g.color.Bounds().Dx() > 20 {
		t.Errorf("emoji de duas células com %d px de largura", g.color.Bounds().Dx())
	}
	if one := withEmoji.get('😂', false, 1); one == nil || one.color.Bounds().Dx() > 10 {
		t.Error("o mesmo emoji numa célula não foi redimensionado para ela")
	}
}

// Um arquivo sem CBDT/CBLC não é fonte de emoji colorida.
func TestParseColorFontRejectsOutlineFont(t *testing.T) {
	if _, err := parseColorFont([]byte("não é fonte")); err == nil {
		t.Error("lixo aceito como fonte de emoji")
	}
	if cf := loadColorFont([]string{"/não/existe.ttf"}); cf != nil {
		t.Error("arquivo inexistente virou fonte de emoji")
	}
}

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Os cinco formatos de índice levam ao registro certo, e os três de imagem, ao PNG.
func TestBitmapIndexFormats(t *testing.T) {
	pngA := encodePNG(t, 4, 4)
	record := func(format uint16) []byte {
		var head []byte
		switch format {
		case 17:
			head = make([]byte, 5)
		case 18:
			head = make([]byte, 8)
		}
		size := binary.BigEndian.AppendUint32(nil, uint32(len(pngA)))
		return append(append(head, size...), pngA...)
	}
	for _, format := range []uint16{17, 18, 19} {
		rec := record(format)
		// O glifo 11 é o segundo da faixa 10–11: dois registros iguais em sequência.
		cbdt := append(append([]byte{0xEE, 0xEE}, rec...), rec...)
		n := uint32(len(rec))
		u32 := func(vs ...uint32) []byte {
			var b []byte
			for _, v := range vs {
				b = binary.BigEndian.AppendUint32(b, v)
			}
			return b
		}
		u16 := func(vs ...uint16) []byte {
			var b []byte
			for _, v := range vs {
				b = binary.BigEndian.AppendUint16(b, v)
			}
			return b
		}
		indexes := map[uint16][]byte{
			1: u32(0, n, 2*n),
			2: append(u32(n), make([]byte, 8)...),
			3: u16(0, uint16(n), uint16(2*n)),
			4: append(u32(2), u16(10, 0, 11, uint16(n), 0, uint16(2*n))...),
			5: append(append(append(u32(n), make([]byte, 8)...), u32(2)...), u16(10, 11)...),
		}
		for indexFormat, index := range indexes {
			s := bitmapSub{first: 10, last: 11, indexFormat: indexFormat, imageFormat: format, imageOffset: 2, index: index}
			got, ok := s.glyphData(11, cbdt)
			if !ok {
				t.Errorf("índice %d, imagem %d: glifo 11 não encontrado", indexFormat, format)
				continue
			}
			if p := pngOf(format, got); !bytes.Equal(p, pngA) {
				t.Errorf("índice %d, imagem %d: PNG errado (%d bytes)", indexFormat, format, len(p))
			}
		}
	}
	// Um índice que aponta para fora do CBDT não lê nada.
	s := bitmapSub{first: 1, last: 1, indexFormat: 1, imageFormat: 17, index: binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 0), 1<<30)}
	if _, ok := s.glyphData(1, make([]byte, 16)); ok {
		t.Error("registro fora do CBDT aceito")
	}
	if pngOf(17, []byte{0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF}) != nil {
		t.Error("PNG maior que o registro aceito")
	}
	if pngOf(1, pngA) != nil {
		t.Error("formato de imagem desconhecido aceito")
	}
}

// A imagem cabe na caixa sem deformar, centralizada.
func TestFitImageKeepsShape(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	g := fitImage(img, 20, 20)
	if b := g.color.Bounds(); b.Dx() != 20 || b.Dy() != 10 || g.x != 0 || g.y != 5 {
		t.Errorf("100x50 em 20x20: %dx%d em %d,%d; quer 20x10 em 0,5", b.Dx(), b.Dy(), g.x, g.y)
	}
	g = fitImage(image.NewRGBA(image.Rect(0, 0, 50, 100)), 20, 20)
	if b := g.color.Bounds(); b.Dx() != 10 || b.Dy() != 20 || g.x != 5 || g.y != 0 {
		t.Errorf("50x100 em 20x20: %dx%d em %d,%d; quer 10x20 em 5,0", b.Dx(), b.Dy(), g.x, g.y)
	}
}

// O emoji se compõe sobre o fundo da célula: opaco cobre, transparente deixa o
// fundo, e o buffer guarda BGRA.
func TestDrawColorGlyphComposites(t *testing.T) {
	a := &app{pw: 4, ph: 1, buf: make([]byte, 16)}
	for i := 0; i < 16; i += 4 {
		copy(a.buf[i:], []byte{10, 20, 30, 255})
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	img.Set(1, 0, color.RGBA{})
	a.drawColorGlyph(&glyph{color: img, x: 1}, 0, 0)
	want := []byte{10, 20, 30, 255, 50, 100, 200, 255, 10, 20, 30, 255, 10, 20, 30, 255}
	if !bytes.Equal(a.buf, want) {
		t.Errorf("buffer %v, quer %v", a.buf, want)
	}
}
