package chat

import (
	"context"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func authTestManager(t *testing.T, reply string, username string) (*Manager, *atomic.Int32) {
	t.Helper()
	srv := newFakeIRCServer(t)
	var attempts atomic.Int32
	for range 8 {
		srv.handlers <- func(conn net.Conn) {
			defer func() { _ = conn.Close() }()
			lines, err := readClientLines(conn, 4, time.Second)
			if err != nil {
				return
			}
			if !strings.HasPrefix(lines[1], "PASS oauth:") || lines[2] != "NICK tester\r\n" {
				t.Error("unexpected authenticated handshake")
			}
			attempts.Add(1)
			_, _ = io.WriteString(conn, reply)
		}
	}
	mgr := New(Options{Token: "fixture", Username: username, Dial: srv.Dial,
		Addr: srv.Addr(), InitialBackoff: time.Millisecond, MaxBackoff: 8 * time.Millisecond})
	t.Cleanup(mgr.Close)
	if _, err := mgr.Join("test"); err != nil {
		t.Fatal(err)
	}
	return mgr, &attempts
}

func authEvent(t *testing.T, mgr *Manager) SessionEvent {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.State == StateReconnecting || ev.State == StateError {
				return ev
			}
		case <-deadline:
			t.Fatal("no reconnect or terminal error")
		}
	}
}

func TestAuthRefusalStopsAtThirdAttempt(t *testing.T) {
	mgr, attempts := authTestManager(t, ":srv NOTICE * :Login authentication failed\r\n", "tester")
	for i := 1; i <= 3; i++ {
		ev := authEvent(t, mgr)
		want := StateReconnecting
		if i == 3 {
			want = StateError
		}
		if ev.State != want || ev.Err == nil || ev.Anonymous {
			t.Fatalf("attempt %d state=%s err=%v anonymous=%v", i, ev.State, ev.Err, ev.Anonymous)
		}
	}
	mgr.mu.Lock()
	done := mgr.done
	mgr.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not end")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts=%d, want 3", got)
	}
	select {
	case ev := <-mgr.Events():
		t.Fatalf("event after terminal failure: %s", ev.State)
	default:
	}
}

func TestAuthBackoffOnlyResetsOnConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		reset       bool
	}{
		{"arbitrary", ":srv NOTICE #test :hello\r\n", false},
		{"network", "", false},
		{"other_join", ":other!other@host JOIN #test\r\n", false},
		{"welcome", ":srv 001 tester :Welcome\r\n", true},
		{"own_join", ":tester!tester@host JOIN #test\r\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, _ := authTestManager(t, tc.reply, "tester")
			for i := 0; i < 4; i++ {
				ev := authEvent(t, mgr)
				want := time.Millisecond
				if !tc.reset {
					want *= time.Duration(1 << i)
				}
				if ev.State != StateReconnecting || ev.NextBackoff != want {
					t.Fatalf("attempt %d state=%s backoff=%s want=%s", i+1, ev.State, ev.NextBackoff, want)
				}
			}
		})
	}
}

func TestAuthTokenWithoutUsernameNeverDials(t *testing.T) {
	var dials atomic.Int32
	mgr := New(Options{Token: "fixture", InitialBackoff: time.Millisecond, Dial: func(context.Context, string) (net.Conn, error) {
		dials.Add(1)
		return nil, io.EOF
	}})
	defer mgr.Close()
	if _, err := mgr.Join("test"); err != nil {
		t.Fatal(err)
	}
	ev := authEvent(t, mgr)
	if ev.State != StateError || ev.Err == nil || dials.Load() != 0 {
		t.Fatalf("state=%s err=%v dials=%d", ev.State, ev.Err, dials.Load())
	}
}
