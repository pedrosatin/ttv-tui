//go:build unix

package player

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// bindOnly creates the socket file without listening, like mpv between bind and
// listen. It returns the fd; the caller listens on it later.
func bindOnly(t *testing.T, path string) int {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("Socket: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		t.Fatalf("Bind: %v", err)
	}
	return fd
}

// A socket file with nobody listening must fail in the short window, not run
// until the far deadline.
func TestDialIPCUntilFailsFastWithoutListener(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "dead")
	fd := bindOnly(t, endpoint)
	defer func() { _ = syscall.Close(fd) }() // never listens

	start := time.Now()
	conn, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(15*time.Second))
	elapsed := time.Since(start)
	if err == nil {
		_ = conn.Close()
		t.Fatal("connected to a socket with no listener")
	}
	if !errors.Is(err, errIPCNoListener) {
		t.Fatalf("err = %v, want errIPCNoListener", err)
	}
	if elapsed < ipcRefusedGrace {
		t.Fatalf("failed after %v, before the %v grace window", elapsed, ipcRefusedGrace)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("failed after %v: must not wait for the 15s deadline", elapsed)
	}
	t.Logf("fast failure measured: %v", elapsed)
}

// The file appears first and the listen call comes 200ms later: still a
// success, because that is inside the grace window.
func TestDialIPCUntilAbsorbsBindListenRace(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "race")
	fd := bindOnly(t, endpoint)
	defer func() { _ = syscall.Close(fd) }()

	time.AfterFunc(200*time.Millisecond, func() {
		if err := syscall.Listen(fd, 4); err != nil {
			t.Errorf("Listen: %v", err)
		}
	})
	start := time.Now()
	conn, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(15*time.Second))
	if err != nil {
		t.Fatalf("dialIPCUntil: %v", err)
	}
	_ = conn.Close()
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("connected after %v, want ~200ms", elapsed)
	}
}

// An endpoint that never appears keeps being waited for until the deadline,
// however short the refused grace is by comparison.
func TestDialIPCUntilKeepsWaitingForAbsentEndpoint(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "absent")
	wait := ipcRefusedGrace + 500*time.Millisecond
	start := time.Now()
	_, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(wait))
	elapsed := time.Since(start)
	if !errors.Is(err, errIPCWaitTimeout) {
		t.Fatalf("err = %v, want errIPCWaitTimeout", err)
	}
	if elapsed < wait-100*time.Millisecond {
		t.Fatalf("gave up after %v, before the %v deadline", elapsed, wait)
	}
}

func TestDialIPCUntilAbortsDuringRefusedGrace(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "dead")
	fd := bindOnly(t, endpoint)
	defer func() { _ = syscall.Close(fd) }()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := dialIPCUntil(ctx, nil, endpoint, time.Now().Add(15*time.Second))
	if !errors.Is(err, errIPCWaitAborted) {
		t.Fatalf("err = %v, want errIPCWaitAborted", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("abort took %v", elapsed)
	}
}

func TestIPCEndpointRefused(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	refused := &net.OpError{Op: "dial", Net: "unix", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	if !ipcEndpointRefused(present, refused) {
		t.Error("present + refused: want true")
	}
	if ipcEndpointRefused(filepath.Join(dir, "absent"), refused) {
		t.Error("absent + refused: want false")
	}
	if ipcEndpointRefused(present, errors.New("boom")) {
		t.Error("present + other error: want false")
	}
}

// Through the whole session: the failure is reported fast, with a clear
// message, and the process group (parent and descendant) is gone.
func TestSessionFailsFastWhenSocketHasNoListener(t *testing.T) {
	mgr := newTestManager(t, "socket-no-listener", Options{SocketWait: 15 * time.Second, StopWait: 300 * time.Millisecond})
	start := time.Now()
	gen, err := mgr.Start("chan")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.mu.Lock()
	pgid := mgr.session.Pid()
	mgr.mu.Unlock()

	ev := waitError(t, mgr, gen, 5*time.Second)
	elapsed := time.Since(start)
	if elapsed > 4*time.Second {
		t.Fatalf("error after %v: must not take the 15s SocketWait", elapsed)
	}
	if !strings.Contains(ev.LastLine, "control channel") || strings.Contains(strings.ToLower(ev.LastLine), "socket") {
		t.Fatalf("LastLine = %q, want a clear message about the control channel", ev.LastLine)
	}
	if ev.Failure != FailureUnknown {
		t.Fatalf("Failure = %q, want %q", ev.Failure, FailureUnknown)
	}
	// The error event is emitted before the group is terminated; wait for it.
	gone := time.Now().Add(3 * time.Second)
	for (processAlive(pgid) || recordedGroup(pgid).alive()) && time.Now().Before(gone) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(pgid) || recordedGroup(pgid).alive() {
		t.Fatalf("process group %d still has members after the failure", pgid)
	}
	t.Logf("session failure measured: %v", elapsed)
}
