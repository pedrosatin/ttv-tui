package player

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// emitRace records what a bare Session emits. The first event in state at is
// the step between the state change and its delivery: other runs there, as a
// competing goroutine would. If s.mu is free at that step, other finishes
// before the held event is delivered; if it is held, other has to wait for it.
type emitRace struct {
	mu     sync.Mutex
	states []State
	done   chan struct{}
}

func holdEmitAt(s *Session, at State, other func()) *emitRace {
	r := &emitRace{done: make(chan struct{})}
	var fired atomic.Bool
	s.cfg.Emit = func(ev Event) {
		if ev.State == at && fired.CompareAndSwap(false, true) {
			if s.mu.TryLock() {
				s.mu.Unlock()
				other()
				close(r.done)
			} else {
				go func() {
					other()
					close(r.done)
				}()
			}
		}
		r.mu.Lock()
		r.states = append(r.states, ev.State)
		r.mu.Unlock()
	}
	return r
}

func (r *emitRace) got() string {
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprint(r.states)
}

// mpv confirming playback and streamlink exiting race; the UI must not get
// "playing" after "ended".
func TestEventsLeaveInStateOrderPlayingThenEnded(t *testing.T) {
	s := &Session{cfg: sessionConfig{Generation: 3, Login: "x"}, state: StateStarting}
	r := holdEmitAt(s, StatePlaying, func() {
		s.transition(StateEnded, EndReasonStreamlinkExit, "")
	})
	s.handleIPCMessage(ipcMessage{Event: "playback-restart"})
	want := fmt.Sprint([]State{StatePlaying, StateEnded})
	if got := r.got(); got != want {
		t.Fatalf("states=%s want %s", got, want)
	}
}

// The interval counter tick and the hold-end line race; the UI must not get
// "interval" after the interval ended.
func TestEventsLeaveInStateOrderBreakTickThenEnd(t *testing.T) {
	s := &Session{cfg: sessionConfig{Generation: 3, Login: "x", Emit: func(Event) {}}, state: StatePlaying}
	s.startAdHold()
	r := holdEmitAt(s, StateBreak, s.endAdHold)
	s.emitStatus()
	want := fmt.Sprint([]State{StateBreak, StatePlaying})
	if got := r.got(); got != want {
		t.Fatalf("states=%s want %s", got, want)
	}
}

// feedLines runs lines through a playing Session and returns the states it
// emitted and the notes it recorded.
func feedLines(lines []string) (states []State, notes []string) {
	s := &Session{
		cfg: sessionConfig{
			Generation: 1,
			Login:      "x",
			Emit:       func(ev Event) { states = append(states, ev.State) },
			Note:       func(n string) { notes = append(notes, n) },
		},
		state: StatePlaying,
	}
	for _, l := range lines {
		s.handleLine(l)
	}
	return states, notes
}

// The fixture holds the lines streamlink 8.6.2 prints at --loglevel info around
// an ad break: plugins/twitch.py and stream/hls/hls.py, in the "[name][level]
// message" format of streamlink/logger.py.
func TestStreamlink8AdHoldLinesFromFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/streamlink-8-ad-hold.log")
	if err != nil {
		t.Fatal(err)
	}
	states, notes := feedLines(strings.Split(strings.TrimSpace(string(raw)), "\n"))
	if want := fmt.Sprint([]State{StateBreak, StatePlaying}); fmt.Sprint(states) != want {
		t.Fatalf("states=%v want %s", states, want)
	}
	if len(notes) != 0 {
		t.Fatalf("known lines recorded as unknown: %q", notes)
	}
}

// A reworded end line must not keep the interval until BreakMaxHold: it ends the
// hold, since its end can no longer be verified, and is recorded.
func TestUnknownHoldEndLineIsNotedAndEndsHold(t *testing.T) {
	unknown := "[stream.hls][info] Stream output resumed"
	states, notes := feedLines([]string{
		"[stream.hls][info] Filtering out segments and pausing stream output",
		unknown,
	})
	if want := fmt.Sprint([]State{StateBreak, StatePlaying}); fmt.Sprint(states) != want {
		t.Fatalf("states=%v want %s", states, want)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], unknown) {
		t.Fatalf("notes=%q want one with %q", notes, unknown)
	}
}

// A reworded start line never claims an interval, and is recorded.
func TestUnknownHoldStartLineIsNotedWithoutBreak(t *testing.T) {
	unknown := "[stream.hls][info] Filtering out segments, stream output paused"
	states, notes := feedLines([]string{unknown})
	if len(states) != 0 {
		t.Fatalf("states=%v want none", states)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], unknown) {
		t.Fatalf("notes=%q want one with %q", notes, unknown)
	}
}
