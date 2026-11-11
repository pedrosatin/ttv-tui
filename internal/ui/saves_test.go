package ui

import (
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// blockingStore holds each save until the test releases it.
type blockingStore struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	done    atomic.Bool
}

func newBlockingStore(t *testing.T) *blockingStore {
	s := &blockingStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(s.unblock)
	return s
}

func (s *blockingStore) Save(store.State) error {
	s.entered <- struct{}{}
	<-s.release
	s.done.Store(true)
	return nil
}

func (s *blockingStore) unblock() { s.once.Do(func() { close(s.release) }) }

// oneChannelModel lists a single channel, highlighted.
func oneChannelModel(t *testing.T, opts ...Option) Model {
	t.Helper()
	m := New(testCatalog(t, i18n.LangEN), opts...)
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{{ID: "ch-1", Login: "streamer", DisplayName: "Streamer", Viewers: twitch.IntPtr(500)}}
	m.reconcileHighlight()
	return m
}

// hostedWithPersist is a hosted model built by New, so its saves are ordered.
// The host is set by hand: WithVideoHost also switches the package to exact
// cell widths, which other tests do not expect.
func hostedWithPersist(t *testing.T, persist HostedColumnsPersist) Model {
	t.Helper()
	m := New(testCatalog(t, i18n.LangEN), WithHostedColumns(0, 0, false, false, 0, persist))
	m.hosted.host = &recordingHost{}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 212, Height: 49})
	return next.(Model)
}

var (
	keyFavorite = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
	keyQuit     = tea.KeyMsg{Type: tea.KeyCtrlQ}
)

// Ctrl+→ followed by Ctrl+Q inside the debounce keeps the new width.
func TestFlushSavesWritesTheWidthStillWaitingForTheKey(t *testing.T) {
	var saved []int
	persist := func(list, _ int, _, _ bool, _ int) error {
		saved = append(saved, list)
		return nil
	}
	m := hostedWithPersist(t, persist)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlRight})
	want := m.hosted.listCols
	if want == 0 {
		t.Fatal("Ctrl+→ did not change the list width")
	}
	m, _ = pressKey(m, keyQuit)
	m.FlushSaves(time.Second)
	if !slices.Equal(saved, []int{want}) {
		t.Fatalf("saved list widths %v, want [%d]", saved, want)
	}
}

// With no change waiting, the exit writes nothing to the config.
func TestFlushSavesLeavesAnUnchangedLayoutAlone(t *testing.T) {
	calls := 0
	persist := func(int, int, bool, bool, int) error {
		calls++
		return nil
	}
	m := hostedWithPersist(t, persist)
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyCtrlRight})
	next, cmd := m.Update(hostedSaveMsg{gen: m.hosted.saveGen})
	m = next.(Model)
	cmd()
	m.FlushSaves(time.Second)
	if calls != 1 {
		t.Fatalf("persist ran %d times, want once", calls)
	}
}

// f then Ctrl+Q: the exit waits for the favorite being saved.
func TestFlushSavesWaitsForTheFavoriteBeingSaved(t *testing.T) {
	bs := newBlockingStore(t)
	m := oneChannelModel(t, WithStore(bs))
	m, save := pressKey(m, keyFavorite)
	go save()
	<-bs.entered
	m, _ = pressKey(m, keyQuit)

	waiting := make(chan struct{})
	hook := sync.OnceFunc(func() { close(waiting) })
	flushStarted.Store(&hook)
	t.Cleanup(func() { flushStarted.Store(nil) })
	flushed := make(chan struct{})
	go func() {
		m.FlushSaves(time.Minute)
		close(flushed)
	}()
	select {
	case <-flushed:
		t.Fatal("FlushSaves returned while the favorite was still being saved")
	case <-waiting:
	}
	bs.unblock()
	<-flushed
	if !bs.done.Load() {
		t.Fatal("FlushSaves returned before the save ended")
	}
}

// A save that never ends does not hold the exit past the timeout.
func TestFlushSavesGivesUpAfterTheTimeout(t *testing.T) {
	bs := newBlockingStore(t)
	m := oneChannelModel(t, WithStore(bs))
	m, save := pressKey(m, keyFavorite)
	go save()
	<-bs.entered
	m.FlushSaves(time.Millisecond)
	if bs.done.Load() {
		t.Fatal("the save was expected to still be blocked")
	}
}

// "ff": the second toggle's command reaches the disk first, and the first one
// must not put the favorite back.
func TestQuickFavoriteTogglesLeaveTheLastStateOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	m := oneChannelModel(t, WithStore(store.New(path)))
	m, first := pressKey(m, keyFavorite)
	m, second := pressKey(m, keyFavorite)
	if len(m.Favorites()) != 0 {
		t.Fatalf("favorites on screen = %v, want none", m.Favorites())
	}
	second()
	first()
	st, err := store.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Favorites) != 0 {
		t.Fatalf("favorites on disk = %v, want none", st.Favorites)
	}
}

// Two quick toggles of a language: the older save must not land last.
func TestQuickFilterTogglesLeaveTheLastStateOnDisk(t *testing.T) {
	m := channelFilterFixture(t)
	var mu sync.Mutex
	var onDisk []string
	m.list.prefs.persist = func(languages []string, _ int) error {
		mu.Lock()
		defer mu.Unlock()
		onDisk = slices.Clone(languages)
		return nil
	}
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	for m.list.prefs.menuItems[m.list.prefs.menuIndex].Kind != filterMenuLang {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, first := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, second := pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.FilterLanguages()) != 0 {
		t.Fatalf("languages on screen = %v, want none", m.FilterLanguages())
	}
	second()
	first()
	if len(onDisk) != 0 {
		t.Fatalf("languages on disk = %v, want none", onDisk)
	}
}
