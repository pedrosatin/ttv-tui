package player

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestIsIPCPeerGone(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: true},
		{name: "EOF", err: io.EOF, want: true},
		{name: "net.ErrClosed", err: net.ErrClosed, want: true},
		{name: "ECONNRESET", err: syscall.ECONNRESET, want: true},
		{name: "EPIPE", err: syscall.EPIPE, want: true},
		{name: "ECONNABORTED", err: syscall.ECONNABORTED, want: true},
		{
			name: "wrapped OpError SyscallError ECONNRESET",
			err: &net.OpError{
				Op:  "read",
				Net: "unix",
				Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
			},
			want: true,
		},
		{name: "unrelated", err: errors.New("boom"), want: false},
		{name: "EACCES", err: syscall.EACCES, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIPCPeerGone(tc.err); got != tc.want {
				t.Fatalf("isIPCPeerGone(%v)=%v want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The endpoint does not exist when the wait starts: readiness is the dial
// succeeding, not a file showing up first.
func TestDialIPCUntilWaitsForLateListener(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "late")
	if _, err := os.Lstat(endpoint); !os.IsNotExist(err) {
		t.Fatalf("endpoint exists before the wait: %v", err)
	}
	lnCh := make(chan net.Listener, 1)
	time.AfterFunc(200*time.Millisecond, func() {
		ln, err := net.Listen("unix", endpoint)
		if err != nil {
			t.Errorf("Listen: %v", err)
			lnCh <- nil
			return
		}
		lnCh <- ln
	})

	start := time.Now()
	conn, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatalf("dialIPCUntil: %v", err)
	}
	_ = conn.Close()
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("connected after %v, before the listener existed", elapsed)
	}
	if ln := <-lnCh; ln != nil {
		_ = ln.Close()
	}
}

func TestDialIPCUntilTimesOut(t *testing.T) {
	dir := t.TempDir()
	// A leftover regular file at the endpoint is not readiness.
	stale := filepath.Join(dir, "stale")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for name, endpoint := range map[string]string{
		"missing": filepath.Join(dir, "missing"),
		"stale":   stale,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			conn, err := dialIPCUntil(context.Background(), nil, endpoint, time.Now().Add(200*time.Millisecond))
			if err == nil {
				_ = conn.Close()
				t.Fatal("dialIPCUntil connected to nothing")
			}
			if !errors.Is(err, errIPCWaitTimeout) {
				t.Fatalf("err = %v, want errIPCWaitTimeout", err)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("took %v for a 200ms deadline", elapsed)
			}
		})
	}
}

func TestDialIPCUntilAborts(t *testing.T) {
	endpoint := filepath.Join(t.TempDir(), "never")
	far := time.Now().Add(time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := dialIPCUntil(ctx, nil, endpoint, far); !errors.Is(err, errIPCWaitAborted) {
		t.Fatalf("cancelled ctx: err = %v, want errIPCWaitAborted", err)
	}

	gone := make(chan struct{})
	time.AfterFunc(50*time.Millisecond, func() { close(gone) })
	if _, err := dialIPCUntil(context.Background(), gone, endpoint, far); !errors.Is(err, errIPCWaitAborted) {
		t.Fatalf("closed gone: err = %v, want errIPCWaitAborted", err)
	}
}
