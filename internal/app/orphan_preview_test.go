//go:build unix

package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Run cleans both the main registry and the nested preview registry during
// startup. FindOrphans skips directories, so a leftover under players/preview
// would otherwise survive SIGKILL of a previous TUI. This goes through app.Run
// on a PTY (the real init path); calling handleOrphans by hand would pass even
// when Run only scanned the main registry.
func TestHandleOrphansCleansPreviewRegistry(t *testing.T) {
	dir := t.TempDir()
	regDir := playerRegistryDir(filepath.Join(dir, "state", "state.json"))
	previewDir := filepath.Join(regDir, "preview")
	if err := os.MkdirAll(previewDir, 0o700); err != nil {
		t.Fatal(err)
	}

	dead := shortLivedPID(t)
	rec := map[string]any{
		"owner_pid":   dead,
		"owner_start": 1,
		"pgid":        dead,
		"socket":      "/nonexistent/preview-orphan/i",
		"generation":  1,
		"login":       "prevorphan",
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(previewDir, "player-1-1.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	master, slave := openPTY(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		"TTV_APP_HELPER=tui",
		"TTV_APP_DIR="+dir,
		"TTV_APP_PREVIEW=1",
		"TTV_APP_RICH=1",
		"TERM=xterm-256color",
		"XDG_STATE_HOME="+filepath.Join(dir, "xdg"),
		"LANG=en_US.UTF-8",
		"LC_ALL=",
		"LC_MESSAGES=",
	)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start tui: %v", err)
	}
	_ = slave.Close()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	var mu sync.Mutex
	var out bytes.Buffer
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
	})

	// Alternate-screen enter is written by tea.Program.Run after handleOrphans.
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		rendered := bytes.Contains(out.Bytes(), []byte("\x1b[?1049h"))
		snapshot := out.String()
		mu.Unlock()
		if rendered {
			break
		}
		select {
		case <-done:
			t.Fatalf("TUI exited before render; output %q", snapshot)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("TUI did not render after startup orphan cleanup; output %q", snapshot)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("preview record still present after Run: %v", err)
	}
}

func shortLivedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if cmd.Process == nil {
		t.Fatal("no process")
	}
	return cmd.Process.Pid
}
