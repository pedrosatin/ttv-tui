package chat

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Pause the event after its closed check, at the final send select.
type pausedEventContext struct {
	context.Context
	calls   int
	entered chan struct{}
	release chan struct{}
}

func (c *pausedEventContext) Done() <-chan struct{} {
	c.calls++
	if c.calls == 2 {
		close(c.entered)
		<-c.release
	}
	return nil
}

func TestStopCloseWithEventInTransit(t *testing.T) {
	mgr := New(Options{})
	ctx := &pausedEventContext{Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{})}
	stoppedCancel := make(chan struct{})
	closingCancel := make(chan struct{})
	releaseStop := make(chan struct{})
	var cancels atomic.Int32
	// Model a session suspended inside emit, with cancellation also paused.
	mgr.done = make(chan struct{})
	mgr.cancel = func() {
		if cancels.Add(1) == 1 {
			close(stoppedCancel)
			<-releaseStop
		} else {
			close(closingCancel)
		}
	}
	emitted := make(chan any, 1)
	go func() {
		defer close(mgr.done)
		defer func() { emitted <- recover() }()
		mgr.emit(ctx, SessionEvent{})
	}()
	<-ctx.entered
	stopped := make(chan struct{})
	go func() { mgr.Stop(); close(stopped) }()
	<-stoppedCancel
	closed := make(chan struct{})
	go func() { mgr.Close(); close(closed) }()
	select {
	case <-closingCancel:
	case <-closed:
	}
	close(ctx.release)
	panicValue := <-emitted
	close(releaseStop)
	<-stopped
	<-closed
	if panicValue != nil {
		t.Fatalf("event in transit panicked: %v", panicValue)
	}
	for range mgr.Events() {
	}
	mgr.Close()
}

func TestSendRefusalDuringClose(t *testing.T) {
	srv := newFakeIRCServer(t)
	srv.handlers <- func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
	}
	mgr := New(Options{Token: "fixture", Username: "tester", Dial: srv.Dial, Addr: srv.Addr()})
	t.Cleanup(mgr.Close)
	gen, err := mgr.Join("test")
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, mgr, gen)
	if _, err := mgr.Send(SendRequest{Generation: gen + 1, Channel: "test", Body: "refused"}); !errors.Is(err, ErrChannelSwitched) {
		t.Fatalf("send before close: %v", err)
	}
	closeEntered := make(chan struct{})
	releaseClose := make(chan struct{})
	mgr.mu.Lock()
	cancel := mgr.cancel
	mgr.cancel = func() {
		close(closeEntered)
		<-releaseClose
		cancel()
	}
	mgr.mu.Unlock()
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		<-start
		for range 100 {
			_, err := mgr.Send(SendRequest{Generation: gen + 1, Channel: "test", Body: "refused"})
			if !errors.Is(err, ErrChannelSwitched) && !errors.Is(err, ErrSessionClosed) {
				t.Errorf("send refusal: %v", err)
			}
		}
	}()
	go func() { defer workers.Done(); <-start; mgr.Close() }()
	go func() { defer workers.Done(); <-start; mgr.Close() }()
	close(start)
	<-closeEntered
	if _, err := mgr.Send(SendRequest{Body: "during close"}); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("send during close: %v", err)
	}
	close(releaseClose)
	workers.Wait()
	if _, err := mgr.Send(SendRequest{Body: "after close"}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("send after close: %v", err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-mgr.Events():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("events did not close")
		}
	}
}
