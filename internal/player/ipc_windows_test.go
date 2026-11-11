package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestPipePathAddsMPVPrefix(t *testing.T) {
	if got, want := pipePath("ttv-tui-1-2"), `\\.\pipe\ttv-tui-1-2`; got != want {
		t.Fatalf("pipePath = %q, want %q", got, want)
	}
}

// The values are the OS's, taken from syscall and x/sys/windows, and each goes
// through the wrappers the real call sites add.
func TestIsIPCPeerGoneWindowsErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"syscall ERROR_BROKEN_PIPE", syscall.ERROR_BROKEN_PIPE, true},
		{"ERROR_BROKEN_PIPE", windows.ERROR_BROKEN_PIPE, true},
		{"ERROR_NO_DATA", windows.ERROR_NO_DATA, true},
		{"ERROR_PIPE_NOT_CONNECTED", windows.ERROR_PIPE_NOT_CONNECTED, true},
		{"syscall WSAECONNRESET", syscall.WSAECONNRESET, true},
		{"WSAECONNRESET", windows.WSAECONNRESET, true},
		{"syscall WSAECONNABORTED", syscall.WSAECONNABORTED, true},
		{"WSAESHUTDOWN", windows.WSAESHUTDOWN, true},
		{"winio closed", winio.ErrFileClosed, true},
		{"ERROR_FILE_NOT_FOUND", windows.ERROR_FILE_NOT_FOUND, false},
		{"ERROR_PIPE_BUSY", windows.ERROR_PIPE_BUSY, false},
		{"ERROR_ACCESS_DENIED", windows.ERROR_ACCESS_DENIED, false},
		{"unrelated", errors.New("boom"), false},
	}
	for _, tc := range cases {
		wrapped := map[string]error{
			"bare":    tc.err,
			"path":    &os.PathError{Op: "write", Path: `\\.\pipe\x`, Err: tc.err},
			"syscall": os.NewSyscallError("WriteFile", tc.err),
			"fmt":     fmt.Errorf("player: ipc write: %w", tc.err),
		}
		for how, err := range wrapped {
			t.Run(tc.name+"/"+how, func(t *testing.T) {
				if got := isIPCPeerGone(err); got != tc.want {
					t.Fatalf("isIPCPeerGone(%v) = %v, want %v", err, got, tc.want)
				}
			})
		}
	}
}

// pipePair is a connected client and server over a real pipe, through the
// production dial.
func pipePair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	endpoint := testEndpoint(t, "pair")
	ln, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept: %v", err)
		}
		accepted <- conn
	}()
	client, err = dialIPCOnce(endpoint, 2*time.Second)
	if err != nil {
		t.Fatalf("dialIPCOnce: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server = <-accepted
	if server == nil {
		t.Fatal("no server side")
	}
	t.Cleanup(func() { _ = server.Close() })
	return client, server
}

// Same protocol as the Unix socket: one JSON document per line, both ways.
func TestPipeCarriesTheIPCProtocol(t *testing.T) {
	client, server := pipePair(t)

	if err := writeIPC(client, ipcWriteWait, map[string]any{"command": []any{"observe_property", 1, "pause"}}); err != nil {
		t.Fatalf("writeIPC: %v", err)
	}
	buf := make([]byte, 256)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("server read: %v", err)
	}
	if got, want := string(buf[:n]), `{"command":["observe_property",1,"pause"]}`+"\n"; got != want {
		t.Fatalf("server got %q, want %q", got, want)
	}

	if _, err := server.Write([]byte(`{"event":"playback-restart"}` + "\n")); err != nil {
		t.Fatalf("server write: %v", err)
	}
	msgs := make(chan ipcMessage, 1)
	done := make(chan error, 1)
	go func() { done <- readIPCMessages(client, func(m ipcMessage) { msgs <- m }) }()
	select {
	case msg := <-msgs:
		if !msg.indicatesPlaying() {
			t.Fatalf("message = %+v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message through the pipe")
	}

	_ = server.Close()
	select {
	case err := <-done:
		if !isIPCPeerGone(err) {
			t.Fatalf("readIPCMessages after peer close = %v, not peer gone", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reader did not end when the peer closed")
	}
}

// What the OS really answers once the far end closes, not a constant picked by
// hand: whatever it is, it has to count as the player being gone.
func TestRealClosedPipeErrorsArePeerGone(t *testing.T) {
	client, server := pipePair(t)
	_ = server.Close()

	_, readErr := client.Read(make([]byte, 16))
	if !errors.Is(readErr, io.EOF) {
		t.Fatalf("read after peer close = %v, want io.EOF", readErr)
	}
	if !isIPCPeerGone(readErr) {
		t.Fatalf("read error %v not peer gone", readErr)
	}

	_, writeErr := client.Write([]byte("{}\n"))
	if writeErr == nil {
		t.Fatal("write to a closed pipe succeeded")
	}
	t.Logf("write after peer close: %#v (%v)", writeErr, writeErr)
	if !isIPCPeerGone(writeErr) {
		t.Fatalf("write error %#v (%v) not peer gone", writeErr, writeErr)
	}
}

// closeIPC closes the conn under the reader goroutine.
func TestClosingOurEndIsPeerGone(t *testing.T) {
	client, _ := pipePair(t)
	done := make(chan error, 1)
	go func() {
		_, err := client.Read(make([]byte, 16))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	_ = client.Close()
	select {
	case err := <-done:
		if !isIPCPeerGone(err) {
			t.Fatalf("read after local close = %#v (%v), not peer gone", err, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read did not return after Close")
	}
}

// An absent pipe is the normal state while mpv starts: the real error must not
// read as "refused", which would start the 2s fast failure, nor as a dead peer.
func TestAbsentPipeIsNotRefused(t *testing.T) {
	endpoint := testEndpoint(t, "absent")
	conn, err := dialIPCOnce(endpoint, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("dialled a pipe nobody created")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("err = %#v (%v), want ERROR_FILE_NOT_FOUND", err, err)
	}
	if ipcEndpointRefused(endpoint, err) {
		t.Fatal("absent pipe read as refused")
	}
	if isIPCPeerGone(err) {
		t.Fatal("absent pipe read as peer gone")
	}
}

// Every instance taken is waiting too: winio retries it until the attempt's
// timeout, and the answer must not start the fast failure.
func TestBusyPipeIsWaitedFor(t *testing.T) {
	endpoint := testEndpoint(t, "busy")
	h, err := createPipeInstance(endpoint, windows.FILE_FLAG_FIRST_PIPE_INSTANCE, 1)
	if err != nil {
		t.Fatalf("createPipeInstance: %v", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	first, err := dialIPCOnce(endpoint, 2*time.Second)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer func() { _ = first.Close() }()

	conn, err := dialIPCOnce(endpoint, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		t.Fatal("second client connected to a one-instance pipe")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %#v (%v), want the attempt to run out its timeout", err, err)
	}
	if ipcEndpointRefused(endpoint, err) || isIPCPeerGone(err) {
		t.Fatalf("busy pipe misread: %v", err)
	}
}

func TestEndpointIsAShlexSafePipeName(t *testing.T) {
	dirs := []string{
		`C:\Users\Ana Maria\AppData\Local\ttv-tui\run\3-123456`,
		`C:\Users\ANAMAR~1\AppData\Local\Temp\ttv-tui-77-3`,
	}
	seen := map[string]bool{}
	for _, dir := range dirs {
		for range 2 {
			endpoint, err := endpointInDir(dir)
			if err != nil {
				t.Fatalf("endpointInDir: %v", err)
			}
			if seen[endpoint] {
				t.Fatalf("endpoint %q repeated", endpoint)
			}
			seen[endpoint] = true
			if strings.ContainsAny(endpoint, `/\ `) || strings.HasPrefix(endpoint, `\\.\pipe`) {
				t.Fatalf("endpoint %q is not a bare pipe name", endpoint)
			}
			arg := "--input-ipc-server=" + endpoint
			if got := quotePlayerArg(arg); got != arg {
				t.Fatalf("shlex would need quotes for %q: %q", arg, got)
			}
			if !strings.HasPrefix(endpoint, fmt.Sprintf("ttv-tui-%d-", os.Getpid())) {
				t.Fatalf("endpoint %q lacks the process id", endpoint)
			}
		}
	}
}

func TestPrepareSessionWindowsEndpointIsNotInDir(t *testing.T) {
	l, err := prepareSession(t.TempDir(), 3)
	if err != nil {
		t.Fatalf("prepareSession: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(l.Dir) })
	if strings.ContainsAny(l.Endpoint, `/\:`) {
		t.Fatalf("Endpoint = %q, want a bare pipe name", l.Endpoint)
	}
	if fi, err := os.Stat(l.Dir); err != nil || !fi.IsDir() {
		t.Fatalf("Dir = %v, %v", fi, err)
	}
}
