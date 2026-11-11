package ui

import (
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// saveSlot orders the saves of one kind: favorites and recents, the channel
// filters, or the panel layout. Each save runs in a command of its own, so two
// quick changes race to the disk; a save older than one already written is
// dropped, and each save carries the whole state, so the newest one is enough.
type saveSlot struct {
	// asked and latest belong to the update loop, and FlushSaves runs after it.
	asked  int
	latest func() tea.Msg

	mu      sync.Mutex // held while a save runs
	written int
}

// cmd numbers save and wraps it in the command that runs it. A nil slot (a
// model built without New) runs save as it is.
func (s *saveSlot) cmd(save func() tea.Msg) tea.Cmd {
	if s == nil {
		return save
	}
	s.asked++
	gen := s.asked
	s.latest = save
	return func() tea.Msg { return s.run(gen, save) }
}

func (s *saveSlot) run(gen int, save func() tea.Msg) tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen <= s.written {
		return nil
	}
	s.written = gen
	return save()
}

// flush waits for the save in progress and writes the newest one if its
// command has not run yet.
func (s *saveSlot) flush() {
	if s != nil && s.latest != nil {
		s.run(s.asked, s.latest)
	}
}

// flushStarted, when set, runs as FlushSaves starts waiting; tests set it.
var flushStarted atomic.Pointer[func()]

// FlushSaves waits, for at most timeout, for the saves still running, and
// writes what has not reached the disk: a save whose command never ran and the
// panel widths still waiting for the arrow key to stop. The app calls it once
// the program has ended, so a quick Ctrl+Q loses no change and does not leave
// the state file locked.
func (m Model) FlushSaves(timeout time.Duration) {
	if m.hosted.saveGen != m.hosted.queuedGen {
		m.hosted.saveColumns(m.hosted.saveGen)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if hook := flushStarted.Load(); hook != nil {
			(*hook)()
		}
		for _, s := range []*saveSlot{m.storeSaves, m.list.prefs.saves, m.hosted.saves} {
			s.flush()
		}
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
