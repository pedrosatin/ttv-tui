package chat

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStrictHandshakeCredentials(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, opts := range []Options{
		{Token: "leakmark\r\nPRIVMSG", Username: "user"},
		{Token: "abc\n", Username: "user"},
		{Token: "abc", Username: "user\n"},
		{Token: "abc", Username: "K"}, // Símbolo Kelvin (U+212A), fora do ASCII.
		{Token: "leakmark def", Username: "user"},
		{RandNick: func() string { return "guest\r\n" }},
	} {
		m := New(opts)
		var wire bytes.Buffer
		_, err := m.writeHandshake(&wire, "channel", opts.Token == "")
		m.Close()
		if err == nil {
			t.Error("handshake accepted invalid credentials")
		}
		if wire.Len() != 0 {
			t.Error("handshake wrote invalid credentials")
		}
		if err != nil && opts.Token != "" && strings.Contains(err.Error(), "leakmark") {
			t.Error("handshake exposed credentials")
		}
	}
}

func TestStrictHandshakeNormalization(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, token := range []string{"OAuth:abc", "oauth:oauth:abc"} {
		m := New(Options{Token: token, Username: "User_1"})
		var wire bytes.Buffer
		_, err := m.writeHandshake(&wire, "channel", false)
		m.Close()
		if err != nil {
			t.Fatal("normalized credentials rejected")
		}
		if !strings.Contains(wire.String(), "PASS oauth:abc\r\n") {
			t.Error("wrong PASS normalization")
		}
	}
}

func TestInvalidCredentialsStopBeforeDial(t *testing.T) {
	for _, opts := range []Options{{Token: "leakmark\r\nPRIVMSG", Username: "user"}, {Token: "abc", Username: "bad user"}} {
		var dials atomic.Int32
		opts.Dial = func(context.Context, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("unexpected dial")
		}
		m := New(opts)
		if _, err := m.JoinLogin("channel"); err != nil {
			t.Fatal(err)
		}
		select {
		case ev := <-m.Events():
			if ev.State != StateError || ev.Err == nil || strings.Contains(ev.Err.Error(), "leakmark") {
				t.Fatal("expected one safe credential error")
			}
		case <-time.After(time.Second):
			t.Fatal("no credential error")
		}
		m.mu.Lock()
		done := m.done
		m.mu.Unlock()
		<-done
		if len(m.events) != 0 || dials.Load() != 0 {
			t.Error("invalid credentials retried or dialed")
		}
		m.Close()
	}
}
