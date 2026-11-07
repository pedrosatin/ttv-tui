//go:build unix

package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// These sizes are what the terminal below reports and what it paints images with.
const (
	wireCellWidth  = 6
	wireCellHeight = 12
	// wireMaxEnvelope bounds a frame nothing ever closed.
	wireMaxEnvelope = 8 << 20
)

// syntheticFrame is the frame number seq of a size. It is a pure function, so the
// terminal can rebuild what the player wrote and compare byte by byte: letters are
// valid sixel data, and a chat line inside a frame would pass any test of the
// bytes alone. Three colour passes put it at the 32 KB and up mpv writes at this
// size. The sequence number rides in the colour register.
func syntheticFrame(seq, width, height int) []byte {
	var b bytes.Buffer
	b.Grow(width*height/2 + 64)
	fmt.Fprintf(&b, "\x1bPq\"1;1;%d;%d#1;2;%d;%d;%d", width, height, seq/10000%100, seq/100%100, seq%100)
	for band := range (height + 5) / 6 {
		for pass := range 3 {
			fmt.Fprintf(&b, "#%d", pass+1)
			for x := range width {
				b.WriteByte(byte('?' + (seq+band*7+pass*13+x)%64))
			}
			b.WriteByte('$')
		}
		b.WriteByte('-')
	}
	b.WriteString("\x1b\\")
	return b.Bytes()
}

// wireFrame is one image as it crossed the PTY.
type wireFrame struct {
	at            time.Time
	offset        int
	row, col      int
	seq           int
	width, height int
}

type wireCell struct {
	text  rune
	image bool
}

type wireState int

const (
	wireGround wireState = iota
	wireEscape
	wireCSI
	wireOSC
	wireOSCEscape
	wireEnvelope
)

var (
	wireEnvelopeStart = []byte("\x1b7")
	wireEnvelopeEnd   = []byte("\x1b\\\x1b8")
	wireEnvelopeHead  = regexp.MustCompile(`^\x1b7\x1b\[\?80l\x1b\[([0-9]+);([0-9]+)H\x1bPq"1;1;([0-9]+);([0-9]+)#1;2;([0-9]+);([0-9]+);([0-9]+)`)
)

// wireTerminal is the far end of the PTY: a small terminal with a grid of cells
// that also checks every byte it receives.
//
// Painting follows what sixel terminals do: an image covers cells from the cursor
// down, scrolling the screen if it passes the bottom edge (mode 80 off), and text
// or an erase over a cell removes the image from that cell.
//
// A violation is anything a real terminal would show as corruption:
//   - a DCS that is not exactly the frame the player wrote, cursor save and
//     position before it and cursor restore right after it;
//   - any sequence or UTF-8 character cut by another one;
//   - a frame inside a block the renderer wrote in one piece;
//   - a frame going back in the sequence, or landing after the alternate screen.
type wireTerminal struct {
	mu sync.Mutex
	// answer writes to the application's input.
	answer io.Writer
	// speed limits what the terminal takes per second, as a slow one does. Zero
	// means no limit.
	speed atomic.Int64

	rows, cols int
	cells      [][]wireCell
	row, col   int
	wrapNext   bool
	savedRow   int
	savedCol   int

	state    wireState
	sequence []byte
	envelope []byte
	pending  []byte // an unfinished UTF-8 character
	offset   int

	// inBlock holds from the renderer's home to its closing cursor position.
	inBlock   bool
	afterWipe bool
	altScreen bool
	leftAlt   bool

	frames     []wireFrame
	seqFloor   int
	violations []string
	clears     int
	// scrolled counts frames that pushed the screen up.
	scrolled int
}

func newWireTerminal(rows, cols int, answer io.Writer) *wireTerminal {
	w := &wireTerminal{answer: answer}
	w.setSize(rows, cols)
	return w
}

func blankCells(cols int) []wireCell {
	line := make([]wireCell, cols)
	for i := range line {
		line[i].text = ' '
	}
	return line
}

func (w *wireTerminal) setSize(rows, cols int) {
	cells := make([][]wireCell, rows)
	for i := range cells {
		cells[i] = blankCells(cols)
		if i < len(w.cells) {
			copy(cells[i], w.cells[i])
		}
	}
	w.cells, w.rows, w.cols = cells, rows, cols
	w.row, w.col = min(w.row, rows-1), min(w.col, cols-1)
	w.wrapNext = false
}

// resize changes the grid, keeping the cells that still fit, as a terminal does
// on the alternate screen.
func (w *wireTerminal) resize(rows, cols int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.setSize(rows, cols)
}

func (w *wireTerminal) violate(format string, args ...any) {
	w.violations = append(w.violations, fmt.Sprintf("byte %d: ", w.offset)+fmt.Sprintf(format, args...))
}

// pump reads the PTY until it closes.
func (w *wireTerminal) pump(master *os.File) {
	buf := make([]byte, 64*1024)
	for {
		size := len(buf)
		speed := w.speed.Load()
		if speed > 0 {
			size = 4096
		}
		n, err := master.Read(buf[:size])
		if n > 0 {
			w.feed(buf[:n])
			if speed > 0 {
				time.Sleep(time.Duration(n) * time.Second / time.Duration(speed))
			}
		}
		if errors.Is(err, syscall.EAGAIN) {
			// Only a master kept off the poller gets here (pty_darwin_test.go).
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			return
		}
	}
}

func (w *wireTerminal) feed(chunk []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for len(chunk) > 0 {
		if w.state == wireEnvelope {
			used := w.takeEnvelope(chunk)
			chunk = chunk[used:]
			w.offset += used
			continue
		}
		w.step(chunk[0])
		chunk = chunk[1:]
		w.offset++
	}
}

func (w *wireTerminal) step(b byte) {
	switch w.state {
	case wireGround:
		w.ground(b)
	case wireEscape:
		w.escape(b)
	case wireCSI:
		w.csiByte(b)
	case wireOSC:
		switch b {
		case 0x07:
			w.state = wireGround
		case 0x1b:
			w.state = wireOSCEscape
		}
	case wireOSCEscape:
		if b != '\\' {
			w.violate("OSC cut by %q", b)
		}
		w.state = wireGround
	}
}

func (w *wireTerminal) ground(b byte) {
	if len(w.pending) > 0 || b >= 0x80 {
		w.pending = append(w.pending, b)
		if !utf8.FullRune(w.pending) {
			return
		}
		r, size := utf8.DecodeRune(w.pending)
		if r == utf8.RuneError || size != len(w.pending) {
			w.violate("broken UTF-8 %q", w.pending)
		} else {
			w.print(r)
		}
		w.pending = w.pending[:0]
		return
	}
	switch b {
	case 0x1b:
		w.state = wireEscape
	case '\r':
		w.col, w.wrapNext = 0, false
	case '\n':
		w.lineFeed()
	case '\b':
		w.col, w.wrapNext = max(w.col-1, 0), false
	case '\t':
		w.col = min(w.col+8-w.col%8, w.cols-1)
	default:
		if b >= 0x20 && b != 0x7f {
			w.print(rune(b))
		}
	}
}

func (w *wireTerminal) escape(b byte) {
	w.state = wireGround
	switch b {
	case '[':
		w.state = wireCSI
		w.sequence = w.sequence[:0]
	case ']':
		w.state = wireOSC
	case '7':
		// Nothing but a frame saves the cursor here.
		if w.inBlock {
			w.violate("frame inside a renderer block")
		}
		w.state = wireEnvelope
		w.envelope = append(w.envelope[:0], wireEnvelopeStart...)
		w.savedRow, w.savedCol = w.row, w.col
	case 'P':
		w.violate("DCS without the cursor save around it")
	case '\\':
		w.violate("stray ST")
	default:
		w.violate("unknown escape %q", b)
	}
}

func (w *wireTerminal) csiByte(b byte) {
	switch {
	case b >= 0x40 && b <= 0x7e:
		w.state = wireGround
		w.sequence = append(w.sequence, b)
		w.csi(string(w.sequence))
	case b >= 0x20 && b <= 0x3f:
		w.sequence = append(w.sequence, b)
	default:
		w.violate("CSI %q cut by %q", w.sequence, b)
		w.state = wireGround
		if b == 0x1b {
			w.state = wireEscape
		}
	}
}

// csiParams reads the numeric parameters of a sequence, missing ones as -1.
func csiParams(params string, want int) []int {
	out := make([]int, want)
	fields := strings.Split(params, ";")
	for i := range out {
		out[i] = -1
		if i < len(fields) {
			if n, err := strconv.Atoi(fields[i]); err == nil {
				out[i] = n
			}
		}
	}
	return out
}

func (w *wireTerminal) csi(seq string) {
	final, params := seq[len(seq)-1], seq[:len(seq)-1]
	wiped := w.afterWipe
	w.afterWipe = false
	one := func(fallback int) int {
		if n := csiParams(params, 1)[0]; n > 0 {
			return n
		}
		return fallback
	}
	switch final {
	case 'H', 'f':
		w.position(params, wiped)
	case 'A':
		w.moveTo(w.row-one(1), w.col)
	case 'B':
		w.moveTo(w.row+one(1), w.col)
	case 'C':
		w.moveTo(w.row, w.col+one(1))
	case 'D':
		w.moveTo(w.row, w.col-one(1))
	case 'G':
		w.moveTo(w.row, one(1)-1)
	case 'd':
		w.moveTo(one(1)-1, w.col)
	case 'J':
		w.eraseScreen(params)
	case 'K':
		w.eraseLine(params)
	case 'h', 'l':
		w.mode(params, final == 'h')
	case 'c':
		_, _ = io.WriteString(w.answer, da1Answer)
	case 'n':
		if params == "6" {
			_, _ = fmt.Fprintf(w.answer, "\x1b[%d;%dR", w.row+1, w.col+1)
		}
	case 't':
		if params == "16" {
			_, _ = fmt.Fprintf(w.answer, "\x1b[6;%d;%dt", wireCellHeight, wireCellWidth)
		}
	case 'm':
	default:
		w.violate("unhandled CSI %q", seq)
	}
}

// position moves the cursor. Bubble Tea opens a block with a bare home and closes
// it on the last row; the home that follows a screen clear stands alone.
func (w *wireTerminal) position(params string, wiped bool) {
	if params == "" {
		w.moveTo(0, 0)
		if !wiped {
			w.inBlock = true
		}
		return
	}
	at := csiParams(params, 2)
	w.moveTo(max(at[0], 1)-1, max(at[1], 1)-1)
	w.inBlock = false
}

func (w *wireTerminal) moveTo(row, col int) {
	w.row = min(max(row, 0), w.rows-1)
	w.col = min(max(col, 0), w.cols-1)
	w.wrapNext = false
}

func (w *wireTerminal) eraseScreen(params string) {
	switch params {
	case "2", "3":
		w.clears++
		w.afterWipe = true
		for i := range w.cells {
			w.cells[i] = blankCells(w.cols)
		}
	case "", "0":
		w.eraseCells(w.row, w.col, w.cols)
		for i := w.row + 1; i < w.rows; i++ {
			w.cells[i] = blankCells(w.cols)
		}
	default:
		w.violate("unhandled erase %q", params)
	}
}

func (w *wireTerminal) eraseLine(params string) {
	switch params {
	case "", "0":
		w.eraseCells(w.row, w.col, w.cols)
	case "1":
		w.eraseCells(w.row, 0, w.col+1)
	case "2":
		w.eraseCells(w.row, 0, w.cols)
	default:
		w.violate("unhandled erase %q", params)
	}
}

func (w *wireTerminal) eraseCells(row, from, to int) {
	for i := from; i < to; i++ {
		w.cells[row][i] = wireCell{text: ' '}
	}
}

func (w *wireTerminal) mode(params string, on bool) {
	if params != "?1049" {
		return
	}
	w.altScreen = on
	w.leftAlt = w.leftAlt || !on
	for i := range w.cells {
		w.cells[i] = blankCells(w.cols)
	}
	w.moveTo(0, 0)
}

func (w *wireTerminal) print(r rune) {
	width := ansi.StringWidth(string(r))
	if width == 0 {
		return
	}
	if w.wrapNext || w.col+width > w.cols {
		w.col, w.wrapNext = 0, false
		w.lineFeed()
	}
	w.cells[w.row][w.col] = wireCell{text: r}
	if width == 2 && w.col+1 < w.cols {
		w.cells[w.row][w.col+1] = wireCell{}
	}
	w.col += width
	if w.col >= w.cols {
		w.col, w.wrapNext = w.cols-1, true
	}
}

func (w *wireTerminal) lineFeed() {
	if w.row == w.rows-1 {
		w.scroll(1)
		return
	}
	w.row++
}

func (w *wireTerminal) scroll(lines int) {
	for range min(lines, w.rows) {
		w.cells = append(w.cells[1:], blankCells(w.cols))
	}
}

// takeEnvelope collects a frame up to its closing bytes and returns how much of
// chunk it used.
func (w *wireTerminal) takeEnvelope(chunk []byte) int {
	from := max(len(w.envelope)-len(wireEnvelopeEnd)+1, 0)
	w.envelope = append(w.envelope, chunk...)
	at := bytes.Index(w.envelope[from:], wireEnvelopeEnd)
	if at < 0 {
		if len(w.envelope) > wireMaxEnvelope {
			w.violate("frame never closed")
			w.state = wireGround
		}
		return len(chunk)
	}
	end := from + at + len(wireEnvelopeEnd)
	used := len(chunk) - (len(w.envelope) - end)
	w.envelope = w.envelope[:end]
	w.closeEnvelope()
	w.state = wireGround
	return used
}

func (w *wireTerminal) closeEnvelope() {
	match := wireEnvelopeHead.FindSubmatch(w.envelope[:min(len(w.envelope), 96)])
	if match == nil {
		w.violate("frame with a wrong head: %q", w.envelope[:min(len(w.envelope), 64)])
		return
	}
	var n [7]int
	for i := range n {
		n[i], _ = strconv.Atoi(string(match[i+1]))
	}
	frame := wireFrame{
		at: time.Now(), offset: w.offset, row: n[0], col: n[1], width: n[2], height: n[3],
		seq: n[4]*10000 + n[5]*100 + n[6],
	}
	head := fmt.Sprintf("\x1b7\x1b[?80l\x1b[%d;%dH", frame.row, frame.col)
	want := append(append([]byte(head), syntheticFrame(frame.seq, frame.width, frame.height)...), "\x1b8"...)
	if !bytes.Equal(w.envelope, want) {
		at := 0
		for at < len(want) && at < len(w.envelope) && want[at] == w.envelope[at] {
			at++
		}
		w.violate("frame %d differs from what the player wrote at byte %d: %q", frame.seq, at,
			w.envelope[at:min(at+48, len(w.envelope))])
		return
	}
	if w.leftAlt {
		w.violate("frame %d after the alternate screen", frame.seq)
	}
	if frame.seq < w.seqFloor {
		w.violate("frame %d after frame %d", frame.seq, w.seqFloor)
	}
	w.seqFloor = frame.seq
	w.frames = append(w.frames, frame)
	w.paint(frame)
}

func (w *wireTerminal) paint(f wireFrame) {
	top := f.row - 1
	lines := (f.height + wireCellHeight - 1) / wireCellHeight
	if over := top + lines - w.rows; over > 0 {
		w.scroll(over)
		w.scrolled++
		top -= over
	}
	width := (f.width + wireCellWidth - 1) / wireCellWidth
	for row := max(top, 0); row < top+lines && row < w.rows; row++ {
		for col := f.col - 1; col < f.col-1+width && col < w.cols; col++ {
			w.cells[row][col].image = true
		}
	}
	w.row, w.col, w.wrapNext = w.savedRow, w.savedCol, false
}

// newSession forgets the sequence: a new player counts from the start.
func (w *wireTerminal) newSession() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seqFloor = 0
}

func (w *wireTerminal) frameCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.frames)
}

func (w *wireTerminal) framesSince(mark int) []wireFrame {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]wireFrame(nil), w.frames[mark:]...)
}

func (w *wireTerminal) clearCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.clears
}

func (w *wireTerminal) scrolls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scrolled
}

func (w *wireTerminal) received() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.offset
}

// problems is what went wrong so far.
func (w *wireTerminal) problems() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.violations...)
}

// unfinished reports a sequence the stream ended inside of.
func (w *wireTerminal) unfinished() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state != wireGround || len(w.pending) > 0
}

func (w *wireTerminal) lineLocked(row int) string {
	var b strings.Builder
	for _, cell := range w.cells[row] {
		if cell.text != 0 {
			b.WriteRune(cell.text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// text is the screen as a person reads it, one line per row.
func (w *wireTerminal) text() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := make([]string, w.rows)
	for i := range lines {
		lines[i] = w.lineLocked(i)
	}
	return strings.Join(lines, "\n")
}

// picture draws the screen with the image as #, for a capture in a report.
func (w *wireTerminal) picture() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var b strings.Builder
	for _, line := range w.cells {
		for _, cell := range line {
			switch {
			case cell.image:
				b.WriteByte('#')
			case cell.text != 0:
				b.WriteRune(cell.text)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (w *wireTerminal) contains(want string) bool {
	return strings.Contains(w.text(), want)
}

// panelText returns the lines of the panel that hold any text.
func (w *wireTerminal) panelText(panelRows int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var dirty []string
	for row := range min(panelRows, w.rows) {
		if line := w.lineLocked(row); line != "" {
			dirty = append(dirty, fmt.Sprintf("row %d: %q", row+1, line))
		}
	}
	return dirty
}

// imageCells counts the cells the image covers inside and outside the panel.
func (w *wireTerminal) imageCells(panelRows int) (inside, outside int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for row, line := range w.cells {
		for _, cell := range line {
			switch {
			case !cell.image:
			case row < panelRows:
				inside++
			default:
				outside++
			}
		}
	}
	return inside, outside
}
