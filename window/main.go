// ttv-tui-window hosts the TUI (PTY + terminal emulator) and the video (libmpv) in
// one window. A shim named "mpv" on the child's PATH hands streamlink's stdin and
// the IPC path to the embedded core, so the TUI drives the player as it always did.
package main

/*
#cgo pkg-config: sdl3 mpv gl
#define GL_GLEXT_PROTOTYPES 1
#include <SDL3/SDL.h>
#include <SDL3/SDL_opengl.h>
#include <mpv/client.h>
#include <mpv/render_gl.h>
#include <stdlib.h>

static Uint32 EV_MPV, EV_TUI, EV_CTL;
static void reg_events(void) { EV_MPV = SDL_RegisterEvents(3); EV_TUI = EV_MPV + 1; EV_CTL = EV_MPV + 2; }
static Uint32 ev_mpv(void) { return EV_MPV; }
static Uint32 ev_tui(void) { return EV_TUI; }
static Uint32 ev_ctl(void) { return EV_CTL; }
static void push_ev(Uint32 t) { SDL_Event e; SDL_zero(e); e.type = t; SDL_PushEvent(&e); }
static void push_quit(void) { SDL_Event e; SDL_zero(e); e.type = SDL_EVENT_QUIT; SDL_PushEvent(&e); }
static void inject_key(SDL_Keycode k, SDL_Keymod m) {
    SDL_Event e; SDL_zero(e); e.type = SDL_EVENT_KEY_DOWN; e.key.key = k; e.key.mod = m; e.key.down = true;
    SDL_PushEvent(&e);
}
static Uint32 e_type(SDL_Event *e) { return e->type; }
static SDL_Keycode e_key(SDL_Event *e) { return e->key.key; }
static SDL_Keymod e_mod(SDL_Event *e) { return e->key.mod; }
static const char *e_text(SDL_Event *e) { return e->text.text; }
static void on_update(void *c) { push_ev(EV_MPV); }
static void *gpa(void *c, const char *n) { return (void *)SDL_GL_GetProcAddress(n); }

static SDL_Window *make_window(const char *title) {
    return SDL_CreateWindow(title, 1280, 720, SDL_WINDOW_OPENGL | SDL_WINDOW_RESIZABLE | SDL_WINDOW_HIGH_PIXEL_DENSITY);
}
static mpv_render_context *make_rc(mpv_handle *m, SDL_Window *win) {
    void *wl = SDL_GetPointerProperty(SDL_GetWindowProperties(win), SDL_PROP_WINDOW_WAYLAND_DISPLAY_POINTER, NULL);
    mpv_opengl_init_params gi = {.get_proc_address = gpa};
    int adv = 1;
    mpv_render_param p[] = {
        {MPV_RENDER_PARAM_API_TYPE, (void *)MPV_RENDER_API_TYPE_OPENGL},
        {MPV_RENDER_PARAM_OPENGL_INIT_PARAMS, &gi},
        {MPV_RENDER_PARAM_WL_DISPLAY, wl},
        {MPV_RENDER_PARAM_ADVANCED_CONTROL, &adv},
        {0, NULL}};
    mpv_render_context *rc = NULL;
    if (mpv_render_context_create(&rc, m, p) < 0) return NULL;
    mpv_render_context_set_update_callback(rc, on_update, NULL);
    return rc;
}
static int rc_has_frame(mpv_render_context *rc) { return (mpv_render_context_update(rc) & MPV_RENDER_UPDATE_FRAME) != 0; }
static void rc_render(mpv_render_context *rc, int w, int h) {
    mpv_opengl_fbo fbo = {.fbo = 0, .w = w, .h = h};
    int flip = 1;
    mpv_render_param p[] = {{MPV_RENDER_PARAM_OPENGL_FBO, &fbo}, {MPV_RENDER_PARAM_FLIP_Y, &flip}, {0, NULL}};
    mpv_render_context_render(rc, p);
}
static void rc_free(mpv_render_context *rc) { mpv_render_context_set_update_callback(rc, NULL, NULL); mpv_render_context_free(rc); }
static void load_fd(mpv_handle *m, const char *url) { const char *a[] = {"loadfile", url, NULL}; mpv_command(m, a); }

static void gl_reset(int W, int H) {
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    glBindVertexArray(0);
    glBindBuffer(GL_ARRAY_BUFFER, 0);
    glBindBuffer(GL_PIXEL_UNPACK_BUFFER, 0);
    glActiveTexture(GL_TEXTURE0);
    glUseProgram(0);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    glViewport(0, 0, W, H);
}
static GLuint make_tex(void) {
    GLuint t; glGenTextures(1, &t); glBindTexture(GL_TEXTURE_2D, t);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    return t;
}
static void tex_alloc(GLuint t, int w, int h) {
    glBindTexture(GL_TEXTURE_2D, t);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA8, w, h, 0, GL_BGRA, GL_UNSIGNED_BYTE, NULL);
}
static void tex_rows(GLuint t, int y, int w, int h, const void *p) {
    glBindTexture(GL_TEXTURE_2D, t);
    glTexSubImage2D(GL_TEXTURE_2D, 0, 0, y, w, h, GL_BGRA, GL_UNSIGNED_BYTE, p);
}
static void tex_rect(GLuint t, int x, int y, int w, int h, int stride_px, const void *p) {
    glBindTexture(GL_TEXTURE_2D, t);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, stride_px);
    glTexSubImage2D(GL_TEXTURE_2D, 0, x, y, w, h, GL_BGRA, GL_UNSIGNED_BYTE, p);
    glPixelStorei(GL_UNPACK_ROW_LENGTH, 0);
}
static void clear_to(int W, int H, float r, float g, float b) {
    gl_reset(W, H); glClearColor(r, g, b, 1); glClear(GL_COLOR_BUFFER_BIT);
}
static void draw_panel(GLuint t, int W, int H, int x, int y, int w, int h) {
    gl_reset(W, H);
    glMatrixMode(GL_PROJECTION); glLoadIdentity(); glOrtho(0, W, H, 0, -1, 1);
    glMatrixMode(GL_MODELVIEW); glLoadIdentity();
    glEnable(GL_TEXTURE_2D); glEnable(GL_BLEND); glBlendFunc(GL_ONE, GL_ONE_MINUS_SRC_ALPHA);
    glBindTexture(GL_TEXTURE_2D, t);
    glBegin(GL_QUADS);
    glTexCoord2f(0, 0); glVertex2i(x, y); glTexCoord2f(1, 0); glVertex2i(x + w, y);
    glTexCoord2f(1, 1); glVertex2i(x + w, y + h); glTexCoord2f(0, 1); glVertex2i(x, y + h);
    glEnd();
    glDisable(GL_BLEND);
    glDisable(GL_TEXTURE_2D);
}
*/
import "C"

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func init() { runtime.LockOSThread() }

const (
	defaultColor = uint32(0xff000000)
	// Opacidade do chat sobre o vídeo: fundo comum e fundo com cor própria (seleção).
	glassAlpha      = uint32(150)
	glassSolidAlpha = uint32(225)
)

var (
	bgR, bgG, bgB byte = 0x1c, 0x1c, 0x1e
	fgR, fgG, fgB byte = 0xf5, 0xf5, 0xf7
	// palette é a paleta ANSI de 16 cores; o tema do foot sobrescreve em loadTheme.
	palette = [16]uint32{
		0x1c1c1e, 0xff5f4a, 0xff9f0a, 0xffc27a, 0x8fb8de, 0xff8fa3, 0xa6dcef, 0xf5f5f7,
		0x8e8e93, 0xff8373, 0xffb340, 0xffd8a8, 0xb5d0ea, 0xffb3c1, 0xcdeefa, 0xffffff,
	}
)

// loadTheme lê as cores do tema atual do foot; sem o arquivo, ficam os padrões.
func loadTheme() {
	b, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".local/state/omarchy/current/theme/foot.ini"))
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(v), 16, 32)
		if err != nil {
			continue
		}
		c := uint32(n)
		switch {
		case k == "foreground":
			fgR, fgG, fgB = byte(c>>16), byte(c>>8), byte(c)
		case k == "background":
			bgR, bgG, bgB = byte(c>>16), byte(c>>8), byte(c)
		case strings.HasPrefix(k, "regular") && len(k) == 8:
			palette[k[7]-'0'] = c
		case strings.HasPrefix(k, "bright") && len(k) == 7:
			palette[8+k[6]-'0'] = c
		}
	}
}

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

func newFontSet(px float64) (*fontSet, error) {
	fs := &fontSet{cache: map[uint64]*glyph{}}
	reg, regF, err := loadFace(fcMatch("monospace"), px)
	if err != nil {
		return nil, fmt.Errorf("monospace font: %w", err)
	}
	fs.faces, fs.fonts = []font.Face{reg}, []*sfnt.Font{regF}
	for _, p := range []string{
		fcMatch("monospace:lang=ja"),
		"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/noto/NotoSans-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansSymbols2-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansSymbols-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansThai-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansArabic-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansHebrew-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansDevanagari-Regular.ttf",
		"/usr/share/fonts/noto/NotoSansMath-Regular.ttf",
	} {
		if f, sf, err := loadFace(p, px); err == nil {
			fs.faces, fs.fonts = append(fs.faces, f), append(fs.fonts, sf)
		}
	}
	if b, bf, err := loadFace(fcMatch("monospace:bold"), px); err == nil {
		fs.bold, fs.boldF = b, bf
	}
	m := reg.Metrics()
	adv, _ := reg.GlyphAdvance('M')
	fs.cellW = adv.Round()
	fs.cellH = (m.Ascent + m.Descent).Ceil()
	fs.base = m.Ascent.Ceil()
	return fs, nil
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

type cellKey struct {
	content string
	fg, bg  uint32
	attrs   uint8
	under   bool
	width   int
	cursor  bool
	hole    bool
	glass   bool // chat sobre o vídeo: fundo translúcido, texto opaco
}

// hostPanel é a região do vídeo que o TUI publica, em células com base 1.
type hostPanel struct {
	Row, Col, Rows, Cols int
	Visible              bool
	OverlayCols          int
}

type ctlMsg struct {
	kind string // "start" | "ended"
	ipc  string
	fd   int
	conn *net.UnixConn
	s    *session
	host hostPanel
}

type session struct {
	m        *C.mpv_handle
	rc       *C.mpv_render_context
	conn     *net.UnixConn
	ipc      string
	stop     atomic.Bool
	done     chan struct{}
	started  time.Time
	gotFirst atomic.Bool
	ttffMs   atomic.Int64
}

type app struct {
	win  *C.SDL_Window
	tex  C.GLuint
	W, H int
	px   int // x do painel
	cols int
	rows int
	pw   int
	ph   int
	fs   *fontSet
	term *vt.SafeEmulator
	ptmx *os.File
	cmd  *exec.Cmd
	buf  []byte
	prev []cellKey
	sess *session
	ctlq chan ctlMsg

	tuiDirty     atomic.Bool
	tuiPending   atomic.Bool
	videoDirty   bool
	panelChanged bool
	layoutDirty  bool
	lastPresent  time.Time
	quit         bool

	mu          sync.Mutex
	presents    int64
	videoFrames int64
	panelPaints int64
	paintNs     int64
	uploadNs    int64
	sessions    int64
	lastTtff    int64
	lastIpc     string
	inputAt     time.Time
	latMs       []float64
	maxMs       int64
	stalls      int64
	lastVideo   time.Time

	// Pintura fora da thread de render: paintMu guarda buf, prev e as linhas
	// sujas; a thread de render só faz TryLock e envia o que já está pronto.
	paintMu  sync.Mutex
	dirtyY0  int
	dirtyY1  int
	dirtyX0  int
	dirtyX1  int
	paintReq chan struct{}
	// host e holeOn também ficam sob paintMu: o painter decide com eles.
	haveHost bool
	host     hostPanel
	holeOn   bool
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
	glass := hole && a.host.OverlayCols > 0 && x >= a.host.Col-1+a.host.Cols-a.host.OverlayCols
	hole = hole && !glass
	if c == nil {
		return cellKey{content: " ", fg: defaultColor, bg: defaultColor, width: 1, hole: hole, glass: glass}
	}
	return cellKey{
		hole:    hole,
		glass:   glass,
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
		for x := 0; x < a.cols; x++ {
			k := a.keyAt(x, y, pos.X, pos.Y, show)
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
		bgA = glassAlpha
		if k.bg == defaultColor && k.attrs&uv.AttrReverse == 0 && !k.cursor {
			br, bg, bb = 0, 0, 0
		} else {
			bgA = glassSolidAlpha
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
	g := a.fs.get(r, k.attrs&uv.AttrBold != 0)
	if g == nil { // sem glifo: caixa marca a lacuna de fonte (emoji colorido, por exemplo)
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
	if g.mask != nil {
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

func (a *app) setPx(x, y int, r, g, b uint32) {
	if x < 0 || y < 0 || x >= a.pw || y >= a.ph {
		return
	}
	off := y*a.pw*4 + x*4
	a.buf[off], a.buf[off+1], a.buf[off+2], a.buf[off+3] = byte(b), byte(g), byte(r), 255
}

// relayout recalcula painel, PTY e emulador a partir do tamanho da janela.
func (a *app) relayout() {
	var w, h C.int
	C.SDL_GetWindowSizeInPixels(a.win, &w, &h)
	if w == 0 || h == 0 {
		return
	}
	a.W, a.H = int(w), int(h)
	a.px = 0
	cols := (a.W - a.px) / a.fs.cellW
	rows := a.H / a.fs.cellH
	if cols < 20 {
		cols = 20
	}
	if rows < 5 {
		rows = 5
	}
	pw, ph := cols*a.fs.cellW, rows*a.fs.cellH
	if cols != a.cols || rows != a.rows {
		a.paintMu.Lock()
		defer a.paintMu.Unlock()
		a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1 = rows, -1, cols, -1
		a.cols, a.rows, a.pw, a.ph = cols, rows, pw, ph
		a.buf = make([]byte, pw*ph*4)
		for i := 0; i < len(a.buf); i += 4 {
			a.buf[i], a.buf[i+1], a.buf[i+2], a.buf[i+3] = bgB, bgG, bgR, 255
		}
		a.prev = make([]cellKey, cols*rows)
		C.tex_alloc(a.tex, C.int(pw), C.int(ph))
		C.tex_rows(a.tex, 0, C.int(pw), C.int(ph), unsafe.Pointer(&a.buf[0]))
		a.term.Resize(cols, rows)
		if a.ptmx != nil {
			_ = pty.Setsize(a.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows), X: uint16(pw), Y: uint16(ph)})
		}
		a.tuiDirty.Store(true)
		a.kickPaint()
	}
	a.applyMargins()
	a.layoutDirty = true
}

// applyMargins põe o vídeo no retângulo publicado pelo TUI.
func (a *app) applyMargins() {
	if a.sess == nil {
		return
	}
	W, H := float64(a.W), float64(a.H)
	ratio := func(name string, px int) {
		a.setOpt(a.sess.m, "video-margin-ratio-"+name, fmt.Sprintf("%.5f", max(float64(px), 0)/map[bool]float64{true: W, false: H}[name == "left" || name == "right"]))
	}
	if !a.host.Visible {
		return // sobreposição na frente: mantém a geometria para quando ela sair
	}
	cw, ch := a.fs.cellW, a.fs.cellH
	x, y := (a.host.Col-1)*cw, (a.host.Row-1)*ch
	w, h := a.host.Cols*cw, a.host.Rows*ch
	ratio("left", x)
	ratio("right", a.W-x-w)
	ratio("top", y)
	ratio("bottom", a.H-y-h)
}

// refreshHole recalcula se as células do vídeo ficam transparentes agora.
func (a *app) refreshHole() {
	a.paintMu.Lock()
	a.holeOn = a.haveHost && a.host.Visible && a.sess != nil
	a.paintMu.Unlock()
	a.tuiDirty.Store(true)
	a.kickPaint()
}

func (a *app) setHost(p hostPanel) {
	a.paintMu.Lock()
	a.haveHost, a.host = true, p
	a.paintMu.Unlock()
	a.refreshHole()
	a.relayout()
}

func (a *app) serveLayout(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			dec := json.NewDecoder(c)
			for {
				var p hostPanel
				if dec.Decode(&p) != nil {
					return
				}
				a.ctlq <- ctlMsg{kind: "layout", host: p}
				C.push_ev(C.ev_ctl())
			}
		}()
	}
}

func (a *app) setOpt(m *C.mpv_handle, k, v string) {
	ck, cv := C.CString(k), C.CString(v)
	C.mpv_set_option_string(m, ck, cv)
	C.free(unsafe.Pointer(ck))
	C.free(unsafe.Pointer(cv))
}

func (a *app) startSession(msg ctlMsg) {
	if a.sess != nil {
		a.stopSession()
	}
	m := C.mpv_create()
	opts := [][2]string{
		{"vo", "libmpv"}, {"hwdec", "auto-safe"}, {"input-ipc-server", msg.ipc},
		{"idle", "once"}, {"volume-max", "100"}, {"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"}, {"osd-level", "1"}, {"osd-bar", "no"},
		{"background-color", fmt.Sprintf("#%02x%02x%02x", bgR, bgG, bgB)},
		{"keep-open", "no"}, {"terminal", "no"},
	}
	if os.Getenv("TTV_WINDOW_MUTE") == "1" {
		opts = append(opts, [2]string{"mute", "yes"})
	}
	for _, o := range opts {
		a.setOpt(m, o[0], o[1])
	}
	C.mpv_initialize(m)
	rc := C.make_rc(m, a.win)
	if rc == nil {
		fmt.Fprintln(os.Stderr, "render context falhou")
		C.mpv_terminate_destroy(m)
		_ = syscall.Close(msg.fd) // libmpv never took it: streamlink has to see the pipe close
		msg.conn.Close()
		return
	}
	s := &session{m: m, rc: rc, conn: msg.conn, ipc: msg.ipc, done: make(chan struct{}), started: time.Now()}
	a.sess = s
	a.mu.Lock()
	a.sessions++
	a.lastIpc = msg.ipc
	a.mu.Unlock()
	url := C.CString(fmt.Sprintf("fdclose://%d", msg.fd))
	C.load_fd(m, url)
	C.free(unsafe.Pointer(url))
	go func() {
		defer close(s.done)
		for {
			ev := C.mpv_wait_event(m, -1)
			if ev.event_id == C.MPV_EVENT_SHUTDOWN {
				a.ctlq <- ctlMsg{kind: "ended", s: s}
				C.push_ev(C.ev_ctl())
				return
			}
			if s.stop.Load() {
				return
			}
		}
	}()
	go func() { // shim morreu (TUI matou o grupo): encerra o core
		b := make([]byte, 1)
		_, _ = msg.conn.Read(b)
		a.ctlq <- ctlMsg{kind: "ended", s: s}
		C.push_ev(C.ev_ctl())
	}()
	a.refreshHole()
	a.relayout()
}

func (a *app) stopSession() {
	s := a.sess
	if s == nil {
		return
	}
	a.sess = nil
	s.stop.Store(true)
	C.mpv_wakeup(s.m)
	<-s.done
	C.rc_free(s.rc)
	C.mpv_terminate_destroy(s.m)
	s.conn.Close()
	a.refreshHole()
	a.relayout()
}

func (a *app) present(video bool) {
	W, H := C.int(a.W), C.int(a.H)
	if a.sess != nil {
		C.rc_render(a.sess.rc, W, H)
		if video {
			if !a.sess.gotFirst.Swap(true) {
				a.sess.ttffMs.Store(time.Since(a.sess.started).Milliseconds())
				a.mu.Lock()
				a.lastTtff = a.sess.ttffMs.Load()
				a.mu.Unlock()
			}
		}
	} else {
		C.clear_to(W, H, C.float(float32(bgR)/255), C.float(float32(bgG)/255), C.float(float32(bgB)/255))
	}
	if a.buf != nil {
		C.draw_panel(a.tex, W, H, C.int(a.px), 0, C.int(a.pw), C.int(a.ph))
	}
	C.SDL_GL_SwapWindow(a.win)
	if a.sess != nil {
		C.mpv_render_context_report_swap(a.sess.rc)
	}
	now := time.Now()
	a.lastPresent = now
	a.mu.Lock()
	a.presents++
	if video {
		a.videoFrames++
		if !a.lastVideo.IsZero() {
			gap := now.Sub(a.lastVideo).Milliseconds()
			if gap > 40 {
				a.stalls++
			}
			if gap > a.maxMs {
				a.maxMs = gap
			}
		}
		a.lastVideo = now
	}
	if !a.inputAt.IsZero() && a.panelChanged {
		ms := float64(now.Sub(a.inputAt).Microseconds()) / 1000
		a.latMs = append(a.latMs, ms)
		a.inputAt = time.Time{}
	}
	a.mu.Unlock()
}

func keyBytes(key C.SDL_Keycode, mod C.SDL_Keymod) []byte {
	ctrl := mod&C.SDL_KMOD_CTRL != 0
	alt := mod&C.SDL_KMOD_ALT != 0
	shift := mod&C.SDL_KMOD_SHIFT != 0
	arrow := func(c byte) []byte {
		switch {
		case ctrl:
			return []byte{0x1b, '[', '1', ';', '5', c}
		case shift:
			return []byte{0x1b, '[', '1', ';', '2', c}
		case alt:
			return []byte{0x1b, '[', '1', ';', '3', c}
		}
		return []byte{0x1b, '[', c}
	}
	switch key {
	case C.SDLK_RETURN, C.SDLK_KP_ENTER:
		return []byte{'\r'}
	case C.SDLK_ESCAPE:
		return []byte{0x1b}
	case C.SDLK_BACKSPACE:
		if ctrl {
			return []byte{0x08}
		}
		return []byte{0x7f}
	case C.SDLK_TAB:
		if shift {
			return []byte{0x1b, '[', 'Z'}
		}
		return []byte{'\t'}
	case C.SDLK_UP:
		return arrow('A')
	case C.SDLK_DOWN:
		return arrow('B')
	case C.SDLK_RIGHT:
		return arrow('C')
	case C.SDLK_LEFT:
		return arrow('D')
	case C.SDLK_HOME:
		return []byte("\x1b[H")
	case C.SDLK_END:
		return []byte("\x1b[F")
	case C.SDLK_PAGEUP:
		return []byte("\x1b[5~")
	case C.SDLK_PAGEDOWN:
		return []byte("\x1b[6~")
	case C.SDLK_DELETE:
		return []byte("\x1b[3~")
	}
	if ctrl && key >= 'a' && key <= 'z' {
		return []byte{byte(key-'a') + 1}
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
	_, _ = a.ptmx.Write(b)
}

func (a *app) handle(ev *C.SDL_Event) {
	t := C.e_type(ev)
	switch {
	case t == C.SDL_EVENT_QUIT || t == C.SDL_EVENT_WINDOW_CLOSE_REQUESTED:
		a.quit = true
	case t == C.SDL_EVENT_WINDOW_PIXEL_SIZE_CHANGED || t == C.SDL_EVENT_WINDOW_RESIZED:
		a.relayout()
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
			}
		}
	}
}

// painter roda em goroutine própria, no máximo a ~30 Hz.
func (a *app) painter() {
	var last time.Time
	for range a.paintReq {
		if d := time.Since(last); d < 16*time.Millisecond { // um quadro de 60 Hz
			time.Sleep(16*time.Millisecond - d)
		}
		for a.tuiDirty.Swap(false) {
			a.paintMu.Lock()
			y0, y1 := a.paint()
			if y1 >= 0 {
				if y0 < a.dirtyY0 {
					a.dirtyY0 = y0
				}
				if y1 > a.dirtyY1 {
					a.dirtyY1 = y1
				}
			}
			a.paintMu.Unlock()
			last = time.Now()
			C.push_ev(C.ev_tui())
		}
	}
}

func (a *app) kickPaint() {
	select {
	case a.paintReq <- struct{}{}:
	default:
	}
}

// uploadPanel manda à GPU as linhas prontas, sem nunca esperar o painter.
func (a *app) uploadPanel() {
	if !a.paintMu.TryLock() {
		return
	}
	defer a.paintMu.Unlock()
	if a.dirtyY1 < 0 {
		return
	}
	t0 := time.Now()
	y0, y1, x0, x1 := a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1
	a.dirtyY0, a.dirtyY1, a.dirtyX0, a.dirtyX1 = a.rows, -1, a.cols, -1
	cw, ch := a.fs.cellW, a.fs.cellH
	off := y0*ch*a.pw*4 + x0*cw*4
	C.tex_rect(a.tex, C.int(x0*cw), C.int(y0*ch), C.int((x1-x0+1)*cw), C.int((y1-y0+1)*ch), C.int(a.pw), unsafe.Pointer(&a.buf[off]))
	a.mu.Lock()
	a.uploadNs += time.Since(t0).Nanoseconds()
	a.mu.Unlock()
	a.panelChanged = true
}

func (a *app) frame() {
	a.uploadPanel()
	switch {
	case a.videoDirty:
		a.present(true)
		a.videoDirty, a.layoutDirty, a.panelChanged = false, false, false
	case a.layoutDirty, a.panelChanged && (a.sess == nil || time.Since(a.lastPresent) > 33*time.Millisecond):
		// Painel sem quadro de vídeo: pausado, ou sem sessão.
		a.present(false)
		a.layoutDirty, a.panelChanged = false, false
	}
}

func (a *app) pumpPTY() {
	buf := make([]byte, 64*1024)
	for {
		n, err := a.ptmx.Read(buf)
		if n > 0 {
			_, _ = a.term.Write(buf[:n])
			a.tuiDirty.Store(true)
			a.kickPaint()
		}
		if err != nil {
			return
		}
	}
}

func (a *app) pumpReplies() {
	buf := make([]byte, 4096)
	for {
		n, err := a.term.Read(buf)
		if n > 0 && a.ptmx != nil {
			_, _ = a.ptmx.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (a *app) serveCtl(path string) {
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		panic(err)
	}
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			return
		}
		go func(c *net.UnixConn) {
			data, oob := make([]byte, 8192), make([]byte, 128)
			n, oobn, _, _, err := c.ReadMsgUnix(data, oob)
			if err != nil {
				c.Close()
				return
			}
			var req struct {
				IPC string `json:"ipc"`
			}
			_ = json.Unmarshal(data[:n], &req)
			fd := -1
			if ms, err := syscall.ParseSocketControlMessage(oob[:oobn]); err == nil && len(ms) > 0 {
				if fds, err := syscall.ParseUnixRights(&ms[0]); err == nil && len(fds) > 0 {
					fd = fds[0]
				}
			}
			a.ctlq <- ctlMsg{kind: "start", ipc: req.IPC, fd: fd, conn: c}
			C.push_ev(C.ev_ctl())
		}(c)
	}
}

// screenText devolve o texto atual da grade (usado só nos testes de fluxo).
func (a *app) screenText() string {
	var sb strings.Builder
	rows, cols := a.term.Height(), a.term.Width()
	for y := 0; y < rows; y++ {
		var line strings.Builder
		for x := 0; x < cols; x++ {
			c := a.term.CellAt(x, y)
			if c == nil || c.Content == "" {
				if c == nil || c.Width != 0 {
					line.WriteByte(' ')
				}
				continue
			}
			line.WriteString(c.Content)
		}
		sb.WriteString(strings.TrimRight(line.String(), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

var keyNames = map[string]C.SDL_Keycode{
	"Return": C.SDLK_RETURN, "Escape": C.SDLK_ESCAPE, "Tab": C.SDLK_TAB, "Up": C.SDLK_UP,
	"Down": C.SDLK_DOWN, "Left": C.SDLK_LEFT, "Right": C.SDLK_RIGHT, "Backspace": C.SDLK_BACKSPACE,
}

func (a *app) serveTest(path string) {
	l, err := net.Listen("unix", path)
	if err != nil {
		panic(err)
	}
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 4096)
			n, _ := c.Read(buf)
			line := strings.TrimSpace(string(buf[:n]))
			cmd, rest, _ := strings.Cut(line, " ")
			switch cmd {
			case "key":
				var mod C.SDL_Keymod
				name := rest
				if strings.HasPrefix(name, "ctrl+") {
					mod, name = C.SDL_KMOD_LCTRL, name[5:]
				} else if strings.HasPrefix(name, "alt+") {
					mod, name = C.SDL_KMOD_LALT, name[4:]
				}
				k, ok := keyNames[name]
				if !ok && len(name) == 1 {
					k, ok = C.SDL_Keycode(name[0]), true
				}
				if ok {
					C.inject_key(k, mod)
					fmt.Fprintln(c, "ok")
				} else {
					fmt.Fprintln(c, "tecla?")
				}
			case "text":
				a.writeInput([]byte(rest))
				fmt.Fprintln(c, "ok")
			case "raw": // bytes em hex, direto no PTY (tecla imprimível, espaço)
				if b, err := hex.DecodeString(rest); err == nil {
					a.writeInput(b)
					fmt.Fprintln(c, "ok")
				}
			case "pixel": // BGRA do canto da célula e o maior alfa dentro dela
				var x, y int
				_, _ = fmt.Sscan(rest, &x, &y)
				a.paintMu.Lock()
				cw, ch := a.fs.cellW, a.fs.cellH
				off := y*ch*a.pw*4 + x*cw*4
				top := byte(0)
				for yy := y * ch; yy < (y+1)*ch; yy++ {
					for xx := x * cw; xx < (x+1)*cw; xx++ {
						top = max(top, a.buf[yy*a.pw*4+xx*4+3])
					}
				}
				fmt.Fprintf(c, "%d %d %d %d %d\n", a.buf[off], a.buf[off+1], a.buf[off+2], a.buf[off+3], top)
				a.paintMu.Unlock()
			case "quit": // mesmo caminho de fechar a janela
				C.push_quit()
				fmt.Fprintln(c, "ok")
			case "screen":
				fmt.Fprint(c, a.screenText())
			case "stats":
				a.mu.Lock()
				out := map[string]any{
					"pid": os.Getpid(), "presents": a.presents, "videoFrames": a.videoFrames,
					"panelPaints": a.panelPaints, "paintMs": a.paintNs / 1e6, "uploadMs": a.uploadNs / 1e6,
					"sessions": a.sessions, "ttffMs": a.lastTtff, "ipc": a.lastIpc, "cols": a.cols,
					"rows": a.rows, "W": a.W, "H": a.H, "cellW": a.fs.cellW, "cellH": a.fs.cellH,
					"hole": []int{a.host.Col, a.host.Row, a.host.Cols, a.host.Rows}, "holeOn": a.holeOn, "overlay": a.host.OverlayCols, "haveHost": a.haveHost, "tofu": a.fs.tofu, "stalls": a.stalls, "maxGapMs": a.maxMs, "active": a.sess != nil, "goroutines": runtime.NumGoroutine(),
				}
				if len(a.latMs) > 0 {
					out["latMs"] = a.latMs
				}
				a.mu.Unlock()
				_ = json.NewEncoder(c).Encode(out)
			}
		}(c)
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func main() {
	if filepath.Base(os.Args[0]) == "mpv" {
		shimMain()
		return
	}
	tui := os.Getenv("TTV_WINDOW_TUI")
	if tui == "" {
		tui = filepath.Join(os.Getenv("HOME"), ".local/bin/ttv-tui")
		if exe, err := os.Executable(); err == nil {
			if sib := filepath.Join(filepath.Dir(exe), "ttv-tui"); fileExists(sib) {
				tui = sib
			}
		}
	}
	loadTheme()
	app_id := os.Getenv("TTV_WINDOW_APPID")
	if app_id == "" {
		app_id = "ttv-tui"
	}

	dir, err := os.MkdirTemp("", "ttv-shell-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	binDir := filepath.Join(dir, "bin")
	_ = os.Mkdir(binDir, 0o755)
	exe, _ := os.Executable()
	_ = os.Symlink(exe, filepath.Join(binDir, "mpv"))
	ctlPath := filepath.Join(dir, "ctl.sock")
	testPath := os.Getenv("TTV_WINDOW_TEST") // só os testes de fluxo abrem esta porta

	cApp := C.CString(app_id)
	cHint := C.CString("SDL_APP_ID")
	C.SDL_SetHint(cHint, cApp)
	drv, wl := C.CString("SDL_VIDEO_DRIVER"), C.CString("wayland")
	C.SDL_SetHint(drv, wl)
	fatal := func(args ...any) {
		fmt.Fprintln(os.Stderr, args...)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	if !C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_EVENTS) {
		fatal(C.GoString(C.SDL_GetError()))
	}
	// --probe answers whether this machine can open the window: the libraries
	// loaded (or this process would not exist), a Wayland display and a usable font.
	if len(os.Args) == 2 && os.Args[1] == "--probe" {
		if _, err := newFontSet(14); err != nil {
			fatal(err)
		}
		C.SDL_Quit()
		return
	}
	C.reg_events()
	title := C.CString("ttv-tui")
	win := C.make_window(title)
	C.SDL_GL_CreateContext(win)
	C.SDL_GL_SetSwapInterval(1)
	C.SDL_StartTextInput(win)

	scale := float64(C.SDL_GetWindowDisplayScale(win))
	if scale <= 0 {
		scale = 1
	}
	pt := 9.0
	if v, err := strconv.ParseFloat(os.Getenv("TTV_WINDOW_PT"), 64); err == nil {
		pt = v
	}
	fs, err := newFontSet(pt * 96.0 / 72.0 * scale)
	if err != nil {
		fatal(err)
	}

	a := &app{win: win, fs: fs, ctlq: make(chan ctlMsg, 16), paintReq: make(chan struct{}, 1), dirtyY1: -1}
	go a.painter()
	a.tex = C.make_tex()
	a.term = vt.NewSafeEmulator(80, 24)
	a.relayout()
	go a.serveCtl(ctlPath)
	if testPath != "" {
		_ = os.Remove(testPath)
		go a.serveTest(testPath)
	}
	layoutPath := filepath.Join(dir, "layout.sock")
	layoutLn, err := net.Listen("unix", layoutPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "layout:", err)
		os.Exit(1)
	}
	go a.serveLayout(layoutLn)

	a.cmd = exec.Command(tui, os.Args[1:]...)
	a.cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TTV_WINDOW_SOCK="+ctlPath,
		"TTV_TUI_VIDEO_HOST="+layoutPath,
		"TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.StartWithSize(a.cmd, &pty.Winsize{Cols: uint16(a.cols), Rows: uint16(a.rows), X: uint16(a.pw), Y: uint16(a.ph)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pty:", err)
		os.Exit(1)
	}
	a.ptmx = ptmx
	go a.pumpPTY()
	go a.pumpReplies()
	go func() {
		_ = a.cmd.Wait()
		C.push_quit()
	}()

	var ev C.SDL_Event
	for !a.quit {
		wait := C.Sint32(-1)
		if a.panelChanged && a.sess != nil {
			wait = 8 // sem quadro de vídeo a caminho (pausa), ainda precisamos apresentar o painel
		}
		if C.SDL_WaitEventTimeout(&ev, wait) {
			a.handle(&ev)
			for C.SDL_PollEvent(&ev) {
				a.handle(&ev)
			}
		}
		a.frame()
	}

	_ = a.cmd.Process.Signal(syscall.SIGHUP)
	done := make(chan struct{})
	go func() { // cmd.Wait já roda acima; só aguarda o filho sumir
		for i := 0; i < 40; i++ {
			if a.cmd.Process.Signal(syscall.Signal(0)) != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(done)
	}()
	<-done
	_ = a.cmd.Process.Kill()
	a.stopSession()
	C.SDL_Quit()
}
