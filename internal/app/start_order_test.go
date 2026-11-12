package app

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/player"
)

// spawnedLogins is a player Command that records each channel it spawns.
type spawnedLogins struct {
	mu     sync.Mutex
	logins []string
}

func (s *spawnedLogins) command(target player.Target, _ string) *exec.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logins = append(s.logins, target.Identity())
	return exec.Command(os.Args[0], "-test.run=^$")
}

func (s *spawnedLogins) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logins)
}

func newOrderTestPlayer(t *testing.T, spawns *spawnedLogins) *player.Manager {
	t.Helper()
	mgr := player.New(player.Options{
		RuntimeDir: t.TempDir(),
		StopWait:   100 * time.Millisecond,
		Command:    spawns.command,
	})
	t.Cleanup(mgr.Close)
	return mgr
}

// Enter on xqc and then on gaules: each Start runs in its own goroutine, and
// gaules's may reach the player first. The player must end on gaules.
func TestPlaybackStartsKeepSelectionOrder(t *testing.T) {
	spawns := &spawnedLogins{}
	b := newPlaybackBridge(newOrderTestPlayer(t, spawns))

	startX := b.Start("xqc", 1)
	startY := b.Start("gaules", 2)
	startY()
	startX()

	if got := spawns.list(); !slices.Equal(got, []string{"gaules"}) {
		t.Fatalf("spawned %v, want only gaules", got)
	}
}

// Ctrl+X and right after it Enter on another channel: the Stop's goroutine may
// run after the Start's. That Stop belongs to the playback before it and must
// not take the new one down.
func TestPlaybackStopLeavesLaterStartPlaying(t *testing.T) {
	spawns := &spawnedLogins{}
	mgr := newOrderTestPlayer(t, spawns)
	b := newPlaybackBridge(mgr)
	b.Start("xqc", 1)()

	stop := b.Stop()
	start := b.Start("gaules", 2)
	start()
	stop()

	if mgr.Volume() == 0 {
		t.Fatalf("no playback after the Stop that came before gaules; spawned %v", spawns.list())
	}
}

// Same as the player: the chat must end on the last channel chosen even when
// the Join for it runs first.
func TestChatJoinsKeepSelectionOrder(t *testing.T) {
	mgr := chat.New(chat.Options{
		Dial: func(context.Context, string) (net.Conn, error) {
			return nil, errors.New("offline test")
		},
	})
	t.Cleanup(mgr.Close)
	b := newChatBridge(mgr)

	joinX := b.Join("xqc")
	joinY := b.Join("gaules")
	joinY()
	joinX()

	if ch := mgr.Channel(); ch != "gaules" {
		t.Fatalf("chat on %q, want gaules", ch)
	}
}
