//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestStreamPipeSecurity(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := l.Accept()
		if err == nil {
			<-accepted
			_ = c.Close()
		}
	}()
	defer func() { close(accepted); _ = l.Close(); <-done }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f := c.(interface{ Fd() uintptr })
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("DACL is not protected")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if acl == nil || acl.AceCount != 1 {
		t.Fatalf("expected one owner ACE, got %v", acl)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(user.User.Sid) || uint32(ace.Mask)&0x1f01ff != 0x1f01ff {
		t.Fatalf("unexpected ACE for %s, mask %#x", sid.String(), ace.Mask)
	}
	for _, kind := range []windows.WELL_KNOWN_SID_TYPE{windows.WinWorldSid, windows.WinAnonymousSid, windows.WinAuthenticatedUserSid} {
		forbidden, err := windows.CreateWellKnownSid(kind)
		if err != nil {
			t.Fatal(err)
		}
		if sid.Equals(forbidden) {
			t.Fatalf("broad SID %s", sid.String())
		}
	}
}

func TestStreamPipeRejectsOtherProcess(t *testing.T) {
	if name := os.Getenv("TTV_PIPE_TEST_CLIENT"); name != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := winio.DialPipeContext(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		b := make([]byte, 1)
		if n, err := c.Read(b); n != 0 || err == nil {
			t.Fatalf("rejected client read %d bytes, %v", n, err)
		}
		return
	}
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	timer := time.AfterFunc(10*time.Second, func() { _ = l.Close() })
	defer timer.Stop()
	result := make(chan error, 1)
	go func() {
		c, err := acceptStreamClient(l, uint32(os.Getpid()))
		if err == nil {
			_, err = c.Write([]byte("ok"))
			_ = c.Close()
		}
		result <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStreamPipeRejectsOtherProcess$")
	cmd.Env = append(os.Environ(), "TTV_PIPE_TEST_CLIENT="+name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("other process: %v\n%s", err, out)
	}
	c, err := winio.DialPipeContext(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b, err := io.ReadAll(c)
	if err != nil || string(b) != "ok" {
		t.Fatalf("accepted client: %q, %v", b, err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

// openStreamOnce uses the same single open as libmpv, without DialPipe retries.
func openStreamOnce(t *testing.T, name string) io.ReadWriteCloser {
	t.Helper()
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := winio.NewOpenFile(h)
	if err != nil {
		_ = windows.CloseHandle(h)
		t.Fatal(err)
	}
	timer := time.AfterFunc(10*time.Second, func() { _ = c.Close() })
	t.Cleanup(func() { timer.Stop() })
	return c
}

func TestStreamPipeAvailableBeforeAccept(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := openStreamOnce(t, name)
	defer c.Close()
	server, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// Even with the previous instance still connected, its successor is ready.
	next := openStreamOnce(t, name)
	defer next.Close()
}

func TestStreamPipeRefusalLimit(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	timer := time.AfterFunc(10*time.Second, func() { _ = l.Close() })
	defer timer.Stop()
	result := make(chan error, 1)
	go func() {
		// PID zero is invalid and must not consume any of the available instances.
		if _, err := acceptStreamClient(l, 0); err == nil {
			result <- fmt.Errorf("accepted PID zero")
			return
		}
		_, err := acceptStreamClient(l, ^uint32(0))
		result <- err
	}()
	for range maxStreamClientRefusals {
		c := openStreamOnce(t, name)
		b := make([]byte, 1)
		n, err := c.Read(b)
		_ = c.Close()
		if n != 0 || err == nil {
			t.Fatalf("refused client read %d bytes, %v", n, err)
		}
	}
	if err := <-result; err == nil || !strings.Contains(err.Error(), "limite de 16") {
		t.Fatalf("limit: %v", err)
	}
}

func TestStreamPipeCloseCancelsAccept(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := l.Accept(); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		accepting := l.accepting
		l.mu.Unlock()
		if accepting {
			break
		}
		if time.Now().After(deadline) {
			_ = l.Close()
			t.Fatal("Accept did not start")
		}
		time.Sleep(time.Millisecond)
	}
	_ = l.Close()
	select {
	case err := <-result:
		if err != net.ErrClosed {
			t.Fatalf("closed listener: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not stop")
	}
}

func TestShimStreamWindowPID(t *testing.T) {
	for _, pid := range []uint32{0, uint32(os.Getpid())} {
		t.Run(fmt.Sprint(pid), func(t *testing.T) {
			// Short AF_UNIX path also fits the Windows sockaddr_un limit.
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "c"), Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			window, err := listener.AcceptUnix()
			if err != nil {
				t.Fatal(err)
			}
			defer window.Close()
			deadline := time.Now().Add(5 * time.Second)
			if err := client.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			if err := window.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			stdin := os.Stdin
			empty, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			os.Stdin = empty
			defer func() { os.Stdin = stdin; _ = empty.Close() }()
			done := make(chan error, 1)
			go func() { done <- sendShimStream(client, "ttv-tui-test") }()
			defer func() { _ = window.Close(); _ = client.Close(); <-done }()
			if err := json.NewEncoder(window).Encode(map[string]uint32{"pid": pid}); err != nil {
				t.Fatal(err)
			}
			if pid == 0 {
				err := <-done
				done <- err
				if err == nil || !strings.Contains(err.Error(), "PID da janela ausente") {
					t.Fatalf("zero PID: %v", err)
				}
				return
			}
			var request struct {
				IPC  string
				Pipe string
			}
			if err := json.NewDecoder(window).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.IPC != "ttv-tui-test" {
				t.Fatalf("IPC: %q", request.IPC)
			}
			c := openStreamOnce(t, request.Pipe)
			defer c.Close()
			b, err := io.ReadAll(c)
			if err != nil || len(b) != 0 {
				t.Fatalf("stream: %q, %v", b, err)
			}
			err = <-done
			done <- err
			if err != nil {
				t.Fatalf("shim: %v", err)
			}
		})
	}
}

func TestStreamPipeDisconnectedClient(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\ttv-win-stream-%d-%d`, os.Getpid(), time.Now().UnixNano())
	l, err := listenStreamPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	// Disconnect before ConnectNamedPipe or PID inspection can run.
	c := openStreamOnce(t, name)
	_ = c.Close()
	timer := time.AfterFunc(10*time.Second, func() { _ = l.Close() })
	defer timer.Stop()
	result := make(chan error, 1)
	go func() {
		_, err := acceptStreamClient(l, ^uint32(0))
		result <- err
	}()
	for range maxStreamClientRefusals - 1 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c, err := winio.DialPipeContext(ctx, name)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 1)
		_, _ = c.Read(b)
		_ = c.Close()
	}
	if err := <-result; err == nil || !strings.Contains(err.Error(), "limite de 16") {
		t.Fatalf("disconnected client: %v", err)
	}
}
