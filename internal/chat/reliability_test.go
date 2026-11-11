package chat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func reliabilityOptions(srv *fakeIRCServer) Options {
	opts := Options{Addr: srv.Addr(), Dial: srv.Dial, InitialBackoff: time.Second, CoalesceInterval: time.Millisecond}
	opts.PingInterval = 20 * time.Millisecond
	opts.PongTimeout = 150 * time.Millisecond
	opts.SendTimeout = 100 * time.Millisecond
	return opts
}

func TestReliabilitySilentServer(t *testing.T) {
	srv := newFakeIRCServer(t)
	srv.handlers <- func(conn net.Conn) { defer func() { _ = conn.Close() }(); _, _ = io.Copy(io.Discard, conn) }
	mgr := New(reliabilityOptions(srv))
	defer mgr.Close()
	gen, err := mgr.Join("test")
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, mgr, gen)
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.State == StateReconnecting {
				if ev.Err == nil {
					t.Fatal("missing reason")
				}
				return
			}
		case <-deadline:
			t.Fatal("silent server still connected after deadline")
		}
	}
}

func TestReliabilityHealthyServer(t *testing.T) {
	for _, traffic := range []bool{false, true} {
		t.Run(fmt.Sprint(traffic), func(t *testing.T) {
			srv := newFakeIRCServer(t)
			pings := make(chan struct{}, 100)
			srv.handlers <- func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					if strings.HasPrefix(scanner.Text(), "PING ") {
						line := "PONG :ttv-tui\r\n"
						if traffic {
							line = ":srv NOTICE #test :alive\r\n"
						}
						if _, err := io.WriteString(conn, line); err != nil {
							return
						}
						pings <- struct{}{}
					}
				}
			}
			mgr := New(reliabilityOptions(srv))
			defer mgr.Close()
			gen, err := mgr.Join("test")
			if err != nil {
				t.Fatal(err)
			}
			waitConnected(t, mgr, gen)
			deadline := time.After(700 * time.Millisecond)
			count := 0
			for {
				select {
				case <-pings:
					count++
				case ev := <-mgr.Events():
					if ev.State == StateReconnecting {
						t.Fatalf("false disconnect: %v", ev.Err)
					}
				case <-deadline:
					if count < 20 {
						t.Fatalf("only %d pings", count)
					}
					return
				}
			}
		})
	}
}

func TestReliabilitySendWithoutEcho(t *testing.T) {
	srv := newFakeIRCServer(t)
	srv.handlers <- func(conn net.Conn) { defer func() { _ = conn.Close() }(); _, _ = io.Copy(io.Discard, conn) }
	opts := reliabilityOptions(srv)
	opts.Token = "test"
	opts.Username = "tester"
	mgr := New(opts)
	defer mgr.Close()
	gen, err := mgr.Join("test")
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, mgr, gen)
	for _, nonce := range []string{"one", "two"} {
		if _, err := mgr.Send(SendRequest{Generation: gen, Channel: "test", Nonce: nonce, Body: nonce}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(140 * time.Millisecond)
	rejected := map[string]bool{}
	for len(rejected) < 2 {
		select {
		case ev := <-mgr.Events():
			for _, u := range ev.SendUpdates {
				if u.Status == DeliveryDiscarded {
					rejected[u.Nonce] = true
				}
			}
		case <-deadline:
			t.Fatalf("messages still pending: %v", rejected)
		}
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.pending)+len(mgr.inFlight) != 0 {
		t.Fatal("queue did not clear")
	}
}

func TestReliabilityHealthySimulatedDay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		before := runtime.NumGoroutine()
		var pings atomic.Int64
		mgr := New(Options{Dial: func(context.Context, string) (net.Conn, error) {
			client, server := net.Pipe()
			go func() {
				defer func() { _ = server.Close() }()
				scanner := bufio.NewScanner(server)
				for scanner.Scan() {
					if strings.HasPrefix(scanner.Text(), "PING ") {
						pings.Add(1)
						if _, err := io.WriteString(server, "PONG :ttv-tui\r\n"); err != nil {
							return
						}
					}
				}
			}()
			return client, nil
		}})
		gen, err := mgr.Join("test")
		if err != nil {
			t.Fatal(err)
		}
		waitConnected(t, mgr, gen)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for ev := range mgr.Events() {
				if ev.State == StateReconnecting {
					t.Errorf("false disconnect: %v", ev.Err)
				}
			}
		}()
		time.Sleep(24 * time.Hour)
		mgr.Close()
		<-done
		synctest.Wait()
		if pings.Load() < 5700 {
			t.Fatalf("only %d heartbeats in 24h", pings.Load())
		}
		if after := runtime.NumGoroutine(); after > before {
			t.Fatalf("goroutines: before %d, after %d", before, after)
		}
	})
}

func TestReliabilityBlockedWriteAndNoReplay(t *testing.T) {
	before := runtime.NumGoroutine()
	synctest.Test(t, func(t *testing.T) {
		var dials atomic.Int64
		release := make(chan struct{})
		mgr := New(Options{Token: "test", Username: "tester", PingInterval: time.Second, PongTimeout: time.Second, SendTimeout: time.Second,
			InitialBackoff: time.Second, Dial: func(context.Context, string) (net.Conn, error) {
				client, server := net.Pipe()
				attempt := dials.Add(1)
				go func() {
					defer func() { _ = server.Close() }()
					if _, err := readClientLines(server, 4, time.Second); err != nil {
						return
					}
					if attempt == 1 {
						<-release
						return
					}
					scanner := bufio.NewScanner(server)
					for scanner.Scan() {
						line := scanner.Text()
						if strings.Contains(line, "PRIVMSG") {
							t.Errorf("replayed message: %s", line)
						}
						if strings.HasPrefix(line, "PING ") {
							_, _ = io.WriteString(server, "PONG :ttv-tui\r\n")
						}
					}
				}()
				return client, nil
			}})
		gen, err := mgr.Join("test")
		if err != nil {
			t.Fatal(err)
		}
		waitConnected(t, mgr, gen)
		if _, err := mgr.Send(SendRequest{Nonce: "stuck", Body: "do not replay"}); err != nil {
			t.Fatal(err)
		}
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev := <-mgr.Events():
				if ev.State != StateReconnecting {
					continue
				}
				if ev.Err == nil || len(ev.SendUpdates) != 1 || ev.SendUpdates[0].Status != DeliveryDiscarded {
					t.Fatalf("bad failure event: %+v", ev)
				}
				upd, err := mgr.Send(SendRequest{Nonce: "offline", Body: "offline"})
				if err != ErrDisconnected || upd.Status != DeliveryDiscarded {
					t.Fatalf("offline send queued: %+v %v", upd, err)
				}
				goto reconnect
			case <-deadline:
				t.Fatal("write stayed blocked")
			}
		}
	reconnect:
		waitConnected(t, mgr, gen)
		time.Sleep(3 * time.Second)
		mgr.Close()
		close(release)
		synctest.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		after := runtime.NumGoroutine()
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Fatalf("goroutines before %d after %d; all stacks:\n%s", before, after, buf[:n])
		}
		time.Sleep(25 * time.Millisecond)
	}
}
