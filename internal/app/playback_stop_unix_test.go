//go:build unix

package app

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestPlaybackStopSurvivorBlocksStart(t *testing.T) {
	if socket := os.Getenv("TTV_STOP_TEST_SOCKET"); socket != "" {
		ln, err := net.Listen("unix", socket)
		if err != nil {
			os.Exit(2)
		}
		conn, err := ln.Accept()
		if err != nil {
			os.Exit(3)
		}
		_, _ = io.WriteString(conn, "{\"event\":\"playback-restart\"}\n")
		_, _ = io.Copy(io.Discard, conn)
		os.Exit(0)
	}
	var cmd *exec.Cmd
	spawns := 0
	mgr := player.New(player.Options{
		RuntimeDir: t.TempDir(),
		StopWait:   100 * time.Millisecond,
		Command: func(_, socket string) *exec.Cmd {
			spawns++
			cmd = exec.Command(os.Args[0], "-test.run=^TestPlaybackStopSurvivorBlocksStart$")
			cmd.Env = append(os.Environ(), "TTV_STOP_TEST_SOCKET="+socket)
			return cmd
		},
	})
	t.Cleanup(mgr.Close)
	b := newPlaybackBridge(mgr)
	started := b.Start("stuck", 1)().(ui.PlayerStatusMsg)
	timeout := time.After(5 * time.Second)
playing:
	for {
		select {
		case ev := <-mgr.Events():
			if ev.Generation == started.Generation && ev.State == player.StatePlaying {
				break playing
			}
		case <-timeout:
			t.Fatal("player did not start")
		}
	}
	pgid := cmd.Process.Pid
	zombie := exec.Command(os.Args[0], "-test.run=^$")
	zombie.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}
	if err := zombie.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = zombie.Wait() })
	want := fmt.Sprintf("player: process group %d survived SIGKILL", pgid)
	stopped := b.Stop()().(ui.PlayerStatusMsg)
	if stopped.Generation != started.Generation || stopped.State != ui.PlayerStateError || stopped.LastLine != want {
		t.Errorf("Stop = %+v, want erro with %q in generation %d", stopped, want, started.Generation)
	}
	for _, login := range []string{"stuck", "other"} {
		next := b.Start(login, 2)().(ui.PlayerStatusMsg)
		if next.State != ui.PlayerStateError || next.LastLine != want {
			t.Errorf("Start(%q) = %+v, want erro with %q", login, next, want)
		}
	}
	if spawns != 1 {
		t.Errorf("spawned %d processes, want 1", spawns)
	}
	_ = zombie.Wait()
	resumed := b.Start("other", 3)().(ui.PlayerStatusMsg)
	if resumed.State != ui.PlayerStateStarting || spawns != 2 {
		t.Errorf("Start after group reaped = %+v, spawns=%d", resumed, spawns)
	}
}
