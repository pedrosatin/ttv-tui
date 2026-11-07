package player

import (
	"bytes"
	"strings"
	"testing"
)

func demuxAll(t *testing.T, chunkSize int, input string) (frames []string, lines []string) {
	t.Helper()
	d := sixelDemux{
		onLine:  func(s string) { lines = append(lines, s) },
		onFrame: func(f []byte) { frames = append(frames, string(f)) },
	}
	data := []byte(input)
	for len(data) > 0 {
		n := min(chunkSize, len(data))
		d.feed(data[:n])
		data = data[n:]
	}
	d.flush()
	return frames, lines
}

// mpvStream reproduces the bytes captured from mpv 0.41 on a pipe.
const (
	testFrameA = "\x1bPq\"1;1;224;126#0;2;0;0;0#1!224~-\x1b\\"
	testFrameB = "\x1bPq\"1;1;224;126#0;2;9;9;9#1!224~-\x1b\\"
	mpvStream  = "\x1b[?25l\x1b[?1003h\x1b[?1070l\x1b[1;3f" + testFrameA + "\x00\x1b[1;3f" + testFrameB + "\x00\x1b[?25h\x1b[?1003l"
)

func TestDemuxKeepsOnlyFramesFromPlayerOutput(t *testing.T) {
	for _, chunk := range []int{1, 2, 3, 7, len(mpvStream)} {
		frames, lines := demuxAll(t, chunk, mpvStream)
		if len(frames) != 2 || frames[0] != testFrameA || frames[1] != testFrameB {
			t.Fatalf("chunk %d: frames = %q", chunk, frames)
		}
		if len(lines) != 0 {
			t.Fatalf("chunk %d: terminal control leaked as text: %q", chunk, lines)
		}
	}
}

func TestDemuxKeepsTextAroundFrames(t *testing.T) {
	frames, lines := demuxAll(t, 5, "first line\r\n"+testFrameA+"second\nlast")
	if len(frames) != 1 || frames[0] != testFrameA {
		t.Fatalf("frames = %q", frames)
	}
	if strings.Join(lines, "|") != "first line|second|last" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestDemuxRejectsDamagedFrames(t *testing.T) {
	for name, input := range map[string]string{
		"cancelled by CAN":     "\x1bPq\"1;1;2;2\x18tail\x1b\\",
		"cancelled by SUB":     "\x1bPq\"1;1;2;2\x1atail\x1b\\",
		"raw C1":               "\x1bPq\"1;1;2;2\x9ctail\x1b\\",
		"interrupted by ESC":   "\x1bPq\"1;1;2;2\x1b[2J",
		"never terminated":     "\x1bPq\"1;1;2;2~~~",
		"NUL inside the image": "\x1bPq\"1;1;2;2\x00~\x1b\\",
	} {
		t.Run(name, func(t *testing.T) {
			if frames, _ := demuxAll(t, 3, input); len(frames) != 0 {
				t.Fatalf("frames = %q", frames)
			}
		})
	}
}

func TestDemuxRecoversAfterDamagedFrame(t *testing.T) {
	frames, _ := demuxAll(t, 4, "\x1bPq\x18junk\x1b\\"+testFrameA)
	if len(frames) != 1 || frames[0] != testFrameA {
		t.Fatalf("frames = %q", frames)
	}
}

func TestDemuxBoundsFrameMemory(t *testing.T) {
	var frames int
	d := sixelDemux{onFrame: func([]byte) { frames++ }}
	d.feed([]byte("\x1bPq"))
	d.feed(bytes.Repeat([]byte{'~'}, maxSixelFrame+1))
	if d.frame != nil {
		t.Fatalf("kept %d bytes past the limit", len(d.frame))
	}
	d.feed([]byte("\x1b\\" + testFrameA))
	if frames != 1 {
		t.Fatalf("frames = %d, want only the one after the oversized", frames)
	}
}

func TestSixelRaster(t *testing.T) {
	for _, tc := range []struct {
		name          string
		frame         string
		width, height int
		ok            bool
	}{
		{"libsixel", testFrameA, 224, 126, true},
		{"with parameters", "\x1bP0;0;0q\"1;1;400;225~\x1b\\", 400, 225, true},
		{"no raster", "\x1bPq#0;2;0;0;0~\x1b\\", 0, 0, false},
		{"zero size", "\x1bPq\"1;1;0;0~\x1b\\", 0, 0, false},
		{"truncated raster", "\x1bPq\"1;1;400\x1b\\", 0, 0, false},
		{"DECRQSS", "\x1bP$qm\x1b\\", 0, 0, false},
		{"XTGETTCAP", "\x1bP+q544e\x1b\\", 0, 0, false},
		{"DECRQSS shaped like a raster", "\x1bP$q1;1;2;2\x1b\\", 0, 0, false},
		{"sixel without the raster mark", "\x1bPq1;1;2;2\x1b\\", 0, 0, false},
		{"not a DCS", "plain", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			width, height, ok := sixelRaster([]byte(tc.frame))
			if width != tc.width || height != tc.height || ok != tc.ok {
				t.Fatalf("got %d %d %v", width, height, ok)
			}
		})
	}
}
