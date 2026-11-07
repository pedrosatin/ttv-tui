package player

import (
	"bytes"
	"io"
	"strconv"
	"sync"
	"time"
)

const (
	// sixelFrameInterval caps the panel at 15 frames per second. It sits just
	// above 1s/15: exactly 1s/15 fits a sixteenth frame in a closed one-second
	// window.
	sixelFrameInterval = 67 * time.Millisecond
	// sixelFPS is what mpv is asked to produce, so it does not encode frames
	// the output would drop.
	sixelFPS = 15
	// sixelPlayerDelay holds player reconfiguration until the panel stops
	// changing: a dragged window, or help opened and closed at once. Each change
	// rebuilds the video output.
	sixelPlayerDelay = 200 * time.Millisecond

	// sixelMaxWidth and sixelMaxHeight are the size the CPU cost was measured at.
	sixelMaxWidth  = 480
	sixelMaxHeight = 270
	// sixelBandHeight is the height of one sixel row. A frame rounded down to it
	// cannot spill past its last band.
	sixelBandHeight = 6

	// Assumed when the terminal does not report its size in pixels.
	defaultCellWidth  = 8
	defaultCellHeight = 16
	// sixelCellSlack is how many rows and columns a guessed cell gives up, because
	// a real cell smaller than the guess draws the frame past the panel's end.
	sixelCellSlack = 1
)

const (
	altScreenEnter = "\x1b[?1049h"
	altScreenExit  = "\x1b[?1049l"
	sixelSaveMode  = "\x1b7\x1b[?80l"
	sixelRestore   = "\x1b8"
	stringTerm     = "\x1b\\"
)

// SixelPanel is the screen region reserved for video, in 1-based cells.
type SixelPanel struct {
	Row, Col   int
	Rows, Cols int
	// Visible means the rows belong to the panel. The renderer keeps them blank
	// for as long as Playing holds.
	Visible bool
	// Playing means frames may be drawn right now.
	Playing bool
}

// SixelOptions configures a SixelOutput.
type SixelOptions struct {
	// Cell reports the terminal cell size in pixels; zero values, or a nil
	// func, select a default.
	Cell func() (width, height int)
	// Resize asks the player for frames of a new size. It runs on the output's
	// own goroutine, never on the UI's.
	Resize func(width, height int)
	// Track turns the player's video on and off as the panel shows and hides, so
	// a hidden panel costs no encoding. Same goroutine as Resize, which runs first.
	Track func(on bool)
}

type sixelFrame struct {
	data          []byte
	width, height int
}

// SixelOutput is the only writer of the terminal while video is on. The renderer
// writes through Write and frames come in through Submit; ioMu orders every write
// to out, so neither can land inside the other's sequence. A frame goes out as a
// single Write that saves the cursor, places the image and restores the cursor.
//
// The renderer gives no hint of what it repainted, so any of its writes counts as
// damage and the last frame goes out again. That costs nothing while frames keep
// coming: a repaint and a new frame share one slot under the rate limit.
type SixelOutput struct {
	// ioMu is taken before mu and held across writes to out. mu is never held
	// across I/O, so SetPanel and Submit do not wait for a slow terminal.
	ioMu sync.Mutex
	out  io.Writer
	buf  []byte

	mu           sync.Mutex
	panel        SixelPanel
	pending      *sixelFrame
	last         *sixelFrame
	dirty        bool
	nextEmit     time.Time
	suspended    bool
	broken       bool
	closed       bool
	cellWidth    int
	cellHeight   int
	cellMeasured bool
	width        int
	height       int
	// playerAt is when the player hears about the panel; zero means nothing waits.
	playerAt      time.Time
	resizePending bool
	// trackSent is the last value given to Track. mpv starts with video on.
	trackSent    bool
	cell         func() (int, int)
	resizePlayer func(int, int)
	trackPlayer  func(bool)

	wake      chan struct{}
	quit      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewSixelOutput starts the output. Close stops it.
func NewSixelOutput(out io.Writer, opts SixelOptions) *SixelOutput {
	o := &SixelOutput{
		out:          out,
		cellWidth:    defaultCellWidth,
		cellHeight:   defaultCellHeight,
		width:        sixelMaxWidth,
		height:       sixelMaxHeight,
		trackSent:    true,
		cell:         opts.Cell,
		resizePlayer: opts.Resize,
		trackPlayer:  opts.Track,
		wake:         make(chan struct{}, 1),
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	go o.run()
	return o
}

// Write passes a renderer block to the terminal in one piece.
func (o *SixelOutput) Write(p []byte) (int, error) {
	o.ioMu.Lock()
	defer o.ioMu.Unlock()
	n, err := o.out.Write(p)
	o.mu.Lock()
	o.dirty = true
	// Leaving the alternate screen means the program is exiting or handing the
	// terminal over; a frame after that would land on the user's shell.
	if bytes.Contains(p, []byte(altScreenExit)) {
		o.suspended = true
		o.dropFramesLocked()
	} else if bytes.Contains(p, []byte(altScreenEnter)) {
		o.suspended = false
	}
	o.mu.Unlock()
	o.signal()
	return n, err
}

// Submit offers a frame and never blocks: a newer frame replaces one still
// waiting for its slot. It takes ownership of frame.
func (o *SixelOutput) Submit(frame []byte) {
	width, height, ok := sixelRaster(frame)
	if !ok {
		return
	}
	o.mu.Lock()
	if !o.activeLocked() {
		o.mu.Unlock()
		return
	}
	o.pending = &sixelFrame{data: frame, width: width, height: height}
	o.mu.Unlock()
	o.signal()
}

// SetPanel moves, shows or hides the region. It never waits for the terminal, and
// once it hides the panel no further frame can start. A frame already being
// written ends before any renderer write that follows.
func (o *SixelOutput) SetPanel(panel SixelPanel) {
	o.mu.Lock()
	moved := panel.Row != o.panel.Row || panel.Col != o.panel.Col ||
		panel.Rows != o.panel.Rows || panel.Cols != o.panel.Cols
	o.panel = panel
	if !o.activeLocked() {
		o.dropFramesLocked()
	} else if moved {
		o.dirty = true
	}
	if moved && panel.Rows > 0 && panel.Cols > 0 {
		o.fitLocked()
	}
	if panel.Visible != o.trackSent {
		o.playerAt = time.Now().Add(sixelPlayerDelay)
	}
	o.mu.Unlock()
	o.signal()
}

// PanelVisible reports whether the player's video has a panel to go to now.
func (o *SixelOutput) PanelVisible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.panel.Visible
}

// FrameSize returns the size in pixels frames should have now.
func (o *SixelOutput) FrameSize() (width, height int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.width, o.height
}

// Close stops emitting and waits for the goroutine. Write keeps working: the
// renderer may still need to restore the terminal.
func (o *SixelOutput) Close() error {
	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.closed = true
		o.dropFramesLocked()
		o.mu.Unlock()
		close(o.quit)
		<-o.done
	})
	return nil
}

func (o *SixelOutput) signal() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *SixelOutput) activeLocked() bool {
	p := o.panel
	return p.Visible && p.Playing && p.Rows > 0 && p.Cols > 0 && !o.suspended && !o.broken && !o.closed
}

func (o *SixelOutput) dropFramesLocked() {
	o.pending, o.last, o.dirty = nil, nil, false
}

// fitLocked derives the frame size from the panel, keeping 16:9 under the
// measured maximum, and schedules the player's reconfiguration if it changed.
func (o *SixelOutput) fitLocked() {
	if o.cell != nil {
		if width, height := o.cell(); width > 0 && height > 0 {
			o.cellWidth, o.cellHeight, o.cellMeasured = width, height, true
		}
	}
	maxWidth, maxHeight := o.drawableLocked()
	width := min(sixelMaxWidth, maxWidth)
	height := width * 9 / 16
	if height > maxHeight {
		height = maxHeight
		width = height * 16 / 9
	}
	height -= height % sixelBandHeight
	if height <= 0 || (width == o.width && height == o.height) {
		return
	}
	o.width, o.height = width, height
	o.resizePending = true
	o.playerAt = time.Now().Add(sixelPlayerDelay)
}

// drawableLocked is the room in the panel, in pixels.
func (o *SixelOutput) drawableLocked() (width, height int) {
	cols, rows := o.panel.Cols, o.panel.Rows
	if !o.cellMeasured {
		cols, rows = max(cols-sixelCellSlack, 1), max(rows-sixelCellSlack, 1)
	}
	return cols * o.cellWidth, rows * o.cellHeight
}

func (o *SixelOutput) fitsLocked(f *sixelFrame) bool {
	width, height := o.drawableLocked()
	return f.width <= width && f.height <= height
}

// nextFrameLocked picks what to draw. A frame that no longer fits the panel is
// dropped, not clipped: it would paint over the chat, so the panel stays blank
// until the player delivers the new size.
func (o *SixelOutput) nextFrameLocked() *sixelFrame {
	if !o.activeLocked() {
		return nil
	}
	if o.pending != nil && !o.fitsLocked(o.pending) {
		o.pending = nil
	}
	if o.last != nil && !o.fitsLocked(o.last) {
		o.last = nil
	}
	if o.pending != nil {
		return o.pending
	}
	if o.dirty {
		return o.last
	}
	return nil
}

func (o *SixelOutput) run() {
	defer close(o.done)
	for {
		select {
		case <-o.quit:
			return
		case <-o.wake:
		}
		for wait := o.step(); wait >= 0; wait = o.step() {
			select {
			case <-o.quit:
				return
			case <-time.After(wait):
			}
		}
	}
}

// step does the work that is due. It returns the wait until the next piece of
// work, or a negative duration when there is none.
func (o *SixelOutput) step() time.Duration {
	wait := o.applyPlayer()
	if emit := o.emit(); emit >= 0 && (wait < 0 || emit < wait) {
		wait = emit
	}
	return wait
}

// applyPlayer tells the player what the panel settled on. The size goes first, so
// video coming back on comes up at the new size in a single rebuild.
func (o *SixelOutput) applyPlayer() time.Duration {
	o.mu.Lock()
	if o.playerAt.IsZero() {
		o.mu.Unlock()
		return -1
	}
	if wait := time.Until(o.playerAt); wait > 0 {
		o.mu.Unlock()
		return wait
	}
	o.playerAt = time.Time{}
	width, height, resize := o.width, o.height, o.resizePending
	on := o.panel.Visible
	track := on != o.trackSent
	o.resizePending, o.trackSent = false, on
	o.mu.Unlock()
	if resize && o.resizePlayer != nil {
		o.resizePlayer(width, height)
	}
	if track && o.trackPlayer != nil {
		o.trackPlayer(on)
	}
	return -1
}

func (o *SixelOutput) emit() time.Duration {
	o.ioMu.Lock()
	defer o.ioMu.Unlock()
	o.mu.Lock()
	frame := o.nextFrameLocked()
	if frame == nil {
		o.mu.Unlock()
		return -1
	}
	now := time.Now()
	if wait := o.nextEmit.Sub(now); wait > 0 {
		o.mu.Unlock()
		return wait
	}
	o.pending, o.last, o.dirty = nil, frame, false
	o.nextEmit = now.Add(sixelFrameInterval)
	row, col := o.panel.Row, o.panel.Col
	o.mu.Unlock()

	o.buf = append(o.buf[:0], sixelSaveMode...)
	o.buf = append(o.buf, "\x1b["...)
	o.buf = strconv.AppendInt(o.buf, int64(row), 10)
	o.buf = append(o.buf, ';')
	o.buf = strconv.AppendInt(o.buf, int64(col), 10)
	o.buf = append(o.buf, 'H')
	o.buf = append(o.buf, frame.data...)
	o.buf = append(o.buf, sixelRestore...)
	if n, err := o.out.Write(o.buf); err != nil || n < len(o.buf) {
		// The terminal may be inside the DCS and would take the renderer's next
		// write as image data. Close it and stop drawing on this output.
		_, _ = io.WriteString(o.out, stringTerm+sixelRestore)
		o.mu.Lock()
		o.broken = true
		o.dropFramesLocked()
		o.mu.Unlock()
	}
	return -1
}
