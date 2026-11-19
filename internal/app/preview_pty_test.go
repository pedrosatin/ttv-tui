//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPreviewWithoutMainVideo_PTY covers ticket 01 end to end on a real TUI:
// Space opens a preview with no main video, Esc closes it, Space opens again,
// Enter closes the preview and opens the channel by the normal path.
func TestPreviewWithoutMainVideo_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RICH=1", "TTV_APP_PREVIEW=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")
	p.waitScreen(0, "PtyCategory")

	// Enter the category so a channel row is highlighted.
	off := p.mark()
	p.send([]byte("\r"))
	p.waitScreen(off, "PtyChan")

	// Space → preview starts (status + fake player process).
	off = p.mark()
	p.send([]byte(" "))
	screen := p.waitScreen(off, "Preview")
	if !strings.Contains(screen, "ptychan") && !strings.Contains(screen, "PtyChan") {
		t.Fatalf("preview status missing channel name:\n%s", screen)
	}
	p.waitPreviewPlayer()
	targetLog := filepath.Join(p.dir, "player_targets.log")
	waitTargetLog(t, targetLog, "ptychan")
	t.Logf("PTY CAPTURE Space opened preview:\n%s", screen)

	previewPid := p.playerPid

	// Esc closes the preview: process dies and the soft status returns.
	off = p.mark()
	p.send([]byte{0x1b}) // Esc
	waitProcessGone(t, previewPid, 5*time.Second)
	p.waitScreen(off, "Selected: PtyCategory")
	if strings.Contains(p.screenSince(off), "Preview") {
		t.Fatalf("preview status still on the bar after Esc:\n%s", p.screenSince(off))
	}

	// Space again → preview of the same channel.
	off = p.mark()
	p.send([]byte(" "))
	p.waitScreen(off, "Preview")
	p.waitPreviewPlayer()
	previewPid = p.playerPid

	// Enter closes preview and opens the channel (chat + main player).
	off = p.mark()
	p.send([]byte("\r"))
	p.waitScreen(off, "Chat — #ptychan")
	waitProcessGone(t, previewPid, 5*time.Second)
	// Main player overwrites the same pid files; wait for a live new group.
	p.followPlayer(previewPid)
	t.Logf("PTY CAPTURE Enter opened channel from preview; main pid=%d", p.playerPid)
}

// waitPreviewPlayer records the fake player the preview helper just spawned.
// It ignores a stale pid file left by a previous preview that already exited.
func (p *ptyTUI) waitPreviewPlayer() {
	p.t.Helper()
	old := p.playerPid
	deadline := time.Now().Add(10 * time.Second)
	for {
		pid := readPidFile(filepath.Join(p.dir, "player.pid"))
		child := readPidFile(filepath.Join(p.dir, "child.pid"))
		if pid > 0 && pid != old && alive(pid) && child > 0 && alive(child) {
			p.playerPid = pid
			p.childPid = child
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatal("preview player group not running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readPidFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var pid int
	for _, c := range strings.TrimSpace(string(data)) {
		if c < '0' || c > '9' {
			return 0
		}
		pid = pid*10 + int(c-'0')
	}
	return pid
}

func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d survived", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitTargetLog(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("player_targets.log never mentioned %q; got:\n%s", want, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
