package chat

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// A reconnect keeps the channel, so what it discards must not be reported as a
// channel change: the message stays in the UI history with this reason.
func TestManager_ReconnectDiscardsPendingAsReconnected(t *testing.T) {
	srv := newFakeIRCServer(t)
	session := func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_, _ = readClientLines(conn, 4, 2*time.Second)
		_, _ = io.Copy(io.Discard, conn)
	}
	srv.handlers <- session
	srv.handlers <- session

	mgr := New(Options{
		Addr:             srv.Addr(),
		Dial:             srv.Dial,
		Token:            "oauth:tok",
		Username:         "pedrosatin",
		CoalesceInterval: 10 * time.Millisecond,
	})
	defer mgr.Close()

	gen1, err := mgr.JoinLogin("gaules")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	waitConnected(t, mgr, gen1)
	if _, err := mgr.Send(SendRequest{Generation: gen1, Channel: "gaules", Nonce: "a1", Body: "oi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	gen2, err := mgr.Reconnect()
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}

	deadline := time.After(2 * time.Second)
wait:
	for {
		select {
		case ev := <-mgr.Events():
			for _, upd := range ev.SendUpdates {
				if upd.Nonce != "a1" {
					continue
				}
				if upd.Status != DeliveryDiscarded || upd.Reason != RejectReconnected {
					t.Fatalf("update = %+v, want discarded/reconnected", upd)
				}
				break wait
			}
		case <-deadline:
			t.Fatal("no update for the pending message")
		}
	}

	// Composed before the reconnect, submitted after it.
	upd, err := mgr.Send(SendRequest{Generation: gen1, Channel: "gaules", Nonce: "a2", Body: "tarde"})
	if !errors.Is(err, ErrChannelSwitched) {
		t.Fatalf("late Send err = %v", err)
	}
	if upd.Generation != gen2 || upd.Reason != RejectReconnected {
		t.Fatalf("late Send upd = %+v, want reconnected on generation %d", upd, gen2)
	}
}
