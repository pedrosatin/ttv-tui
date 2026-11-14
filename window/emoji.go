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
	arrayOff, _ := be32(cblc, best)
	count, _ := be32(cblc, best+8)
	cf := &colorFont{f: f, cbdt: cbdt}
	for i := range int(count) {
		entry := int(arrayOff) + 8*i
		first, ok1 := be16(cblc, entry)
		last, ok2 := be16(cblc, entry+2)
		add, ok3 := be32(cblc, entry+4)
		head := int(arrayOff) + int(add)
		indexFormat, ok4 := be16(cblc, head)
		imageFormat, ok5 := be16(cblc, head+2)
		imageOffset, ok6 := be32(cblc, head+4)
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || last < first || head+8 > len(cblc) {
			continue
		}
		cf.subs = append(cf.subs, bitmapSub{
			first: first, last: last, indexFormat: indexFormat, imageFormat: imageFormat,
			imageOffset: imageOffset, index: cblc[head+8:],
		})
	}
	if len(cf.subs) == 0 {
		return nil, errNoColorBitmaps
	}
	return cf, nil
}

// glyphData returns the bytes of glyph gi in CBDT and the image format.
func (s bitmapSub) glyphData(gi uint16, cbdt []byte) ([]byte, bool) {
	i := int(gi - s.first)
	var start, end uint32
	var ok1, ok2 bool
	switch s.indexFormat {
	case 1:
		start, ok1 = be32(s.index, 4*i)
		end, ok2 = be32(s.index, 4*i+4)
	case 2:
		var size uint32
		size, ok1 = be32(s.index, 0)
		start, end, ok2 = size*uint32(i), size*uint32(i+1), true
	case 3:
		var a, b uint16
		a, ok1 = be16(s.index, 2*i)
		b, ok2 = be16(s.index, 2*i+2)
		start, end = uint32(a), uint32(b)
	case 4:
		n, ok := be32(s.index, 0)
		for j := 0; ok && j < int(n); j++ {
			id, _ := be16(s.index, 4+4*j)
			if id != gi {
				continue
			}
			a, okA := be16(s.index, 4+4*j+2)
			b, okB := be16(s.index, 4+4*(j+1)+2)
			start, end, ok1, ok2 = uint32(a), uint32(b), okA, okB
			break
		}
	case 5:
		size, ok := be32(s.index, 0)
		n, okN := be32(s.index, 12)
		for j := 0; ok && okN && j < int(n); j++ {
			if id, _ := be16(s.index, 16+2*j); id == gi {
				start, end, ok1, ok2 = size*uint32(j), size*uint32(j+1), true, true
				break
			}
		}
	}
	lo, hi := uint64(s.imageOffset)+uint64(start), uint64(s.imageOffset)+uint64(end)
	if !ok1 || !ok2 || end <= start || hi > uint64(len(cbdt)) {
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

// glyph draws rune r to fit w×h pixels, centred, or returns nil when the font
// has no bitmap for it. buf is the caller's sfnt scratch buffer.
func (cf *colorFont) glyph(buf *sfnt.Buffer, r rune, w, h int) *glyph {
	gi, err := cf.f.GlyphIndex(buf, r)
	if err != nil || gi == 0 || w <= 0 || h <= 0 {
		return nil
	}
	for _, s := range cf.subs {
		if uint16(gi) < s.first || uint16(gi) > s.last {
			continue
		}
		rec, ok := s.glyphData(uint16(gi), cf.cbdt)
		if !ok {
			return nil
		}
		data := pngOf(s.imageFormat, rec)
		if data == nil {
			return nil
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		return fitImage(img, w, h)
	}
	return nil
}

// fitImage scales img to fit w×h, keeping its shape, centred in that box.
func fitImage(img image.Image, w, h int) *glyph {
	iw, ih := img.Bounds().Dx(), img.Bounds().Dy()
	if iw <= 0 || ih <= 0 {
		return nil
	}
	dw, dh := w, ih*w/iw
	if dh > h {
		dw, dh = iw*h/ih, h
	}
	dw, dh = max(dw, 1), max(dh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return &glyph{color: dst, x: (w - dw) / 2, y: (h - dh) / 2}
}
