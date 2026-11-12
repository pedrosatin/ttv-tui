package main

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type glyph struct {
	mask *image.Alpha
	x, y int
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

// fcMatch resolves a fontconfig pattern to a file; empty when it cannot.
func fcMatch(pattern string) string {
	out, err := exec.Command("fc-match", "-f", "%{file}", pattern).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resolveFont(pattern string) string {
	if runtime.GOOS == "windows" {
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		fdir := filepath.Join(windir, "Fonts")
		switch pattern {
		case "monospace":
			for _, name := range []string{"CascadiaMono.ttf", "CascadiaCode.ttf", "consola.ttf", "lucon.ttf", "cour.ttf"} {
				p := filepath.Join(fdir, name)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		case "monospace:bold":
			for _, name := range []string{"CascadiaMono-Bold.ttf", "CascadiaCode-Bold.ttf", "consolab.ttf", "courbd.ttf"} {
				p := filepath.Join(fdir, name)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		case "monospace:lang=ja":
			for _, name := range []string{"msgothic.ttc", "meiryo.ttc"} {
				p := filepath.Join(fdir, name)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
		return ""
	}
	return fcMatch(pattern)
}

func newFontSet(px float64) (*fontSet, error) {
	fs := &fontSet{cache: map[uint64]*glyph{}}
	regPath := resolveFont("monospace")
	reg, regF, err := loadFace(regPath, px)
	if err != nil {
		return nil, fmt.Errorf("monospace font (%s): %w", regPath, err)
	}
	fs.faces, fs.fonts = []font.Face{reg}, []*sfnt.Font{regF}

	var fallbackPaths []string
	if runtime.GOOS == "windows" {
		windir := os.Getenv("WINDIR")
		if windir == "" {
			windir = `C:\Windows`
		}
		fdir := filepath.Join(windir, "Fonts")
		fallbackPaths = []string{
			resolveFont("monospace:lang=ja"),
			filepath.Join(fdir, "seguiemj.ttf"),
			filepath.Join(fdir, "segoeui.ttf"),
			filepath.Join(fdir, "msyh.ttc"),
			filepath.Join(fdir, "malgun.ttf"),
		}
	} else {
		fallbackPaths = []string{
			resolveFont("monospace:lang=ja"),
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansSymbols2-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansSymbols-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansThai-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansArabic-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansHebrew-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansDevanagari-Regular.ttf",
			"/usr/share/fonts/noto/NotoSansMath-Regular.ttf",
		}
	}

	for _, p := range fallbackPaths {
		if p == "" {
			continue
		}
		if f, sf, err := loadFace(p, px); err == nil {
			fs.faces, fs.fonts = append(fs.faces, f), append(fs.fonts, sf)
		}
	}
	if b, bf, err := loadFace(resolveFont("monospace:bold"), px); err == nil {
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

func (fs *fontSet) get(r rune, bold bool) *glyph {
	key := uint64(r) << 1
	if bold {
		key |= 1
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
		fs.tofu++
		fs.cache[key] = nil
		return nil
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
