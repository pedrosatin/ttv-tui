package main

/*
#include "bridge.h"
*/
import "C"

import (
	"io"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
)

// hostPanel é a região do vídeo que o TUI publica, em células com base 1.
type hostPanel struct {
	Row, Col, Rows, Cols int
	Visible              bool
	OverlayCols          int
	OverlayLeft          bool
	OverlayOpacity       int
}

type ctlMsg struct {
	kind string // "start" | "ended" | "layout" | "resize"
	ipc  string
	fd   int    // Unix: streamlink's stdin, closed if libmpv never takes it
	url  string // what libmpv loads: fdclose://N or the shim's named pipe
	conn *net.UnixConn
	s    *session
	host hostPanel
	// resize: o tamanho pedido em pixels e onde avisar que a grade mudou.
	w, h    int
	resized chan [2]int
}

type session struct {
	m        *C.mpv_handle
	rc       *C.mpv_render_context
	conn     *net.UnixConn
	ipc      string
	stop     atomic.Bool
	done     chan struct{}
	stopping chan struct{}
	started  time.Time
	gotFirst atomic.Bool
	ttffMs   atomic.Int64

	setProperty func(string, string)
}

// fbReq pede a cor de um pixel da janela, com a origem em cima, à esquerda.
type fbReq struct {
	x, y  int
	reply chan [3]byte
}

type ptyDevice interface {
	io.ReadWriteCloser
	Resize(cols, rows, pw, ph int) error
}

type app struct {
	win  *C.SDL_Window
	W, H int
	px   int // x do painel
	cols int
	rows int
	pw   int
	ph   int
	fs   *fontSet
	pt   float64
	term *vt.SafeEmulator
	ptmx ptyDevice
	cmd  *exec.Cmd
	buf  []byte
	prev []cellKey
	sess *session
	ctlq chan ctlMsg

	// O painel fica em uma textura, ou em várias quando o OpenGL não aceita uma do
	// tamanho dele (paneltex.go). texs[i] guarda tiles[i]; só a thread de render mexe.
	gl      glCaps
	glName  string // GL_VERSION e GL_RENDERER, para o stats e as mensagens de erro
	tiles   []panelTile
	texs    []C.GLuint
	glCheck bool // confere glGetError no próximo quadro

	inputq chan []byte
	fbq    chan fbReq // leituras do framebuffer pedidas pelo socket de teste

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
	glErrors    int64
	glLastError int

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

	// testing liga a porta de teste (TTV_WINDOW_TEST). testState é o último
	// resumo de estado que a TUI mandou, guardado só com testing (sob mu).
	testing   bool
	testState []byte
}
