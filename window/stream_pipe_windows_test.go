//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	go func() {
		c, err := l.Accept()
		if err == nil {
			<-accepted
			_ = c.Close()
		}
	}()
	defer close(accepted)
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
