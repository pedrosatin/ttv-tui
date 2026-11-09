package main

/*
#include "bridge.h"
*/
import "C"

import (
	"net"
	"os"
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
