package main

import (
	"fmt"
	"image"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// glyph is a drawn rune: an alpha mask painted in the cell's colour, placed
// from the baseline, or a colour image (an emoji), placed from the cell's top
// left corner.
type glyph struct {
	mask  *image.Alpha
	color *image.RGBA
	x, y  int
}

type fontSet struct {
	faces   []font.Face
	fonts   []*sfnt.Font
	bold    font.Face
	boldF   *sfnt.Font
	cellW   int
	cellH   int
	base    int
	cache   map[uint64]*glyph
	sfntBuf sfnt.Buffer
	tofu    int
	// emojiPaths are the colour emoji fonts, opened on the first rune no other
	// font has: most screens never need the 10 MB of Noto Color Emoji.
	emojiPaths []string
	emoji      *colorFont
	emojiTried bool
}

func loadFace(path string, px float64) (font.Face, *sfnt.Font, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var f *sfnt.Font
	if strings.HasSuffix(path, ".ttc") {
		c, err := opentype.ParseCollection(b)
		if err != nil {
			return nil, nil, err
		}
		if f, err = c.Font(0); err != nil {
			return nil, nil, err
		}
	} else if f, err = opentype.Parse(b); err != nil {
		return nil, nil, err
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	return face, f, err
}

// fontFiles lists font files in order of preference; missing ones are skipped.
// systemFonts fills it for each platform: fontconfig on Linux and the BSDs, the
// system font folders on Windows and macOS.
type fontFiles struct {
	regular, bold, fallback []string
	// emoji are colour bitmap fonts (CBDT/CBLC), the last resort for a rune.
	emoji []string
}

// loadFirst opens the first file in paths that parses.
func loadFirst(paths []string, px float64) (font.Face, *sfnt.Font, error) {
	err := fmt.Errorf("none of %q", paths)
	for _, p := range paths {
		var f font.Face
		var sf *sfnt.Font
		if f, sf, err = loadFace(p, px); err == nil {
			return f, sf, nil
		}
	}
	return nil, nil, err
}

func newFontSet(px float64) (*fontSet, error) {
	files := systemFonts()
	fs := &fontSet{cache: map[uint64]*glyph{}, emojiPaths: files.emoji}
	reg, regF, err := loadFirst(files.regular, px)
	if err != nil {
		return nil, fmt.Errorf("monospace font: %w", err)
	}
	fs.faces, fs.fonts = []font.Face{reg}, []*sfnt.Font{regF}
	for _, p := range files.fallback {
		if f, sf, err := loadFace(p, px); err == nil {
			fs.faces, fs.fonts = append(fs.faces, f), append(fs.fonts, sf)
		}
	}
	if b, bf, err := loadFirst(files.bold, px); err == nil {
		fs.bold, fs.boldF = b, bf
	}
	fs.cellW, fs.cellH, fs.base, err = cellMetrics(reg)
	if err != nil {
		fs.close()
		return nil, err
	}
	return fs, nil
}

func cellMetrics(reg font.Face) (int, int, int, error) {
	m := reg.Metrics()
	adv, ok := reg.GlyphAdvance('M')
	if !ok || adv.Round() <= 0 || (m.Ascent+m.Descent).Ceil() <= 0 {
		return 0, 0, 0, fmt.Errorf("monospace font: invalid cell metrics or missing M")
	}
	return adv.Round(), (m.Ascent + m.Descent).Ceil(), m.Ascent.Ceil(), nil
}

// get returns the glyph of r for a cell span cells wide (1 or 2), or nil when no
// font has it. Only a colour emoji depends on the span: it is scaled to the cells.
func (fs *fontSet) get(r rune, bold bool, span int) *glyph {
	key := uint64(r) << 2
	if bold {
		key |= 1
	}
	if span > 1 {
		key |= 2
	}
	if g, ok := fs.cache[key]; ok {
		return g
	}
	var face font.Face
	if bold && fs.bold != nil {
		if i, _ := fs.boldF.GlyphIndex(&fs.sfntBuf, r); i != 0 {
			face = fs.bold
		}
	}
	if face == nil {
		for i, f := range fs.fonts {
			if gi, _ := f.GlyphIndex(&fs.sfntBuf, r); gi != 0 {
				face = fs.faces[i]
				break
			}
		}
	}
	if face == nil {
		g := fs.emojiGlyph(r, span)
		if g == nil {
			fs.tofu++
		}
		fs.cache[key] = g
		return g
	}
	dr, mask, mp, _, ok := face.Glyph(fixed.P(0, 0), r)
	if !ok || dr.Empty() {
		fs.cache[key] = &glyph{}
		return fs.cache[key]
	}
	a := image.NewAlpha(image.Rect(0, 0, dr.Dx(), dr.Dy()))
	for y := 0; y < dr.Dy(); y++ {
		for x := 0; x < dr.Dx(); x++ {
			_, _, _, ca := mask.At(mp.X+x, mp.Y+y).RGBA()
			a.Pix[a.PixOffset(x, y)] = uint8(ca >> 8)
		}
	}
	g := &glyph{mask: a, x: dr.Min.X, y: dr.Min.Y}
	fs.cache[key] = g
	return g
}

// emojiGlyph draws r from the colour emoji font, sized to span cells.
func (fs *fontSet) emojiGlyph(r rune, span int) *glyph {
	if !fs.emojiTried {
		fs.emojiTried = true
		fs.emoji = loadColorFont(fs.emojiPaths)
	}
	if fs.emoji == nil {
		return nil
	}
	return fs.emoji.glyph(&fs.sfntBuf, r, fs.cellW*max(span, 1), fs.cellH)
}

func scaledFontSet(pt, scale float64) (*fontSet, error) {
	if scale <= 0 {
		scale = 1
	}
	return newFontSet(pt * 96.0 / 72.0 * scale)
}

func (fs *fontSet) close() {
	for _, f := range fs.faces {
		_ = f.Close()
	}
	if fs.bold != nil {
		_ = fs.bold.Close()
	}
}
