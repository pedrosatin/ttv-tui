package player

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// terminalRecorder stands in for the TTY. It takes each Write byte by byte,
// yielding along the way, so a second writer that got in without the lock would
// land inside the block and show up in the recorded stream.
type terminalRecorder struct {
	writers  atomic.Int32
	overlaps atomic.Int32
	slow     bool

	mu     sync.Mutex
	data   []byte
	stamps []time.Time
}

func (r *terminalRecorder) Write(p []byte) (int, error) {
	if r.writers.Add(1) > 1 {
		r.overlaps.Add(1)
	}
	defer r.writers.Add(-1)
	if bytes.Contains(p, []byte("\x1bP")) {
		r.mu.Lock()
		r.stamps = append(r.stamps, time.Now())
		r.mu.Unlock()
	}
	step := len(p)
	if r.slow {
		step = 512
	}
	for start := 0; start < len(p); start += step {
		end := min(start+step, len(p))
		r.mu.Lock()
		r.data = append(r.data, p[start:end]...)
		r.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

func (r *terminalRecorder) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Clone(r.data)
}

func (r *terminalRecorder) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.stamps)
}

func (r *terminalRecorder) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.stamps...)
}

// reset forgets what was recorded, as if the test started from this point.
func (r *terminalRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data, r.stamps = nil, nil
}

func newTestSixel(t *testing.T, rec *terminalRecorder) *SixelOutput {
	t.Helper()
	o := NewSixelOutput(rec, SixelOptions{Cell: func() (int, int) { return 10, 20 }})
	t.Cleanup(func() { _ = o.Close() })
	return o
}

var testPanel = SixelPanel{Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true, Playing: true}

// sized builds a frame declaring a size, with an id in the payload.
func sized(id, width, height int) []byte {
	return fmt.Appendf(nil, "\x1bPq\"1;1;%d;%d#%d~~~~-\x1b\\", width, height, id)
}

func frame(id int) []byte { return sized(id, 480, 270) }

func framed(row, col int, f []byte) string {
	return fmt.Sprintf("\x1b7\x1b[?80l\x1b[%d;%dH%s\x1b8", row, col, f)
}

func waitFrames(t *testing.T, rec *terminalRecorder, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for rec.frames() < want {
		if time.Now().After(deadline) {
			t.Fatalf("frames = %d, want %d\nstream: %q", rec.frames(), want, rec.bytes())
		}
		time.Sleep(time.Millisecond)
	}
}

// settle gives the output time to emit anything it still owes.
func settle() { time.Sleep(3 * sixelFrameInterval) }

// splitEmissions walks the stream and returns what is outside image sequences.
// Every DCS must be the exact wrapper around a frame: anything else in between
// means two writers mixed.
func splitEmissions(t *testing.T, stream []byte, row, col int) (text []byte, frames [][]byte) {
	t.Helper()
	prefix := fmt.Appendf(nil, "\x1b7\x1b[?80l\x1b[%d;%dH", row, col)
	for len(stream) > 0 {
		dcs := bytes.Index(stream, []byte("\x1bP"))
		if dcs < 0 {
			return append(text, stream...), frames
		}
		if dcs < len(prefix) || !bytes.Equal(stream[dcs-len(prefix):dcs], prefix) {
			t.Fatalf("DCS without its exact prefix: %q", stream[max(dcs-32, 0):dcs+8])
		}
		text = append(text, stream[:dcs-len(prefix)]...)
		end := bytes.Index(stream[dcs:], []byte("\x1b\\"))
		if end < 0 {
			t.Fatalf("unterminated DCS: %q", stream[dcs:])
		}
		end += dcs + 2
		body := stream[dcs:end]
		if _, _, ok := sixelRaster(body); !ok {
			t.Fatalf("damaged frame: %q", body)
		}
		if !bytes.HasPrefix(stream[end:], []byte("\x1b8")) {
			t.Fatalf("frame not followed by cursor restore: %q", stream[end:min(end+16, len(stream))])
		}
		frames = append(frames, body)
		stream = stream[end+2:]
	}
	return text, frames
}

func TestConcurrentWritersNeverInterleave(t *testing.T) {
	rec := &terminalRecorder{slow: true}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)

	// Frames are large against the recorder's step, so a stray renderer write
	// has plenty of room to fall inside one.
	payload := strings.Repeat("!100~-", 2000)
	known := make(map[string]bool)
	var frameList [][]byte
	for id := range 8 {
		f := fmt.Appendf(nil, "\x1bPq\"1;1;480;270#%d%s\x1b\\", id, payload)
		known[string(f)] = true
		frameList = append(frameList, f)
	}

	const renderers, blocks = 4, 150
	stop := make(chan struct{})
	var video, render sync.WaitGroup
	video.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				o.Submit(bytes.Clone(frameList[i%len(frameList)]))
				time.Sleep(time.Millisecond)
			}
		}
	})
	for w := range renderers {
		render.Go(func() {
			for i := range blocks {
				block := fmt.Sprintf("<r%d:%04d|%s>", w, i, strings.Repeat("x", 700))
				if _, err := o.Write([]byte(block)); err != nil {
					t.Error(err)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
	render.Wait()
	settle()
	close(stop)
	video.Wait()
	_ = o.Close()

	if n := rec.overlaps.Load(); n != 0 {
		t.Fatalf("%d writes overlapped at the terminal", n)
	}
	text, frames := splitEmissions(t, rec.bytes(), 1, 1)
	if len(frames) == 0 {
		t.Fatal("no frame emitted")
	}
	for _, f := range frames {
		if !known[string(f)] {
			t.Fatalf("frame changed on its way out: %.60q", f)
		}
	}
	// Each renderer block must survive whole and in its writer's order.
	next := make([]int, renderers)
	rest := string(text)
	for rest != "" {
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			t.Fatalf("renderer block cut: %.40q", rest)
		}
		var w, i int
		block := rest[:end+1]
		if _, err := fmt.Sscanf(block, "<r%d:%04d|", &w, &i); err != nil {
			t.Fatalf("renderer block damaged: %.40q: %v", block, err)
		}
		if want := fmt.Sprintf("<r%d:%04d|%s>", w, i, strings.Repeat("x", 700)); block != want {
			t.Fatalf("renderer block damaged: %.40q", block)
		}
		if i != next[w] {
			t.Fatalf("writer %d block %d after %d", w, i, next[w])
		}
		next[w]++
		rest = rest[end+1:]
	}
	for w, n := range next {
		if n != blocks {
			t.Fatalf("writer %d: %d blocks reached the terminal", w, n)
		}
	}
}

func TestFrameIsWrappedForThePanelPosition(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	panel := testPanel
	panel.Row, panel.Col = 5, 3
	o.SetPanel(panel)
	f := frame(1)
	o.Submit(bytes.Clone(f))
	waitFrames(t, rec, 1)
	if got, want := string(rec.bytes()), framed(5, 3, f); got != want {
		t.Fatalf("stream = %q, want %q", got, want)
	}
}

func TestRateNeverPassesFifteenPerSecond(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)

	// Frames at 200 fps plus renderer damage, which also asks for emissions.
	start := time.Now()
	for i := 0; time.Since(start) < 1500*time.Millisecond; i++ {
		o.Submit(frame(i))
		if _, err := o.Write([]byte("text")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = o.Close()

	stamps := rec.times()
	if len(stamps) < 10 {
		t.Fatalf("only %d frames in 1.5s", len(stamps))
	}
	for i := 15; i < len(stamps); i++ {
		if span := stamps[i].Sub(stamps[i-15]); span <= time.Second {
			t.Fatalf("16 frames in %v", span)
		}
	}
}

func TestSubmitNeverWaitsForTheTerminal(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	out := writerFunc(func(p []byte) (int, error) {
		once.Do(func() { close(blocked) })
		<-release
		return len(p), nil
	})
	o := NewSixelOutput(out, SixelOptions{})
	defer func() { _ = o.Close() }()
	defer close(release)
	o.SetPanel(testPanel)
	o.Submit(frame(1))
	<-blocked

	done := make(chan struct{})
	go func() {
		for i := range 1000 {
			o.Submit(frame(i))
		}
		o.SetPanel(testPanel)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Submit or SetPanel waited for a blocked terminal")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestHiddenPanelEmitsNothing(t *testing.T) {
	for name, panel := range map[string]SixelPanel{
		"off":         {},
		"hidden":      {Row: 1, Col: 1, Rows: 18, Cols: 120, Playing: true},
		"not playing": {Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true},
		"no rows":     {Row: 1, Col: 1, Cols: 120, Visible: true, Playing: true},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &terminalRecorder{}
			o := newTestSixel(t, rec)
			o.SetPanel(panel)
			for i := range 10 {
				o.Submit(frame(i))
				if _, err := o.Write([]byte("text")); err != nil {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			settle()
			if n := rec.frames(); n != 0 {
				t.Fatalf("%d frames emitted", n)
			}
			if got := string(rec.bytes()); got != strings.Repeat("text", 10) {
				t.Fatalf("stream = %q", got)
			}
		})
	}
}

func TestHidingStopsFramesAndForgetsTheLastOne(t *testing.T) {
	for name, hidden := range map[string]SixelPanel{
		"panel hidden":   {},
		"player stopped": {Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &terminalRecorder{}
			o := newTestSixel(t, rec)
			o.SetPanel(testPanel)
			o.Submit(frame(1))
			waitFrames(t, rec, 1)

			o.SetPanel(hidden)
			o.Submit(frame(2))
			if _, err := o.Write([]byte("\x1b[2J")); err != nil {
				t.Fatal(err)
			}
			settle()
			if n := rec.frames(); n != 1 {
				t.Fatalf("frames = %d after hiding", n)
			}

			// Showing again waits for a new frame: the old one may be another
			// channel's, or a size that no longer applies.
			o.SetPanel(testPanel)
			if _, err := o.Write([]byte("\x1b[2J")); err != nil {
				t.Fatal(err)
			}
			settle()
			if n := rec.frames(); n != 1 {
				t.Fatalf("stale frame came back: frames = %d", n)
			}
		})
	}
}

func TestClearAndResizeBringTheImageBack(t *testing.T) {
	for name, damage := range map[string]func(*SixelOutput) error{
		"clear": func(o *SixelOutput) error {
			_, err := o.Write([]byte("\x1b[2J\x1b[H"))
			return err
		},
		"resize": func(o *SixelOutput) error {
			panel := testPanel
			panel.Rows, panel.Cols = 16, 100
			o.SetPanel(panel)
			return nil
		},
		"repaint over the panel": func(o *SixelOutput) error {
			_, err := o.Write([]byte("\x1b[H" + strings.Repeat(" ", 120) + "\r\n"))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &terminalRecorder{}
			o := newTestSixel(t, rec)
			o.SetPanel(testPanel)
			f := frame(7)
			o.Submit(bytes.Clone(f))
			waitFrames(t, rec, 1)
			settle()
			if n := rec.frames(); n != 1 {
				t.Fatalf("frames = %d before the damage", n)
			}

			rec.reset()
			if err := damage(o); err != nil {
				t.Fatal(err)
			}
			// No new frame arrives: the stream may be stalled or buffering.
			waitFrames(t, rec, 1)
			settle()
			stream := rec.bytes()
			if !bytes.HasSuffix(stream, []byte(framed(1, 1, f))) {
				t.Fatalf("the image is not the last thing written: %q", stream)
			}
			if n := rec.frames(); n != 1 {
				t.Fatalf("frames = %d, want one reemission", n)
			}
		})
	}
}

func TestReemissionFollowsTheLastRendererWrite(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)
	f := frame(1)
	o.Submit(bytes.Clone(f))
	waitFrames(t, rec, 1)

	// A clear is a burst: erase, home, then the repaint on a later tick. An
	// emission between them is erased, so the last word must be the image.
	for _, block := range []string{"\x1b[2J", "\x1b[H", "\r\nrepaint a", "\r\nrepaint b"} {
		if _, err := o.Write([]byte(block)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(sixelFrameInterval / 2)
	}
	settle()
	if stream := rec.bytes(); !bytes.HasSuffix(stream, []byte(framed(1, 1, f))) {
		t.Fatalf("a renderer write was left on top of the image: %q", stream)
	}
}

func TestNoReemissionWithoutDamage(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)
	o.Submit(frame(1))
	waitFrames(t, rec, 1)
	// The loop wakes up for these, and finds nothing damaged.
	for range 3 {
		o.SetPanel(testPanel)
		o.Submit([]byte("not a frame"))
		settle()
	}
	if n := rec.frames(); n != 1 {
		t.Fatalf("frames = %d with nothing to redraw", n)
	}
}

func TestFrameLargerThanThePanelIsNotDrawn(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)
	o.Submit(sized(1, 480, 270))
	waitFrames(t, rec, 1)

	// 5 rows of 20px hold 100px: the 270px image would cover the chat.
	small := testPanel
	small.Rows = 5
	o.SetPanel(small)
	if _, err := o.Write([]byte("\x1b[2J")); err != nil {
		t.Fatal(err)
	}
	o.Submit(sized(2, 480, 270))
	settle()
	if n := rec.frames(); n != 1 {
		t.Fatalf("oversized frame drawn: frames = %d", n)
	}
	o.Submit(sized(3, 168, 96))
	waitFrames(t, rec, 2)
}

func TestFrameSizeFollowsThePanel(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rows, cols    int
		cellW, cellH  int
		width, height int
	}{
		{"room to spare", 18, 120, 10, 20, 480, 270},
		{"short panel", 5, 120, 10, 20, 177, 96},
		{"narrow panel", 18, 30, 10, 20, 300, 168},
		{"unknown cell", 18, 120, 0, 0, 480, 270},
		{"unknown cell gives up a row", 10, 120, 0, 0, 256, 144},
		{"unknown cell gives up a column", 18, 30, 0, 0, 232, 126},
		{"unknown cell in a tiny panel", 1, 1, 0, 0, 480, 270},
		{"known cell keeps the whole panel", 10, 120, 8, 16, 284, 156},
		{"small known cell", 18, 120, 6, 12, 384, 216},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resized := make(chan [2]int, 1)
			o := NewSixelOutput(&terminalRecorder{}, SixelOptions{
				Cell:   func() (int, int) { return tc.cellW, tc.cellH },
				Resize: func(w, h int) { resized <- [2]int{w, h} },
			})
			defer func() { _ = o.Close() }()
			o.SetPanel(SixelPanel{Row: 1, Col: 1, Rows: tc.rows, Cols: tc.cols, Visible: true})
			if w, h := o.FrameSize(); w != tc.width || h != tc.height {
				t.Fatalf("size %dx%d, want %dx%d", w, h, tc.width, tc.height)
			}
			if tc.height%sixelBandHeight != 0 {
				t.Fatalf("height %d splits a sixel band", tc.height)
			}
			if tc.width == sixelMaxWidth {
				return
			}
			select {
			case got := <-resized:
				if got != [2]int{tc.width, tc.height} {
					t.Fatalf("player asked for %v", got)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("player not asked to resize")
			}
		})
	}
}

func TestDraggedResizeReconfiguresThePlayerOnce(t *testing.T) {
	var calls atomic.Int32
	o := NewSixelOutput(&terminalRecorder{}, SixelOptions{
		Cell:   func() (int, int) { return 10, 20 },
		Resize: func(int, int) { calls.Add(1) },
	})
	defer func() { _ = o.Close() }()
	for rows := 3; rows < 10; rows++ {
		o.SetPanel(SixelPanel{Row: 1, Col: 1, Rows: rows, Cols: 120, Visible: true})
		time.Sleep(sixelPlayerDelay / 10)
	}
	time.Sleep(2 * sixelPlayerDelay)
	if n := calls.Load(); n != 1 {
		t.Fatalf("player reconfigured %d times", n)
	}
}

func TestLeavingAltScreenStopsFrames(t *testing.T) {
	rec := &terminalRecorder{}
	o := newTestSixel(t, rec)
	o.SetPanel(testPanel)
	o.Submit(frame(1))
	waitFrames(t, rec, 1)
	if _, err := o.Write([]byte(altScreenExit)); err != nil {
		t.Fatal(err)
	}
	o.Submit(frame(2))
	settle()
	if n := rec.frames(); n != 1 {
		t.Fatalf("frame drawn over the shell: frames = %d", n)
	}
	if _, err := o.Write([]byte(altScreenEnter)); err != nil {
		t.Fatal(err)
	}
	o.Submit(frame(3))
	waitFrames(t, rec, 2)
}

func TestFailedFrameWriteClosesTheSequence(t *testing.T) {
	var stream bytes.Buffer
	var failed atomic.Bool
	out := writerFunc(func(p []byte) (int, error) {
		if bytes.Contains(p, []byte("\x1bP")) && failed.CompareAndSwap(false, true) {
			stream.Write(p[:len(p)/2])
			return len(p) / 2, errors.New("short write")
		}
		return stream.Write(p)
	})
	o := NewSixelOutput(out, SixelOptions{Cell: func() (int, int) { return 10, 20 }})
	o.SetPanel(testPanel)
	o.Submit(frame(1))
	deadline := time.Now().Add(2 * time.Second)
	for !failed.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	settle()
	o.Submit(frame(2))
	settle()
	_ = o.Close()
	if !bytes.HasSuffix(stream.Bytes(), []byte("\x1b\\\x1b8")) {
		t.Fatalf("sequence left open for the renderer: %q", stream.Bytes())
	}
	if n := bytes.Count(stream.Bytes(), []byte("\x1bP")); n != 1 {
		t.Fatalf("kept drawing on a broken output: %d frames", n)
	}
}

func TestCloseStopsTheGoroutineAndKeepsWrites(t *testing.T) {
	before := runtime.NumGoroutine()
	rec := &terminalRecorder{}
	o := NewSixelOutput(rec, SixelOptions{Cell: func() (int, int) { return 10, 20 }})
	o.SetPanel(testPanel)
	o.Submit(frame(1))
	waitFrames(t, rec, 1)
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	o.Submit(frame(2))
	if _, err := o.Write([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	settle()
	if n := rec.frames(); n != 1 {
		t.Fatalf("frames = %d after Close", n)
	}
	if !bytes.HasSuffix(rec.bytes(), []byte("bye")) {
		t.Fatal("write lost after Close")
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
}

// A frame sized for the whole panel reaches past it when the cell is a guess: it
// waits for the player to deliver the smaller size, as any oversized frame does.
func TestGuessedCellKeepsFramesInsideTheSlack(t *testing.T) {
	rec := &terminalRecorder{}
	o := NewSixelOutput(rec, SixelOptions{})
	defer func() { _ = o.Close() }()
	o.SetPanel(SixelPanel{Row: 1, Col: 1, Rows: 10, Cols: 120, Visible: true, Playing: true})
	o.Submit(sized(1, 284, 156))
	settle()
	if n := rec.frames(); n != 0 {
		t.Fatalf("%d frames over the slack", n)
	}
	o.Submit(sized(2, 256, 144))
	waitFrames(t, rec, 1)
}

// trackRecorder collects what the output tells the player, in order.
type trackRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *trackRecorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *trackRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func newTrackedSixel(t *testing.T) (*SixelOutput, *trackRecorder) {
	t.Helper()
	rec := &trackRecorder{}
	o := NewSixelOutput(&terminalRecorder{}, SixelOptions{
		Cell:   func() (int, int) { return 10, 20 },
		Resize: func(width, height int) { rec.add(fmt.Sprintf("size %dx%d", width, height)) },
		Track:  func(on bool) { rec.add(fmt.Sprintf("track %v", on)) },
	})
	t.Cleanup(func() { _ = o.Close() })
	return o, rec
}

func waitPlayerCalls(t *testing.T, rec *trackRecorder, want ...string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !slices.Equal(rec.snapshot(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("calls = %q, want %q", rec.snapshot(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTrackFollowsThePanel(t *testing.T) {
	o, rec := newTrackedSixel(t)
	o.SetPanel(testPanel)
	settleTrack()
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("mpv starts with video on, nothing to say: %q", got)
	}
	o.SetPanel(SixelPanel{})
	waitPlayerCalls(t, rec, "track false")
	if o.PanelVisible() {
		t.Fatal("panel still visible")
	}
	o.SetPanel(testPanel)
	waitPlayerCalls(t, rec, "track false", "track true")
}

// Help opened and closed at once costs no video output rebuild.
func TestQuickHideAndShowLeavesTheTrackAlone(t *testing.T) {
	o, rec := newTrackedSixel(t)
	o.SetPanel(testPanel)
	settleTrack()
	o.SetPanel(SixelPanel{})
	time.Sleep(sixelPlayerDelay / 4)
	o.SetPanel(testPanel)
	settleTrack()
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("calls = %q", got)
	}
}

func TestPausedPanelKeepsTheTrack(t *testing.T) {
	o, rec := newTrackedSixel(t)
	o.SetPanel(testPanel)
	paused := testPanel
	paused.Playing = false
	o.SetPanel(paused)
	settleTrack()
	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("calls = %q", got)
	}
}

// The window changes while the panel is hidden: video must come back at the new
// size in one rebuild, so the size has to reach the player first.
func TestSizeReachesThePlayerBeforeTheTrack(t *testing.T) {
	o, rec := newTrackedSixel(t)
	o.SetPanel(testPanel)
	settleTrack()
	o.SetPanel(SixelPanel{})
	waitPlayerCalls(t, rec, "track false")
	smaller := testPanel
	smaller.Rows = 5
	o.SetPanel(smaller)
	waitPlayerCalls(t, rec, "track false", "size 177x96", "track true")
}

func settleTrack() { time.Sleep(2 * sixelPlayerDelay) }
