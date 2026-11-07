package player

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// realStreamlinkEnv turns on the tests against real streamlink, mpv and ffmpeg.
// They need no network: a local server plays the part of the CDN.
//
//	TTV_TEST_REAL_STREAMLINK=1 go test -count=1 -v -run TestRealStreamlink ./internal/player
const realStreamlinkEnv = "TTV_TEST_REAL_STREAMLINK"

// localStreamlink makes the default command reach the real streamlink with target
// in place of the Twitch URL, which is the only argument that changes.
func localStreamlink(t *testing.T, target string) {
	t.Helper()
	if os.Getenv(realStreamlinkEnv) != "1" {
		t.Skipf("set %s=1 to run against a real streamlink", realStreamlinkEnv)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no embedded video")
	}
	real, err := exec.LookPath("streamlink")
	if err != nil {
		t.Fatalf("%s is set but streamlink is not on PATH: %v", realStreamlinkEnv, err)
	}
	script := fmt.Sprintf(`#!/bin/sh
for arg; do
	shift
	case "$arg" in
	https://www.twitch.tv/*) set -- "$@" %q ;;
	*) set -- "$@" "$arg" ;;
	esac
done
exec %q "$@"
`, target, real)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "streamlink"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// localHLS serves video as an HLS playlist at about the pace of a live one. streamlink
// closes the player when the stream ends, so a video that downloads at once ends
// before mpv can draw.
func localHLS(t *testing.T) *httptest.Server {
	t.Helper()
	if os.Getenv(realStreamlinkEnv) != "1" {
		t.Skipf("set %s=1 to run against a real streamlink", realStreamlinkEnv)
	}
	dir := t.TempDir()
	ffmpeg := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30",
		"-t", "20", "-c:v", "mpeg2video", "-q:v", "5", "-hls_time", "2", "-hls_playlist_type", "vod",
		filepath.Join(dir, "live.m3u8"))
	if out, err := ffmpeg.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	files := http.FileServer(http.Dir(dir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".ts") {
			time.Sleep(time.Second)
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// The whole path of the first real test that failed: streamlink starts with its
// real logging setup, plays through a real mpv and the frames reach the terminal,
// filling the panel, while its log reaches the session and never the frames.
func TestRealStreamlinkDrawsAndLogsToTheSession(t *testing.T) {
	server := localHLS(t)
	localStreamlink(t, "hls://"+server.URL+"/live.m3u8")

	rec := &terminalRecorder{}
	out := newTestSixel(t, rec)
	out.SetPanel(SixelPanel{Row: 1, Col: 1, Rows: 18, Cols: 120, Visible: true, Playing: true})
	mgr := New(Options{RuntimeDir: t.TempDir(), StopWait: time.Second})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("local")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ev := waitForState(t, mgr.Events(), gen, StatePlaying, 30*time.Second)
	if !regexp.MustCompile(`^\[[a-z.]+\]\[info\] `).MatchString(ev.LastLine) {
		t.Fatalf("no line of streamlink's log reached the session: %q", ev.LastLine)
	}
	t.Logf("from the log: %q", ev.LastLine)
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, frames := splitEmissions(t, bytes.Clone(rec.bytes()), 0, 0)
		if len(frames) > 0 {
			if width, height, _ := sixelRaster(frames[len(frames)-1]); width == 640 && height == 360 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d frames, none filling the panel", len(frames))
		}
		time.Sleep(50 * time.Millisecond)
	}
	mgr.Stop()
	_ = out.Close()

	// The frames of the final size stand centered in 1200x360.
	stream := rec.bytes()
	if !bytes.Contains(stream, []byte("\x1b7\x1b[?80l\x1b[1;29H")) {
		t.Fatal("no frame centered in the panel")
	}
	text, _ := splitEmissions(t, stream, 0, 0)
	if len(text) != 0 {
		t.Fatalf("the log or the player reached the terminal: %q", text)
	}
}

func TestRealStreamlinkFailureIsClassifiedFromTheLog(t *testing.T) {
	server := localHLS(t)
	localStreamlink(t, "hls://"+server.URL+"/missing.m3u8")

	out := newTestSixel(t, &terminalRecorder{})
	out.SetPanel(testPanel)
	mgr := New(Options{RuntimeDir: t.TempDir(), StopWait: time.Second})
	t.Cleanup(mgr.Close)
	mgr.SetVideoOutput(out)
	gen, err := mgr.StartEmbedded("local")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev := waitError(t, mgr, gen, 30*time.Second)
	if ev.Failure != FailureNetwork && ev.Failure != FailureOffline {
		t.Fatalf("Failure = %q, line %q", ev.Failure, ev.LastLine)
	}
	t.Logf("%s: %q", ev.Failure, ev.LastLine)
}
