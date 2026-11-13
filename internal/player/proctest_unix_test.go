//go:build unix

package player

import (
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// gracefulTerminate is true where terminate gives members until the StopWait
// deadline before killing them.
const gracefulTerminate = true

// pidExists is kill(pid, 0): a zombie still answers, unlike processAlive.
func pidExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// groupExists is kill(-pgid, 0).
func groupExists(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}

func groupOfPid(pid int) (int, error) {
	return syscall.Getpgid(pid)
}

// groupHoldsPid reports whether pid is in the process group of s.
func groupHoldsPid(t *testing.T, s *Session, pid int) bool {
	t.Helper()
	pgid, err := groupOfPid(pid)
	if err != nil {
		t.Fatalf("group of %d: %v", pid, err)
	}
	return pgid == s.Pid()
}

func killPid(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// helperSwapOnTerm simula um grupo que troca de dono entre o SIGTERM e o
// SIGKILL: ao receber SIGTERM, o processo se substitui por exec (mesmo PID, mesmo
// grupo) por outro sem o endpoint no argv. Antes de esperar o sinal, grava o PID
// em TTV_PLAYER_HELPER_READY.
func helperSwapOnTerm() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	if err := os.WriteFile(os.Getenv("TTV_PLAYER_HELPER_READY"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		os.Exit(2)
	}
	<-sig
	env := []string{"TTV_PLAYER_HELPER=1", "TTV_PLAYER_HELPER_MODE=idle-child"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TTV_PLAYER_HELPER") {
			env = append(env, kv)
		}
	}
	_ = syscall.Exec(os.Args[0], []string{os.Args[0], "--", "estranho"}, env)
	os.Exit(2)
}
