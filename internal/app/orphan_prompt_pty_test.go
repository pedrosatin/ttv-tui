//go:build linux

package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestOrphanPromptNeutralizesLogin_PTY checks the escaping of a seeded legacy
// orphan label after a rejected URL paste. The URL of the audit is pasted into search: streamurl refuses its decoded
// ESC and BEL, so it runs as a search and the player keeps playing. The TUI is
// then SIGKILLed and its live-player record rewritten with that decoded login,
// as an older version wrote it. A second start of the real program on a fresh
// PTY must print the orphan prompt with the sequences escaped, so the terminal
// neither retitles the window nor clears the screen nor answers a query on the
// input the prompt reads.
func TestOrphanPromptNeutralizesLogin_PTY(t *testing.T) {
	const pasted = "https://attacker.example/%1b%5d0;T%07%1b%5b2J/live.m3u8"
	const decoded = "attacker.example:\x1b]0;T\x07\x1b[2J/live.m3u8"
	env := []string{"TTV_APP_RICH=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES="}
	p := startTUIOnPTY(t, env...)
	p.waitScreen(0, "PtyCategory")
	targetLog := filepath.Join(p.dir, "player_targets.log")
	targetsBefore, _ := os.ReadFile(targetLog)
	processesBefore := playerProcesses(p.dir)
	p.sendAndWait([]byte{0x06}, "Search:")
	p.sendAndWait([]byte(pasted+"\r"), "Search Results")
	// The search results are the observable proof that the paste was not taken
	// as a stream. A player would start asynchronously, so give a late one time
	// to show up before reading the history: the file player.pid alone would
	// keep the old value.
	time.Sleep(300 * time.Millisecond)
	if targetsAfter, _ := os.ReadFile(targetLog); !bytes.Equal(targetsAfter, targetsBefore) {
		t.Fatalf("the pasted URL reached the player; targets before %q, after %q", targetsBefore, targetsAfter)
	}
	if got := playerProcesses(p.dir); !slices.Equal(got, processesBefore) {
		t.Fatalf("player processes changed after the paste: before %v, after %v", processesBefore, got)
	}
	if recs := records(t, p.dir); len(recs) != 1 {
		t.Fatalf("player records after the paste = %v, want only the original", recs)
	}

	orphanPid := p.playerPid
	_ = p.cmd.Process.Signal(syscall.SIGKILL)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatal("TUI did not die on SIGKILL")
	}
	time.Sleep(200 * time.Millisecond)
	if !alive(orphanPid) {
		t.Fatal("player did not outlive the SIGKILLed TUI")
	}
	t.Cleanup(func() { _ = syscall.Kill(-orphanPid, syscall.SIGKILL) })

	setOrphanLogin(t, p.dir, decoded)

	// Second start on a fresh PTY, same state directory.
	master, slave := openPTY(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "TTV_APP_HELPER=tui", "TTV_APP_DIR="+p.dir, "TERM=xterm-256color", "XDG_STATE_HOME="+filepath.Join(p.dir, "xdg"))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start second tui: %v", err)
	}
	_ = slave.Close()
	var mu sync.Mutex
	var out bytes.Buffer
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				mu.Lock()
				out.Write(buf[:n])
				mu.Unlock()
				answerTerminalQueries(master, buf[:n])
			}
			if errors.Is(err, syscall.EAGAIN) {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = master.Close()
		for _, pid := range playerProcesses(p.dir) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); return out.String() }
	const question = "Stop it before continuing? [Y/n] "
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(snapshot(), question) {
		if time.Now().After(deadline) {
			t.Fatalf("orphan prompt never appeared; output %q", snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
	all := snapshot()
	start := strings.Index(all, "A player from a previous session")
	end := strings.Index(all, question)
	if start < 0 || end < start {
		t.Fatalf("orphan prompt not found whole in %q", all)
	}
	prompt := all[start : end+len(question)]
	assertNoControlBytes(t, "orphan prompt", prompt)
	if !strings.Contains(prompt, `attacker.example:\x1b]0;T\x07\x1b[2J/live.m3u8`) {
		t.Fatalf("orphan prompt does not show the pasted URL escaped: %q", prompt)
	}
	if strings.Contains(all, "\x1b]0;T") || strings.Contains(all, "\x1b[2J") || strings.Contains(all, "\a") {
		t.Fatalf("the pasted bytes reached the terminal before the answer: %q", all)
	}

	// Declined: the player stays, and the program goes on to the TUI.
	_, _ = master.Write([]byte("n\r"))
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(snapshot(), "Player kept running") {
		if time.Now().After(deadline) {
			t.Fatalf("decline not acknowledged; output %q", snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !alive(orphanPid) {
		t.Fatal("declined orphan was stopped")
	}
}
