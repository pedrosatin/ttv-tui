package player

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// processAlive treats zombies as dead, like the Linux helper: they no longer run
// or play audio.
func processAlive(pid int) bool {
	start, err := osProcStartTime(pid)
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	return start != 0
}

func TestOSProcStartTimeOfRealProcesses(t *testing.T) {
	self, err := osProcStartTime(os.Getpid())
	if err != nil || self == 0 {
		t.Fatalf("osProcStartTime(self) = %d, %v", self, err)
	}
	if self != selfStart {
		t.Fatalf("osProcStartTime(self) = %d, selfStart = %d", self, selfStart)
	}
	if st, err := osProcStartTime(deadPid(t)); st != 0 || err != nil {
		t.Fatalf("osProcStartTime(dead) = %d, %v, want 0 with no error", st, err)
	}
}

func TestOSGroupHasArgReadsArgvOfRealGroup(t *testing.T) {
	socket := "/nonexistent/argv/i"
	pgid := startIdleGroup(t, socket).Process.Pid

	if found, err := osGroupHasArg(pgid, socket); !found || err != nil {
		t.Fatalf("osGroupHasArg(argv) = %v, %v, want true", found, err)
	}
	if found, err := osGroupHasArg(pgid, "/nonexistent/other/i"); found || err != nil {
		t.Fatalf("osGroupHasArg(other) = %v, %v, want false", found, err)
	}
}

func TestOSGroupHasArgIgnoresEnvironment(t *testing.T) {
	const needle = "/nonexistent/env-only/i"
	cmd := exec.Command(os.Args[0], "--", "idle", "no-socket")
	cmd.Env = append(os.Environ(), "TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child", "LEAK="+needle)
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = startedGroup(cmd).kill()
		_ = cmd.Wait()
	})

	found, err := osGroupHasArg(cmd.Process.Pid, needle)
	if err != nil {
		t.Fatalf("osGroupHasArg: %v", err)
	}
	if found {
		t.Fatal("environment taken for argv")
	}
}
