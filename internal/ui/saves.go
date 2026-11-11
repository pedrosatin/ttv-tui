package ui

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// saveSlot orders the saves of one kind: favorites and recents, the channel
// filters, or the panel layout. Each save runs in a command of its own, so two
// quick changes race to the disk. Each save carries the whole state, so the
// newest one is enough: a save older than one already tried is dropped, and the
// newest one that failed is tried again when the app exits. Models must come
// from New, which creates the slots.
type saveSlot struct {
	// asked and latest belong to the update loop, and FlushSaves runs after it.
	asked  int
	latest func() tea.Msg

	mu      sync.Mutex // held while a save runs
	tried   int
	written int
}

// savedMsg is the message a save ends with.
type savedMsg interface{ saveErr() error }

func (m storeSavedMsg) saveErr() error  { return m.Err }
func (m configSavedMsg) saveErr() error { return m.Err }

// cmd numbers save and wraps it in the command that runs it.
func (s *saveSlot) cmd(save func() tea.Msg) tea.Cmd {
	s.asked++
	gen := s.asked
	s.latest = save
	return func() tea.Msg { return s.run(gen, save) }
}

func (s *saveSlot) run(gen int, save func() tea.Msg) tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen < s.tried || gen <= s.written {
		return nil
	}
	s.tried = gen
	msg := save()
	if r, ok := msg.(savedMsg); ok && r.saveErr() == nil {
		s.written = gen
	}
	return msg
}

// flush waits for the save in progress and writes the newest one if it has not
// reached the disk: its command never ran, or it failed.
func (s *saveSlot) flush() error {
	if s.latest == nil {
		return nil
	}
	if r, ok := s.run(s.asked, s.latest).(savedMsg); ok {
		return r.saveErr()
	}
	return nil
}

// flushStarted, when set, runs as FlushSaves starts waiting; tests set it.
var flushStarted atomic.Pointer[func()]

// FlushSaves waits, for at most timeout, for the saves still running, and
// writes what has not reached the disk: a save whose command never ran and the
// panel widths still waiting for the arrow key to stop. The app calls it once
// the program has ended, so a quick Ctrl+Q loses no change and does not leave
// the state file locked. It returns the failed saves and the timeout.
func (m Model) FlushSaves(timeout time.Duration) error {
	if m.hosted.saveGen != m.hosted.queuedGen {
		m.hosted.saveColumns(m.hosted.saveGen)
	}
	done := make(chan error, 1)
	go func() {
		if hook := flushStarted.Load(); hook != nil {
			(*hook)()
		}
		var errs []error
		for _, s := range []*saveSlot{m.storeSaves, m.list.prefs.saves, m.hosted.saves} {
			errs = append(errs, s.flush())
		}
		done <- errors.Join(errs...)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf(m.t("saves.timeout"), timeout)
	}
}
