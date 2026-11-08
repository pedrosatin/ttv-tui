//go:build unix

package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	ctrlW = string(rune(keyCtrlW))
	ctrlX = "\x18"
	esc   = "\x1b"
	space = " "
	help  = "?"
)

// Twenty messages a second for ten seconds, then a burst, against frames at full
// rate. Every byte of the stream goes through wireTerminal, so a chat line inside
// a frame, a frame inside a repaint or a sequence cut in half fails the test.
func TestVideoStressChatFlood(t *testing.T) {
	r := startStressRig(t, stressOptions{raceTimeout: true})
	r.play()
	r.awaitPicture()

	mark, received, start := r.term.frameCount(), r.term.received(), time.Now()
	for _, phase := range []struct {
		rate int64
		hold time.Duration
	}{{20, 10 * time.Second}, {200, 2 * time.Second}} {
		r.irc.rate.Store(phase.rate)
		r.holdPicture(phase.hold)
	}
	r.irc.rate.Store(0)
	elapsed := time.Since(start)
	time.Sleep(50 * time.Millisecond)
	sent := r.irc.sent.Load()
	r.awaitText(fmt.Sprintf("msg %05d", sent))

	frames := r.term.framesSince(mark)
	unique := map[int]bool{}
	for _, f := range frames {
		unique[f.seq] = true
	}
	rate := float64(len(frames)) / elapsed.Seconds()
	t.Logf("%d chat messages, %d frames (%d distinct) in %s: %.1f fps, %.1f distinct fps, %.1f MB on the wire, %d frames scrolled the screen",
		sent, len(frames), len(unique), elapsed.Round(time.Millisecond), rate,
		float64(len(unique))/elapsed.Seconds(), float64(r.term.received()-received)/1e6, r.term.scrolls())
	if sent < 200 {
		t.Fatalf("only %d messages went out", sent)
	}
	maxFPS := float64(stressPlayerFPS) + 0.5
	if raceEnabled {
		// Instrumentation can delay delivery and bunch frames at the terminal.
		maxFPS = stressPlayerFPS * 1.5
	}
	if rate > maxFPS {
		t.Fatalf("%.1f fps is over the limit", rate)
	}
	if rate < stressPlayerFPS/2 {
		t.Fatalf("%.1f fps under the chat", rate)
	}
	if n := r.term.scrolls(); n != 0 {
		t.Fatalf("%d frames scrolled the screen", n)
	}
	t.Logf("screen at the end:\n%s", r.term.picture())
}

func TestVideoStressResize(t *testing.T) {
	r := startStressRig(t, stressOptions{})
	r.play()
	r.awaitPicture()
	r.irc.rate.Store(20)
	for _, tc := range []struct {
		name  string
		gap   time.Duration
		sizes [][2]int
	}{
		// Faster than the debounce: the player hears only of the last one.
		{"drag ends large", 30 * time.Millisecond, [][2]int{{120, 40}, {40, 15}, {90, 26}, {60, 20}, {120, 40}}},
		// Slower than it: each size reaches the player, whose frames keep arriving late.
		{"steps end small", 350 * time.Millisecond, [][2]int{{40, 15}, {120, 40}, {80, 24}, {100, 30}, {40, 15}}},
		{"steps end large", 350 * time.Millisecond, [][2]int{{60, 20}, {40, 15}, {120, 40}, {40, 15}, {120, 40}}},
	} {
		mark := r.term.frameCount()
		for _, size := range tc.sizes {
			r.resize(size[0], size[1])
			time.Sleep(tc.gap)
		}
		resized := time.Now()
		r.await("frames of the last size", func() bool {
			frames := r.term.framesSince(mark)
			fresh := frames[max(len(frames)-3, 0):]
			return len(fresh) == 3 && !slices.ContainsFunc(fresh, func(f wireFrame) bool {
				return !r.fits(f) || f.at.Before(resized)
			})
		})
		t.Logf("%s: image back %s after the last resize", tc.name, time.Since(resized).Round(time.Millisecond))
		r.awaitPicture()
		t.Logf("%s, %dx%d:\n%s", tc.name, r.cols, r.rows, r.term.picture())
	}
	t.Logf("%d frames scrolled the screen on the way", r.term.scrolls())
}

// clearOnce clears the screen and returns the frames that followed, once the image
// is back.
func clearOnce(r *stressRig) []wireFrame {
	r.t.Helper()
	clears, mark := r.term.clearCount(), r.term.frameCount()
	r.prog.Send(tea.ClearScreen())
	r.await("the clear", func() bool { return r.term.clearCount() > clears })
	r.awaitPicture()
	return r.term.framesSince(mark)
}

func TestVideoStressClear(t *testing.T) {
	t.Run("playing", func(t *testing.T) {
		r := startStressRig(t, stressOptions{})
		r.play()
		r.awaitPicture()
		r.irc.rate.Store(20)
		for range 5 {
			start := time.Now()
			frames := clearOnce(r)
			if len(frames) == 0 {
				t.Fatal("no frame after the clear")
			}
			t.Logf("first frame %s after the clear was sent", frames[0].at.Sub(start).Round(time.Millisecond))
		}
	})

	// Nothing new comes while paused, so the image can only be the last frame again.
	t.Run("paused", func(t *testing.T) {
		r := startStressRig(t, stressOptions{raceTimeout: true})
		r.play()
		r.awaitPicture()
		r.press(space)
		r.await("the pause", func() bool { return r.ipcCount(`"pause",true`) > 0 })
		time.Sleep(200 * time.Millisecond)
		mark := r.term.frameCount()
		time.Sleep(300 * time.Millisecond)
		// Frames already in flight can arrive late on any platform; they are not
		// a continuous redraw loop. More than two still indicates ongoing output.
		const maxResidual = 2
		if n := r.term.frameCount() - mark; n > maxResidual {
			t.Fatalf("%d frames with nothing new to show", n)
		}
		frames := r.term.framesSince(mark - 1)
		last := frames[len(frames)-1].seq
		for range 5 {
			for _, f := range clearOnce(r) {
				if f.seq != last {
					t.Fatalf("frame %d while paused on %d", f.seq, last)
				}
			}
		}
	})

	// Help takes the rows away and keeps no frame: what follows it is new.
	t.Run("under help", func(t *testing.T) {
		r := startStressRig(t, stressOptions{})
		r.play()
		r.awaitPicture()
		r.irc.rate.Store(20)
		for range 3 {
			clears := r.term.clearCount()
			r.press(help)
			r.await("the clear", func() bool { return r.term.clearCount() > clears })
			r.await("the help", func() bool {
				inside, outside := r.term.imageCells(r.rows)
				return inside+outside == 0 && strings.Contains(r.term.text(), "Ctrl+Q")
			})
			mark := r.term.frameCount()
			lastSeq := r.term.framesSince(mark - 1)[0].seq
			time.Sleep(400 * time.Millisecond)
			if n := r.term.frameCount() - mark; n != 0 {
				t.Fatalf("%d frames under help", n)
			}
			r.press(help)
			r.await("frames after help", func() bool { return r.term.frameCount() > mark })
			if first := r.term.framesSince(mark)[0]; first.seq <= lastSeq {
				t.Fatalf("ghost: frame %d after help, %d before it", first.seq, lastSeq)
			}
			r.awaitPicture()
		}
	})
}

func TestVideoStressListToggle(t *testing.T) {
	for name, rogue := range map[string]bool{"player obeys": false, "player keeps drawing": true} {
		t.Run(name, func(t *testing.T) {
			r := startStressRig(t, stressOptions{rogue: rogue, raceTimeout: true})
			r.play()
			r.awaitPicture()
			r.irc.rate.Store(20)
			listOpen := r.cat.T("video.list_open")

			var back time.Duration
			for i := range 10 {
				r.press("l")
				r.awaitText(listOpen)
				mark := r.term.frameCount()
				r.await("the image to go", func() bool {
					inside, outside := r.term.imageCells(r.rows)
					return inside+outside == 0
				})
				// Every third stays long enough to turn the track off.
				hold := 30 * time.Millisecond
				if i%3 == 0 {
					hold = 400 * time.Millisecond
				}
				if i == 5 {
					r.prog.Send(tea.ClearScreen())
				}
				time.Sleep(hold)
				if n := r.term.frameCount() - mark; n != 0 {
					t.Fatalf("round %d: %d frames under the list", i, n)
				}
				r.assertNoImage()
				start := time.Now()
				r.press(esc)
				r.await("the image back", func() bool { return r.term.frameCount() > mark })
				back = max(back, time.Since(start))
				r.awaitPicture()
			}
			t.Logf("image back at most %s after Esc", back.Round(time.Millisecond))

			// Nobody waits here: the keys land faster than a frame, and the clears
			// each opening asks for arrive after the list is gone.
			before, burst := r.ipcCount(`"vid"`), time.Now()
			for range 10 {
				r.press("l")
				time.Sleep(15 * time.Millisecond)
				r.press(esc)
				time.Sleep(15 * time.Millisecond)
			}
			elapsed := time.Since(burst)
			time.Sleep(500 * time.Millisecond)
			if r.term.contains(listOpen) {
				r.press(esc)
			}
			mark := r.term.frameCount()
			r.await("frames after the burst", func() bool { return r.term.frameCount() > mark })
			r.awaitPicture()
			// The debounce allows one change per 200 ms, not one per key.
			if n, most := r.ipcCount(`"vid"`)-before, int(elapsed/(200*time.Millisecond))+2; n > most {
				t.Fatalf("%d vid commands for 20 keys in %s", n, elapsed)
			}
		})
	}
}

func TestVideoStressHiddenPanel(t *testing.T) {
	for name, rogue := range map[string]bool{"player obeys": false, "player keeps drawing": true} {
		t.Run(name, func(t *testing.T) {
			r := startStressRig(t, stressOptions{rogue: rogue})
			r.play()
			r.awaitPicture()
			startsBefore := len(r.lines(r.starts))
			r.irc.rate.Store(20)
			offs, ons := r.ipcCount(vidOff), r.ipcCount(vidOn)

			clears, pressed := r.term.clearCount(), time.Now()
			r.press(ctrlW)
			r.awaitText(r.cat.T("video.audio_only"))
			r.await("vid=no", func() bool { return r.ipcCount(vidOff) > offs })
			t.Logf("vid=no reached the player %s after the key", time.Since(pressed).Round(time.Millisecond))
			r.await("the clear", func() bool { return r.term.clearCount() > clears })
			mark := r.term.frameCount()
			r.assertNoImage()

			// Every layout but video, and a resize, with the chat still writing.
			for range 3 {
				time.Sleep(300 * time.Millisecond)
				r.press(ctrlW)
			}
			r.resize(120, 40)
			time.Sleep(500 * time.Millisecond)
			r.resize(100, 30)
			time.Sleep(500 * time.Millisecond)
			if n := r.term.frameCount() - mark; n != 0 {
				t.Fatalf("%d frames with the panel hidden", n)
			}
			if n := r.ipcCount(vidOn) - ons; n != 0 {
				t.Fatalf("a hidden panel turned the track on %d times", n)
			}
			r.assertNoImage()

			r.press(ctrlW)
			r.await("vid=auto", func() bool { return r.ipcCount(vidOn) > ons })
			r.awaitPicture()
			if starts := r.lines(r.starts); len(starts) != startsBefore {
				t.Fatalf("the player restarted: %q", starts)
			}
		})
	}
}

// What a pause costs: the player sends nothing, and the last frame has to survive
// on its own.
func TestVideoStressPause(t *testing.T) {
	r := startStressRig(t, stressOptions{})
	r.play()
	r.awaitPicture()
	r.press(space)
	r.await("the pause", func() bool { return r.ipcCount(`"pause",true`) > 0 })
	time.Sleep(200 * time.Millisecond)

	// Every chat line is a write of the renderer, and a write counts as damage.
	mark := r.term.frameCount()
	r.irc.rate.Store(20)
	r.holdPicture(2 * time.Second)
	r.irc.rate.Store(0)
	t.Logf("paused under 20 msg/s: the last frame went out %d times in 2s", r.term.frameCount()-mark)

	// A smaller panel throws the last frame away, and the paused player owes one.
	start := time.Now()
	mark = r.term.frameCount()
	r.resize(40, 15)
	r.await("a frame of the new size", func() bool {
		return slices.ContainsFunc(r.term.framesSince(mark), r.fits)
	})
	t.Logf("paused, image back %s after the resize", time.Since(start).Round(time.Millisecond))
	r.awaitPicture()
}

// Stopping takes the image away for good, and the panel says so. The next channel
// comes through the list, and its picture replaces that text.
func TestVideoStressStopAndNextChannel(t *testing.T) {
	r := startStressRig(t, stressOptions{})
	r.play()
	r.awaitPicture()
	r.irc.rate.Store(20)

	r.press(ctrlX)
	placeholder := r.cat.T("video.placeholder")
	r.awaitText(placeholder)
	r.await("the image to go", func() bool {
		inside, outside := r.term.imageCells(r.rows)
		return inside+outside == 0
	})
	mark := r.term.frameCount()
	time.Sleep(500 * time.Millisecond)
	if n := r.term.frameCount() - mark; n != 0 {
		t.Fatalf("%d frames with the player gone", n)
	}
	r.assertNoImage()

	r.term.newSession()
	r.press("l")
	r.awaitText(r.cat.T("video.list_open"))
	r.press("\r")
	r.await("the new session", func() bool { return r.term.frameCount() > mark })
	r.awaitPicture()
	if r.term.contains(placeholder) {
		t.Fatalf("the panel text outlived the image:\n%s", r.term.picture())
	}
	if starts := r.lines(r.starts); len(starts) < 1 || !usesSixel(starts[len(starts)-1]) {
		t.Fatalf("starts: %q", starts)
	}
}

// A terminal slower than the video: the frames fill the PTY, and everything else
// has to take turns with them. The stream stays whole and the keys still work.
func TestVideoStressSlowTerminal(t *testing.T) {
	r := startStressRig(t, stressOptions{})
	r.play()
	r.awaitPicture()
	r.irc.rate.Store(20)
	r.term.speed.Store(300_000)
	time.Sleep(time.Second)

	listOpen := r.cat.T("video.list_open")
	var slowest time.Duration
	for range 3 {
		start := time.Now()
		r.press("l")
		r.awaitText(listOpen)
		slowest = max(slowest, time.Since(start))
		r.press(esc)
		r.await("the list to close", func() bool { return !r.term.contains(listOpen) })
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("at 300 KB/s a key took at most %s to reach the screen", slowest.Round(time.Millisecond))
	r.term.speed.Store(0)
	r.awaitPicture()
}

// The whole of Run, detection and cell query included, on the same terminal. The
// rig quits with frames running and checks the stream closed whole. A signal stays
// out: Bubble Tea's kill path closes its input reader without waiting for the read
// loop, a race of its own that the race detector would pin on this test.
func TestVideoStressThroughRun(t *testing.T) {
	r := startStressRig(t, stressOptions{viaRun: true})
	r.play()
	r.awaitPicture()
	r.irc.rate.Store(20)
	r.holdPicture(time.Second)
	r.resize(120, 40)
	mark := r.term.frameCount()
	r.await("frames after the resize", func() bool { return r.term.frameCount() > mark })
	r.awaitPicture()
}

// The report that started this: with video = "sixel" in the config and a terminal
// that can draw it, the first channel goes to the panel and no window opens.
func TestVideoOpensEmbeddedFromTheConfigThroughRun(t *testing.T) {
	r := startStressRig(t, stressOptions{viaRun: true, videoFromConfig: true, config: "video = \"sixel\"\n"})
	r.awaitText(r.cat.T("video.active"))
	r.play()
	r.awaitPicture()
	starts := r.lines(r.starts)
	if len(starts) != 1 {
		t.Fatalf("starts: %q", starts)
	}
	if strings.LastIndex(starts[0], "--force-window=no") < strings.LastIndex(starts[0], "--force-window=immediate") {
		t.Fatalf("mpv would open its window: %s", starts[0])
	}
}

// The config moves the strip: Run reads video_chat_position and video_chat_rows,
// and the image lands under a strip of that height, on the real wire.
func TestVideoChatOnTopThroughRun(t *testing.T) {
	const strip = 5
	r := startStressRig(t, stressOptions{viaRun: true, config: "video_chat_position = \"top\"\nvideo_chat_rows = 5\n"})
	r.play()
	first, last := strip, r.rows-1
	clean := time.Now()
	r.await("the image under the strip", func() bool {
		inside, outside := r.term.imageCellsIn(first, last)
		if inside == 0 || outside > 0 || len(r.term.panelTextIn(first, last)) > 0 {
			clean = time.Now()
			return false
		}
		return time.Since(clean) >= 300*time.Millisecond
	})
	lines := strings.Split(r.term.text(), "\n")
	if !strings.Contains(lines[0], "Chat") {
		t.Fatalf("the strip is not on the first row:\n%s", r.term.picture())
	}
	for _, f := range r.term.framesSince(0) {
		if f.row < first || f.height > (last-first)*wireCellHeight {
			t.Fatalf("frame at row %d, %d px high, outside the panel on rows %d..%d", f.row+1, f.height, first+1, last)
		}
	}
}
