//go:build unix

package app

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// oneChannelSource offers a single category with a single live channel.
type oneChannelSource struct{}

func (oneChannelSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-pty", Name: "PtyCategory"}}}, nil
}

func (oneChannelSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{{ID: "ch-pty", Login: "ptychan", DisplayName: "PtyChan"}}}, nil
}

func (oneChannelSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{{ID: "ch-pty", Login: "ptychan", DisplayName: "PtyChan"}}}, nil
}

func (oneChannelSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (oneChannelSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

func (oneChannelSource) ChannelsByIDs(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// screenSince is the PTY output written after byte offset off, without escape sequences.
func (p *ptyTUI) screenSince(off int) string {
	out := p.output()
	if off > len(out) {
		off = len(out)
	}
	return ansiSeq.ReplaceAllString(out[off:], "")
}

func (p *ptyTUI) mark() int { return len(p.output()) }

func (p *ptyTUI) send(b []byte) {
	p.t.Helper()
	if _, err := p.master.Write(b); err != nil {
		p.t.Fatalf("write %q: %v", b, err)
	}
}

// waitScreen waits until the output after offset off holds every want, and
// returns that output. A frame reaches the PTY in pieces, so one part of it on
// screen says nothing about the rest: wait for the text the test needs.
func (p *ptyTUI) waitScreen(off int, want ...string) string {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen := p.screenSince(off)
		missing := slices.DeleteFunc(slices.Clone(want), func(w string) bool {
			return strings.Contains(screen, w)
		})
		if len(missing) == 0 {
			return screen
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("screen never showed %q; got:\n%s", missing, screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sendAndWait writes b and waits until the screen redrawn afterwards contains want.
func (p *ptyTUI) sendAndWait(b []byte, want string) {
	p.t.Helper()
	off := p.mark()
	p.send(b)
	p.waitScreen(off, want)
}

// openChannel enters the category and the channel, which switches playback to
// ptychan and joins its chat; the tracked player pids follow the new player.
func (p *ptyTUI) openChannel() {
	p.t.Helper()
	oldPid := p.playerPid
	p.sendAndWait([]byte("\r"), "PtyChan")
	p.sendAndWait([]byte("\r"), "Chat — #ptychan")
	p.followPlayer(oldPid)
}

// followPlayer waits for the player that replaced oldPid and tracks it, so the
// test cleanup kills the new pair and not the one already gone.
func (p *ptyTUI) followPlayer(oldPid int) {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		pid := waitPid(p.t, filepath.Join(p.dir, "player.pid"), 10*time.Second)
		if pid != oldPid {
			p.playerPid = pid
			break
		}
		if time.Now().After(deadline) {
			p.t.Fatal("player did not switch to the selected channel")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		child := waitPid(p.t, filepath.Join(p.dir, "child.pid"), 10*time.Second)
		if alive(child) && child != p.childPid {
			p.childPid = child
			break
		}
		if time.Now().After(deadline) {
			p.t.Fatal("player descendant did not restart for the selected channel")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if alive(oldPid) {
		p.t.Errorf("previous player %d still alive after switching channel", oldPid)
	}
}

// Ctrl+C reaches a raw-mode TUI as byte 0x03, not SIGINT. In every mode it
// must stay running, show the "To quit use Ctrl+Q" hint, and leave the player
// group alive. Ctrl+Q afterwards still exits cleanly with code 0.
func TestCtrlCByteDoesNotQuitFromEveryMode(t *testing.T) {
	const hint = "To quit use Ctrl+Q"
	modes := []struct {
		name  string
		enter func(p *ptyTUI)
	}{
		{"list", func(p *ptyTUI) {
			// 'q' is not a quit key outside the compose input.
			p.send([]byte("q"))
			time.Sleep(300 * time.Millisecond)
			select {
			case <-p.done:
				p.t.Fatalf("'q' in the list exited the TUI (code %d)", p.cmd.ProcessState.ExitCode())
			default:
			}
		}},
		{"filter", func(p *ptyTUI) {
			p.sendAndWait([]byte("/"), "Filter:")
			p.sendAndWait([]byte("pty"), "Filter: pty")
		}},
		{"search", func(p *ptyTUI) {
			p.sendAndWait([]byte{0x06}, "Search:") // Ctrl+F
			p.sendAndWait([]byte("ptyc"), "Search: ptyc")
		}},
		{"chat", func(p *ptyTUI) {
			p.openChannel()
			p.sendAndWait([]byte("\t"), "Chat — #ptychan [focus]")
		}},
		{"compose", func(p *ptyTUI) {
			p.openChannel()
			// 'q' typed while composing is text, not a quit key.
			p.sendAndWait([]byte("i"), "Send to #ptychan: █")
			p.sendAndWait([]byte("quit? q"), "Send to #ptychan: quit? q█")
		}},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			p := startTUIOnPTY(t, "TTV_APP_RICH=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")
			mode.enter(p)
			select {
			case <-p.done:
				t.Fatalf("TUI exited before Ctrl+C (code %d)", p.cmd.ProcessState.ExitCode())
			default:
			}
			if !alive(p.playerPid) || !alive(p.childPid) {
				t.Fatal("player not running before Ctrl+C")
			}
			p.sendAndWait([]byte{0x03}, hint)
			select {
			case <-p.done:
				t.Fatalf("Ctrl+C exited the TUI (code %d)", p.cmd.ProcessState.ExitCode())
			default:
			}
			if !alive(p.playerPid) || !alive(p.childPid) {
				t.Fatal("player died after Ctrl+C")
			}
			p.send([]byte{0x11}) // Ctrl+Q
			code := p.waitExit(10 * time.Second)
			if code != 0 {
				t.Errorf("Ctrl+Q exit code = %d, want 0", code)
			}
			p.assertPlayerGone()
			if !t.Failed() {
				t.Logf("%s: 0x03 → hint %q, stayed up; Ctrl+Q → exit %d; player %d and descendant %d gone",
					mode.name, hint, code, p.playerPid, p.childPid)
			}
		})
	}
}
