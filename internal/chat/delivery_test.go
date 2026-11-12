package chat

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func deliverySession(t *testing.T) (*Manager, net.Conn, <-chan string, *fakeClock) {
	t.Helper()
	srv := newFakeIRCServer(t)
	ready := make(chan net.Conn, 1)
	wire := make(chan string, 20)
	srv.handlers <- func(c net.Conn) {
		defer func() { _ = c.Close() }()
		scanner := bufio.NewScanner(c)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "JOIN ") {
				ready <- c
			}
			if strings.Contains(line, "PRIVMSG ") {
				wire <- line
			}
		}
	}
	clk := newFakeClock(time.Now())
	m := New(Options{Addr: srv.Addr(), Dial: srv.Dial, Token: "fixture", Username: "tester", Now: clk.Now, After: clk.After, CoalesceInterval: time.Millisecond, SendTimeout: 200 * time.Millisecond})
	t.Cleanup(m.Close)
	gen, err := m.JoinLogin("test")
	if err != nil {
		t.Fatal(err)
	}
	var c net.Conn
	select {
	case c = <-ready:
	case <-time.After(time.Second):
		t.Fatal("handshake timeout")
	}
	waitConnected(t, m, gen)
	return m, c, wire, clk
}

func deliverySend(t *testing.T, m *Manager, wire <-chan string, nonce string) {
	t.Helper()
	if _, err := m.Send(SendRequest{Nonce: nonce, Body: nonce}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wire:
	case <-time.After(time.Second):
		t.Fatal("send timeout")
	}
}

// A notice after the tested line flushes the batch and proves that line was handled.
func deliveryRound(t *testing.T, m *Manager, c net.Conn, line string) ([]SendStatusUpdate, []Message) {
	t.Helper()
	if _, err := io.WriteString(c, line+"\r\n:srv NOTICE #test :barrier\r\n"); err != nil {
		t.Fatal(err)
	}
	var updates []SendStatusUpdate
	var messages []Message
	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-m.Events():
			updates = append(updates, ev.SendUpdates...)
			messages = append(messages, ev.Messages...)
			for _, msg := range ev.Messages {
				if msg.Body == "barrier" {
					return updates, messages
				}
			}
		case <-deadline:
			t.Fatal("barrier timeout")
		}
	}
}

func TestDeliveryUnrelatedUserState(t *testing.T) {
	for _, line := range []string{":srv USERSTATE #test", "@client-nonce=old :srv USERSTATE #test", "@client-nonce=current :srv USERSTATE #other"} {
		t.Run(line, func(t *testing.T) {
			m, c, wire, _ := deliverySession(t)
			deliverySend(t, m, wire, "current")
			updates, _ := deliveryRound(t, m, c, line)
			if len(updates) != 0 {
				t.Fatalf("unrelated USERSTATE confirmed current: %+v", updates)
			}
			updates, _ = deliveryRound(t, m, c, "@client-nonce=current :srv USERSTATE #test")
			if len(updates) != 1 || updates[0].Status != DeliveryConfirmed {
				t.Fatalf("matching confirmation: %+v", updates)
			}
		})
	}
}

func TestDeliveryLateUserState(t *testing.T) {
	m, c, wire, clk := deliverySession(t)
	deliverySend(t, m, wire, "A")
	deliveryRound(t, m, c, "@client-nonce=A :srv USERSTATE #test")
	clk.Advance(time.Second)
	deliverySend(t, m, wire, "B")
	updates, _ := deliveryRound(t, m, c, "@client-nonce=A :srv USERSTATE #test")
	if len(updates) != 0 {
		t.Fatalf("late A confirmed B: %+v", updates)
	}
}

func TestDeliveryInformationalNotice(t *testing.T) {
	for _, tc := range []struct{ id, body string }{{"slow_on", "This room is now in slow mode."}, {"followers_on", "This room is now in followers-only mode."}} {
		t.Run(tc.id, func(t *testing.T) {
			m, c, wire, _ := deliverySession(t)
			deliverySend(t, m, wire, "A")
			updates, _ := deliveryRound(t, m, c, fmt.Sprintf("@msg-id=%s :srv NOTICE #test :%s", tc.id, tc.body))
			if len(updates) != 0 {
				t.Fatalf("informational notice rejected A: %+v", updates)
			}
			updates, _ = deliveryRound(t, m, c, "@client-nonce=A :srv USERSTATE #test")
			if len(updates) != 1 || updates[0].Status != DeliveryConfirmed {
				t.Fatalf("confirmation: %+v", updates)
			}
		})
	}
}

func TestDeliveryForeignNonce(t *testing.T) {
	m, c, wire, _ := deliverySession(t)
	deliverySend(t, m, wire, "A")
	updates, messages := deliveryRound(t, m, c, "@client-nonce=A :someone!someone@host PRIVMSG #test :hello")
	if len(updates) != 0 {
		t.Errorf("foreign message confirmed A: %+v", updates)
	}
	found := false
	for _, msg := range messages {
		if msg.User == "someone" && msg.Body == "hello" {
			found = true
		}
	}
	if !found {
		t.Error("foreign message disappeared")
	}
}

func TestDeliveryDeadline(t *testing.T) {
	m, c, wire, _ := deliverySession(t)
	deliverySend(t, m, wire, "silent")
	upd := waitSendUpdate(t, m, "silent")
	if upd.Reason != RejectTimeout {
		t.Fatalf("missing timeout: %+v", upd)
	}
	updates, _ := deliveryRound(t, m, c, "@client-nonce=silent :srv USERSTATE #test")
	if len(updates) != 0 {
		t.Fatalf("late confirmation after timeout: %+v", updates)
	}
}

func TestDeliveryNonceAcrossManagers(t *testing.T) {
	a, _, _, _ := deliverySession(t)
	b, _, _, _ := deliverySession(t)
	if a.NextNonce() == b.NextNonce() {
		t.Fatal("nonce repeats across sessions")
	}
}
