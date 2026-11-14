package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font/sfnt"
)

// colorFont reads the color bitmaps of an emoji font in the CBDT/CBLC format,
// the one Noto Color Emoji uses. sfnt maps the rune to a glyph, but it only
// draws outlines, and these glyphs are PNG images.
type colorFont struct {
	f    *sfnt.Font
	cbdt []byte
	subs []bitmapSub
}

// bitmapSub is one index subtable of the strike: the glyph range it covers, how
// it stores the offsets and the format of the images.
type bitmapSub struct {
	first, last              uint16
	indexFormat, imageFormat uint16
	imageOffset              uint32
	// index is the subtable after its header, up to the end of CBLC.
	index []byte
}

var errNoColorBitmaps = errors.New("font has no CBDT/CBLC color bitmaps")

// emojiFonts keeps each font file parsed once: a new fontSet, made on every
// scale change, reuses it. A colorFont is never changed after parsing.
var emojiFonts = struct {
	sync.Mutex
	byPath map[string]*colorFont
}{byPath: map[string]*colorFont{}}

// loadColorFont opens the first file in paths with color bitmaps, or nil.
func loadColorFont(paths []string) *colorFont {
	emojiFonts.Lock()
	defer emojiFonts.Unlock()
	for _, p := range paths {
		if p == "" {
			continue
		}
		if cf, ok := emojiFonts.byPath[p]; ok {
			if cf != nil {
				return cf
			}
			continue
		}
		b, err := os.ReadFile(p)
		var cf *colorFont
		if err == nil {
			cf, _ = parseColorFont(b)
		}
		emojiFonts.byPath[p] = cf
		if cf != nil {
			return cf
		}
	}
	return nil
}

func be16(b []byte, off int) (uint16, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint16(b[off:]), true
}

func be32(b []byte, off int) (uint32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[off:]), true
}

// sfntTable returns the bytes of a table of a single font file (not a collection).
func sfntTable(b []byte, tag string) []byte {
	n, ok := be16(b, 4)
	if !ok {
		return nil
	}
	for i := range int(n) {
		rec := 12 + 16*i
		if rec+16 > len(b) {
			return nil
		}
		if string(b[rec:rec+4]) != tag {
			continue
		}
		off, _ := be32(b, rec+8)
		size, _ := be32(b, rec+12)
		if uint64(off)+uint64(size) > uint64(len(b)) {
			return nil
		}
		return b[off : off+size]
	}
	return nil
}

// parseColorFont reads the cmap and the largest bitmap strike of an emoji font.
func parseColorFont(b []byte) (*colorFont, error) {
	cblc, cbdt := sfntTable(b, "CBLC"), sfntTable(b, "CBDT")
	if cblc == nil || cbdt == nil {
		return nil, errNoColorBitmaps
	}
	f, err := sfnt.Parse(b)
	if err != nil {
		return nil, err
	}
	sizes, ok := be32(cblc, 4)
	if !ok {
		return nil, errNoColorBitmaps
	}
	// The largest strike: downscaling it gives the sharpest small emoji.
	best, bestPPEM := -1, -1
	for i := range int(sizes) {
		rec := 8 + 48*i
		if rec+48 > len(cblc) {
			break
		}
		if ppem := int(cblc[rec+45]); ppem > bestPPEM {
			best, bestPPEM = rec, ppem
		}
	}
	if best < 0 {
		return nil, errNoColorBitmaps
	}
	cf := &colorFont{f: f, cbdt: cbdt, subs: strikeSubtables(cblc, best)}
	if len(cf.subs) == 0 {
		return nil, errNoColorBitmaps
	}
	return cf, nil
}

// strikeSubtables reads the index subtables of the strike whose record starts at
// rec in CBLC.
func strikeSubtables(cblc []byte, rec int) []bitmapSub {
	arrayOff, _ := be32(cblc, rec)
	count, _ := be32(cblc, rec+8)
	var subs []bitmapSub
	// count comes from the file: the loop stops at the end of CBLC, not at count.
	// Offsets are summed in uint64 so a 32-bit int cannot wrap them.
	for i := uint64(0); i < uint64(count); i++ {
		entry := uint64(arrayOff) + 8*i
		if entry+8 > uint64(len(cblc)) {
			break
		}
		first, _ := be16(cblc, int(entry))
		last, _ := be16(cblc, int(entry)+2)
		add, _ := be32(cblc, int(entry)+4)
		head := uint64(arrayOff) + uint64(add)
		if last < first || head+8 > uint64(len(cblc)) {
			continue
		}
		h := int(head)
		indexFormat, _ := be16(cblc, h)
		imageFormat, _ := be16(cblc, h+2)
		imageOffset, _ := be32(cblc, h+4)
		subs = append(subs, bitmapSub{
			first: first, last: last, indexFormat: indexFormat, imageFormat: imageFormat,
			imageOffset: imageOffset, index: cblc[h+8:],
		})
	}
	return subs
}

// glyphData returns the CBDT record bytes for glyph gi, or false if absent.
func (s bitmapSub) glyphData(gi uint16, cbdt []byte) ([]byte, bool) {
	if gi < s.first || gi > s.last {
		return nil, false
	}
	i := int(gi - s.first)
	// start and end are uint64: a size read from the file times the glyph's
	// position must not wrap before it is checked against CBDT.
	var start, end uint64
	ok := false
	switch s.indexFormat {
	case 1:
		a, okA := be32(s.index, 4*i)
		b, okB := be32(s.index, 4*i+4)
		start, end, ok = uint64(a), uint64(b), okA && okB
	case 2:
		size, okS := be32(s.index, 0)
		start, end, ok = uint64(size)*uint64(i), uint64(size)*uint64(i+1), okS
	case 3:
		a, okA := be16(s.index, 2*i)
		b, okB := be16(s.index, 2*i+2)
		start, end, ok = uint64(a), uint64(b), okA && okB
	case 4:
		// n comes from the file: the loop stops where the index ends.
		n, okN := be32(s.index, 0)
		for j := 0; okN && j < int(min(uint64(n), uint64(len(s.index)))); j++ {
			rec := 4 + 4*j
			if rec+4 > len(s.index) {
				break
			}
			if id, _ := be16(s.index, rec); id != gi {
				continue
			}
			a, _ := be16(s.index, rec+2)
			b, okB := be16(s.index, rec+6)
			start, end, ok = uint64(a), uint64(b), okB
			break
		}
	case 5:
		size, okS := be32(s.index, 0)
		n, okN := be32(s.index, 12)
		for j := 0; okS && okN && j < int(min(uint64(n), uint64(len(s.index)))); j++ {
			rec := 16 + 2*j
			if rec+2 > len(s.index) {
				break
			}
			if id, _ := be16(s.index, rec); id == gi {
				start, end, ok = uint64(size)*uint64(j), uint64(size)*uint64(j+1), true
				break
			}
		}
	}
	lo, hi := uint64(s.imageOffset)+start, uint64(s.imageOffset)+end
	if !ok || end <= start || hi > uint64(len(cbdt)) {
		return nil, false
	}
	return cbdt[lo:hi], true
}

// pngOf takes the PNG out of a CBDT glyph record.
func pngOf(format uint16, rec []byte) []byte {
	skip := 0
	switch format {
	case 17: // small metrics, 5 bytes
		skip = 5
	case 18: // big metrics, 8 bytes
		skip = 8
	case 19: // metrics in CBLC
	default:
		return nil
	}
	size, ok := be32(rec, skip)
	if !ok || uint64(skip+4)+uint64(size) > uint64(len(rec)) {
		return nil
	}
	return rec[skip+4 : skip+4+int(size)]
}

// maxGlyphSide and maxGlyphPixels cap the PNG a glyph may declare. The font is
// read from the system and not trusted: its IHDR could ask for 100000×100000
// pixels, and png.Decode would try to allocate them.
const (
	maxGlyphSide   = 4096
	maxGlyphPixels = 16 << 20
)

// decodeGlyphPNG decodes data after checking the size its header declares.
func decodeGlyphPNG(data []byte) image.Image {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > maxGlyphSide || cfg.Height > maxGlyphSide ||
		uint64(cfg.Width)*uint64(cfg.Height) > maxGlyphPixels {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// glyph draws rune r to fit w×h pixels, centered, or returns nil when the font
// has no bitmap for it. buf is the caller's sfnt scratch buffer.
func (cf *colorFont) glyph(buf *sfnt.Buffer, r rune, w, h int) *glyph {
	gi, err := cf.f.GlyphIndex(buf, r)
	if err != nil || gi == 0 || w <= 0 || h <= 0 {
		return nil
	}
	return cf.glyphAt(uint16(gi), w, h)
}

// glyphAt draws glyph gi. Subtables may overlap: one that covers gi without a
// bitmap for it hands over to the next.
func (cf *colorFont) glyphAt(gi uint16, w, h int) *glyph {
	for _, s := range cf.subs {
		if gi < s.first || gi > s.last {
			continue
		}
		rec, ok := s.glyphData(gi, cf.cbdt)
		if !ok {
			continue
		}
		data := pngOf(s.imageFormat, rec)
		if data == nil {
			continue
		}
		img := decodeGlyphPNG(data)
		if img == nil {
			continue
		}
		return fitImage(img, w, h)
	}
	return nil
}

// fitImage scales img to fit w×h, keeping its shape, centered in that box.
func fitImage(img image.Image, w, h int) *glyph {
	iw, ih := int64(img.Bounds().Dx()), int64(img.Bounds().Dy())
	if iw <= 0 || ih <= 0 || w <= 0 || h <= 0 {
		return nil
	}
	// In int64 the products cannot wrap, even on a 32-bit int.
	dw, dh := int64(w), ih*int64(w)/iw
	if dh > int64(h) {
		dw, dh = iw*int64(h)/ih, int64(h)
	}
	dw, dh = max(dw, 1), max(dh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, int(dw), int(dh)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return &glyph{color: dst, x: (w - int(dw)) / 2, y: (h - int(dh)) / 2}
}
