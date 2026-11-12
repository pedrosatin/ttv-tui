package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestStore_NonExistentFileReturnsEmptyWithoutError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom non-existent returned error: %v", err)
	}
	if len(st.Favorites) != 0 || len(st.Recents) != 0 {
		t.Fatalf("expected empty state, got %+v", st)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected state file not to exist on disk: %v", err)
	}
}

func TestStore_RoundTripAtomicAndPermissions0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")

	// Pre-create with loose 0644 permissions to verify overwrite tightens to 0600.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"favorites":[],"recents":[]}`), 0o644); err != nil {
		t.Fatalf("write loose: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	want := State{
		Favorites: []Channel{
			{Platform: PlatformTwitch, ID: "101", Login: "streamer_a", DisplayName: "Streamer A"},
			{Platform: PlatformTwitch, ID: "102", Login: "streamer_b", DisplayName: "Streamer B"},
		},
		Recents: []Channel{
			{Platform: PlatformKick, ID: "201", Login: "streamer_c", DisplayName: "Streamer C"},
		},
	}

	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	// Windows has no mode bits: Go reports 0666 whatever the ACL says.
	if gotPerm := info.Mode().Perm(); runtime.GOOS != "windows" && gotPerm != 0o600 {
		t.Fatalf("got file perm %#o, want 0600", gotPerm)
	}

	// Verify no leftover temporary files in directory
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("unexpected directory entries after atomic save: %v", entries)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom returned error: %v", err)
	}

	if len(got.Favorites) != len(want.Favorites) {
		t.Fatalf("got %d favorites, want %d", len(got.Favorites), len(want.Favorites))
	}
	for i := range want.Favorites {
		if got.Favorites[i] != want.Favorites[i] {
			t.Fatalf("favorite[%d]: got %+v, want %+v", i, got.Favorites[i], want.Favorites[i])
		}
	}

	if len(got.Recents) != len(want.Recents) {
		t.Fatalf("got %d recents, want %d", len(got.Recents), len(want.Recents))
	}
	for i := range want.Recents {
		if got.Recents[i] != want.Recents[i] {
			t.Fatalf("recent[%d]: got %+v, want %+v", i, got.Recents[i], want.Recents[i])
		}
	}
}

func TestStore_RecentUsageOrderWithoutDuplicates(t *testing.T) {
	var st State
	chA := Channel{ID: "1", Login: "alice"}
	chB := Channel{ID: "2", Login: "bob"}
	chC := Channel{ID: "3", Login: "carol"}

	st.AddRecent(chA, 50)
	st.AddRecent(chB, 50)
	st.AddRecent(chC, 50)

	// Order of use: C, B, A
	if len(st.Recents) != 3 {
		t.Fatalf("expected 3 recents, got %d", len(st.Recents))
	}
	if st.Recents[0].Login != "carol" || st.Recents[1].Login != "bob" || st.Recents[2].Login != "alice" {
		t.Fatalf("unexpected order: %+v", st.Recents)
	}

	// Accessing A again moves it to top without duplicates: A, C, B
	st.AddRecent(chA, 50)
	if len(st.Recents) != 3 {
		t.Fatalf("expected 3 recents without duplicates, got %d", len(st.Recents))
	}
	if st.Recents[0].Login != "alice" || st.Recents[1].Login != "carol" || st.Recents[2].Login != "bob" {
		t.Fatalf("unexpected order after re-add: %+v", st.Recents)
	}

	// Case-insensitive login match moves it to top
	chAUpper := Channel{ID: "1", Login: "ALICE"}
	st.AddRecent(chAUpper, 50)
	if len(st.Recents) != 3 {
		t.Fatalf("expected 3 recents, got %d", len(st.Recents))
	}
	if st.Recents[0].Login != "ALICE" || st.Recents[1].Login != "carol" || st.Recents[2].Login != "bob" {
		t.Fatalf("unexpected order after case-insensitive re-add: %+v", st.Recents)
	}
}

func TestStore_RecentsLimit(t *testing.T) {
	var st State
	for i := 1; i <= 10; i++ {
		st.AddRecent(Channel{ID: string(rune('0' + i)), Login: string(rune('a' + i))}, 5)
	}
	if len(st.Recents) != 5 {
		t.Fatalf("expected 5 recents, got %d", len(st.Recents))
	}
	// The newest channel added was 'j' (10th)
	if st.Recents[0].Login != "k" { // 'a'+10 is 'k'
		t.Fatalf("expected newest recent at top, got %q", st.Recents[0].Login)
	}
}

func TestStore_ToggleFavorites(t *testing.T) {
	var st State
	chA := Channel{ID: "1", Login: "streamer_a"}

	if !st.ToggleFavorite(chA) {
		t.Fatalf("expected first toggle to add favorite")
	}
	if !st.IsFavorite(chA) {
		t.Fatalf("expected chA to be in favorites")
	}
	if len(st.Favorites) != 1 {
		t.Fatalf("expected 1 favorite, got %d", len(st.Favorites))
	}

	if st.ToggleFavorite(chA) {
		t.Fatalf("expected second toggle to remove favorite")
	}
	if st.IsFavorite(chA) {
		t.Fatalf("expected chA to not be in favorites")
	}
	if len(st.Favorites) != 0 {
		t.Fatalf("expected 0 favorites, got %d", len(st.Favorites))
	}
}

func TestStore_CorruptJSONGeneratesBakWithoutLosingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	corruptContent := `{"favorites": [corrupted json...`

	if err := os.WriteFile(path, []byte(corruptContent), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	st, err := LoadFrom(path)
	if err == nil || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	if len(st.Favorites) != 0 || len(st.Recents) != 0 {
		t.Fatalf("expected empty state on corrupt file, got %+v", st)
	}

	// Verify original file was renamed to .bak and contains original content
	bakPath := path + ".bak"
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("failed to read .bak file: %v", err)
	}
	if string(bakData) != corruptContent {
		t.Fatalf("expected .bak to hold original content %q, got %q", corruptContent, string(bakData))
	}

	// Verify original file path no longer exists (it was moved)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected original corrupt file to be moved, but it still exists")
	}
}

func TestStore_EmptyFileGeneratesBakWithoutLosingData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// 0 bytes empty file
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	st, err := LoadFrom(path)
	if err == nil || !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt on empty file, got %v", err)
	}
	if len(st.Favorites) != 0 || len(st.Recents) != 0 {
		t.Fatalf("expected empty state, got %+v", st)
	}

	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("expected .bak file to exist: %v", err)
	}
}

func TestStore_StoreObjectDelegation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := New(path)
	if s.Path() != path {
		t.Fatalf("Path() = %q, want %q", s.Path(), path)
	}
	if s.MaxRecents() != DefaultMaxRecents {
		t.Fatalf("MaxRecents() = %d, want %d", s.MaxRecents(), DefaultMaxRecents)
	}
	s.SetMaxRecents(10)
	if s.MaxRecents() != 10 {
		t.Fatalf("MaxRecents() = %d, want 10", s.MaxRecents())
	}

	st := State{
		Favorites: []Channel{{Login: "fav1"}},
		Recents:   []Channel{{Login: "rec1"}},
	}
	if err := s.Save(st); err != nil {
		t.Fatalf("s.Save() error: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("s.Load() error: %v", err)
	}
	if len(loaded.Favorites) != 1 || loaded.Favorites[0].Login != "fav1" {
		t.Fatalf("unexpected loaded favorites: %+v", loaded.Favorites)
	}
}

func TestChannelWithoutViewersIsNotOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	live := twitch.Channel{
		ID: "1", Login: "eslcs", DisplayName: "ESLCS",
		Viewers: twitch.IntPtr(12900), Availability: twitch.AvailabilityLive,
	}
	st := State{Favorites: []Channel{FromChannel(live)}, Recents: []Channel{FromChannel(live)}}
	if err := SaveTo(path, st); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	for _, ch := range append(loaded.Favorites, loaded.Recents...) {
		tw := ch.ToChannel()
		if tw.Viewers != nil {
			t.Fatalf("store must keep identity only, got viewers %v", *tw.Viewers)
		}
		if tw.Availability != twitch.AvailabilityUnknown {
			t.Fatalf("channel loaded from store without viewers = %v, want unknown (never offline)", tw.Availability)
		}
	}
}

// The parent loads both snapshots before either child is allowed to save.
func TestStore_TwoProcesses(t *testing.T) {
	if os.Getenv("TTV_STORE_HELPER") == "1" {
		var st State
		if err := json.Unmarshal([]byte(os.Getenv("TTV_STORE_STATE")), &st); err != nil {
			t.Fatal(err)
		}
		signal := os.Stdin
		var b [1]byte
		if _, err := signal.Read(b[:]); err != nil {
			t.Fatal(err)
		}
		_ = signal.Close()
		if err := SaveTo(os.Getenv("TTV_STORE_PATH"), st); err != nil {
			t.Fatal(err)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SaveTo(path, State{Favorites: []Channel{{Login: "hasanabi"}, {Login: "sodapoppin"}}}); err != nil {
		t.Fatal(err)
	}
	type child struct {
		cmd   *exec.Cmd
		start *os.File
	}
	children := make([]child, 0, 2)
	for _, login := range []string{"zackrawrr", "another"} {
		st, err := LoadFrom(path)
		if err != nil {
			t.Fatal(err)
		}
		st.ToggleFavorite(Channel{Login: login})
		data, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestStore_TwoProcesses$")
		cmd.Env = append(os.Environ(), "TTV_STORE_HELPER=1", "TTV_STORE_PATH="+path, "TTV_STORE_STATE="+string(data))
		cmd.Stdin = r
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		children = append(children, child{cmd, w})
	}
	for _, c := range children {
		_, err := c.start.Write([]byte{1})
		_ = c.start.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range children {
		if err := c.cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Favorites) != 4 {
		t.Fatalf("lost concurrent favorite: %+v", st.Favorites)
	}
}

func TestStore_ConcurrentSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			<-start
			ch := Channel{Login: fmt.Sprintf("channel%d", i)}
			if err := SaveTo(path, State{Favorites: []Channel{ch}, Recents: []Channel{ch}}); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Favorites) != 20 || len(st.Recents) != 20 {
		t.Fatalf("lost entries: %+v", st)
	}
}

func TestStore_StaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("abandoned"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, State{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock left behind: %v", err)
	}
}

// A writer must survive a lock held longer than the old 70-90ms retry window.
func TestStore_WaitsForContendedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lock := path + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	time.AfterFunc(150*time.Millisecond, func() { released <- os.Remove(lock) })
	err := SaveTo(path, State{Favorites: []Channel{{Login: "another"}}})
	if releaseErr := <-released; releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	st, err := LoadFrom(path)
	if err != nil || len(st.Favorites) != 1 || st.Favorites[0].Login != "another" {
		t.Fatalf("save after contention: %+v, %v", st, err)
	}
}

func TestStore_ContendedLockReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := SaveTo(path, State{}); err == nil {
		t.Fatal("expected lock error")
	}
	// Nine production retries sleep for 450-495ms before I/O and scheduling.
	// Allow CI overhead (including Windows timer granularity), but keep the
	// guarantee that persistent contention returns an error within one second.
	if elapsed := time.Since(started); elapsed < 300*time.Millisecond || elapsed > time.Second {
		t.Fatalf("lock wait = %v, want 300ms-1s", elapsed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("contended save changed state: %v", err)
	}
}

func TestStore_RecentsMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var disk State
	for i := 0; i < 60; i++ {
		disk.AddRecent(Channel{Login: fmt.Sprintf("channel%d", i)}, 50)
	}
	if err := SaveTo(path, disk); err != nil {
		t.Fatal(err)
	}
	incoming := State{Recents: []Channel{{Login: "newest"}, {Login: "CHANNEL40"}}}
	if err := SaveTo(path, incoming); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recents) != 50 || got.Recents[0].Login != "newest" || got.Recents[1].Login != "CHANNEL40" || got.Recents[2].Login != "channel59" || got.Recents[49].Login != "channel11" {
		t.Fatalf("bad merged order: %+v", got.Recents)
	}
}

func TestStore_WriteFailureCleansTempAndLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	// A nonempty directory cannot be replaced by the temporary JSON file.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := lockState(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeState(path, State{}); err == nil {
		t.Error("expected rename error")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("orphan file: %v", entries)
	}
	// Public SaveTo also returns read errors and releases its lock.
	if err := SaveTo(path, State{}); err == nil {
		t.Fatal("expected read error")
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("lock leaked: %v", err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, State{}); err != nil {
		t.Fatalf("save after failure: %v", err)
	}
}

func TestStore_SaveCorruptBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	bad := []byte("{broken")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTo(path, State{Favorites: []Channel{{Login: "new"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Fatalf("backup changed: %q", got)
	}
	st, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsFavorite(Channel{Login: "new"}) {
		t.Fatalf("missing favorite: %+v", st)
	}
}

func TestStore_FavoritesUnionAndCustomRecentLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := State{Favorites: []Channel{{ID: "1", Login: "one"}, {Login: "two"}}, Recents: []Channel{{Login: "old"}, {Login: "older"}}}
	if err := SaveTo(path, first); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	s.SetMaxRecents(2)
	next := State{Favorites: []Channel{{ID: "1", Login: "renamed"}, {Login: "TWO"}, {Login: "three"}}, Recents: []Channel{{Login: "new"}}}
	if err := s.Save(next); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Favorites) != 3 {
		t.Fatalf("bad union: %+v", got.Favorites)
	}
	if len(got.Recents) != 2 || got.Recents[0].Login != "new" || got.Recents[1].Login != "old" {
		t.Fatalf("bad recents: %+v", got.Recents)
	}
}

func TestStore_RemovalPersists(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"))
	st := State{Favorites: []Channel{{Login: "alice"}}}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	st.Favorites = nil
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Favorites) != 0 {
		t.Fatalf("removed favorite returned: %+v", got.Favorites)
	}
}

func TestStore_SaveToRemovalPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var st State
	ch := Channel{Login: "alice"}
	st.ToggleFavorite(ch)
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
	st.ToggleFavorite(ch)
	if err := SaveTo(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsFavorite(ch) {
		t.Fatal("removed favorite returned")
	}
}

func TestStore_RemovalOperationsAcknowledged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := New(path)
	ch := Channel{ID: "1", Login: "alice"}
	st := State{Favorites: []Channel{ch}}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	st.Favorites = nil
	// A failed save must leave the removal pending for retry.
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err == nil {
		t.Fatal("expected lock error")
	}
	if len(s.snapshot.favorites) != 1 {
		t.Fatal("failed save acknowledged removal")
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if len(s.snapshot.favorites) != 0 {
		t.Fatal("removed entries retained after save")
	}
	// Once acknowledged, a removal must not delete another pane's later re-add.
	if err := SaveTo(path, State{Favorites: []Channel{ch}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsFavorite(ch) {
		t.Fatal("old removal deleted a later re-add")
	}
	if len(withoutChannels(got.snapshot.favorites, got.Favorites)) != 0 {
		t.Fatal("reload has pending removals")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("bookkeeping serialized: %s", data)
	}
}

func TestStore_RemoveIdentityAndReAdd(t *testing.T) {
	for _, alias := range []Channel{{Login: "ALICE"}, {ID: "1", Login: "renamed"}} {
		t.Run(alias.Login, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			ch := Channel{ID: "1", Login: "alice"}
			if err := SaveTo(path, State{Favorites: []Channel{ch}}); err != nil {
				t.Fatal(err)
			}
			st, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			st.ToggleFavorite(alias)
			if err := SaveTo(path, st); err != nil {
				t.Fatal(err)
			}
			if len(st.snapshot.favorites) != 0 {
				t.Fatal("removal retained")
			}
			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Favorites) != 0 {
				t.Fatal("alias removal failed")
			}
			st.ToggleFavorite(alias)
			if err := SaveTo(path, st); err != nil {
				t.Fatal(err)
			}
			got, err = LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if !got.IsFavorite(alias) {
				t.Fatal("re-add failed")
			}
		})
	}
}

func TestStore_TwoProcessOperations(t *testing.T) {
	if os.Getenv("TTV_STORE_OP_HELPER") == "1" {
		s := New(os.Getenv("TTV_STORE_PATH"))
		st, err := s.Load()
		if err != nil {
			t.Fatal(err)
		}
		st.ToggleFavorite(Channel{Login: os.Getenv("TTV_STORE_LOGIN")})
		if _, err := os.Stdout.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		var signal [1]byte
		if _, err := os.Stdin.Read(signal[:]); err != nil {
			t.Fatal(err)
		}
		if err := s.Save(st); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, tc := range []struct {
		name   string
		remove bool
		order  []int
	}{
		{"add-add", false, []int{0, 1}},
		{"remove-then-add", true, []int{0, 1}},
		{"add-then-remove", true, []int{1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if tc.remove {
				if err := SaveTo(path, State{Favorites: []Channel{{Login: "x"}}}); err != nil {
					t.Fatal(err)
				}
			}
			type child struct {
				cmd   *exec.Cmd
				start *os.File
			}
			var children []child
			for _, login := range []string{"x", "y"} {
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestStore_TwoProcessOperations$")
				cmd.Env = append(os.Environ(), "TTV_STORE_OP_HELPER=1", "TTV_STORE_PATH="+path, "TTV_STORE_LOGIN="+login)
				cmd.Stdin = r
				cmd.Stderr = os.Stderr
				ready, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cmd.Process.Kill(); _ = w.Close(); _ = ready.Close() })
				_ = r.Close()
				var signal [1]byte
				if _, err := ready.Read(signal[:]); err != nil {
					t.Fatal(err)
				}
				children = append(children, child{cmd, w})
			}
			// Both children loaded the same file before either can save.
			for _, i := range tc.order {
				c := children[i]
				if _, err := c.start.Write([]byte{1}); err != nil {
					t.Fatal(err)
				}
				_ = c.start.Close()
				if err := c.cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.IsFavorite(Channel{Login: "x"}) == tc.remove || !got.IsFavorite(Channel{Login: "y"}) {
				t.Fatalf("lost operation: %+v", got.Favorites)
			}
		})
	}
}

func TestStore_PreservesFavoritesWhenDiskCorruptedDuringSession(t *testing.T) {
	for _, tc := range []struct {
		name      string
		corruptFn func(path string) error
		useStore  bool
	}{
		{
			name: "store invalid json",
			corruptFn: func(path string) error {
				return os.WriteFile(path, []byte("{corrupt json"), 0o600)
			},
			useStore: true,
		},
		{
			name: "store empty file",
			corruptFn: func(path string) error {
				return os.WriteFile(path, []byte(""), 0o600)
			},
			useStore: true,
		},
		{
			name: "saveto invalid json",
			corruptFn: func(path string) error {
				return os.WriteFile(path, []byte("{corrupt json"), 0o600)
			},
			useStore: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s := New(path)
			initial := State{
				Favorites: []Channel{
					{Login: "streamer_a"},
					{Login: "streamer_b"},
				},
			}
			if err := s.Save(initial); err != nil {
				t.Fatal(err)
			}

			var st State
			var err error
			if tc.useStore {
				st, err = s.Load()
			} else {
				st, err = LoadFrom(path)
			}
			if err != nil {
				t.Fatal(err)
			}

			if err := tc.corruptFn(path); err != nil {
				t.Fatal(err)
			}

			st.ToggleFavorite(Channel{Login: "streamer_c"})
			if tc.useStore {
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := SaveTo(path, st); err != nil {
					t.Fatal(err)
				}
			}

			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}

			for _, login := range []string{"streamer_a", "streamer_b", "streamer_c"} {
				if !got.IsFavorite(Channel{Login: login}) {
					t.Fatalf("expected favorite %q to be preserved, got %+v", login, got.Favorites)
				}
			}
		})
	}
}

func TestStore_RespectsRemovalWhenDiskCorruptedDuringSession(t *testing.T) {
	for _, tc := range []struct {
		name     string
		useStore bool
	}{
		{name: "store", useStore: true},
		{name: "saveto", useStore: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			s := New(path)
			initial := State{
				Favorites: []Channel{
					{Login: "streamer_a"},
					{Login: "streamer_b"},
				},
			}
			if err := s.Save(initial); err != nil {
				t.Fatal(err)
			}

			var st State
			var err error
			if tc.useStore {
				st, err = s.Load()
			} else {
				st, err = LoadFrom(path)
			}
			if err != nil {
				t.Fatal(err)
			}

			// Remove streamer_a before corruption
			st.ToggleFavorite(Channel{Login: "streamer_a"})

			// Corrupt file on disk during session
			if err := os.WriteFile(path, []byte("{broken json"), 0o600); err != nil {
				t.Fatal(err)
			}

			// Add streamer_c
			st.ToggleFavorite(Channel{Login: "streamer_c"})

			if tc.useStore {
				if err := s.Save(st); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := SaveTo(path, st); err != nil {
					t.Fatal(err)
				}
			}

			got, err := LoadFrom(path)
			if err != nil {
				t.Fatal(err)
			}

			if got.IsFavorite(Channel{Login: "streamer_a"}) {
				t.Fatalf("removed favorite streamer_a should not be present, got %+v", got.Favorites)
			}
			if !got.IsFavorite(Channel{Login: "streamer_b"}) {
				t.Fatalf("expected streamer_b to be preserved, got %+v", got.Favorites)
			}
			if !got.IsFavorite(Channel{Login: "streamer_c"}) {
				t.Fatalf("expected streamer_c to be preserved, got %+v", got.Favorites)
			}
		})
	}
}

func TestStore_LegacyStateWithoutPlatformLoadsAsTwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Legacy state file without "platform" field.
	legacyJSON := `{
  "favorites": [
    {
      "id": "1",
      "login": "streamer_a",
      "display_name": "Streamer A"
    },
    {
      "id": "2",
      "login": "streamer_b",
      "display_name": "Streamer B"
    }
  ],
  "recents": [
    {
      "id": "3",
      "login": "streamer_c",
      "display_name": "Streamer C"
    },
    {
      "id": "4",
      "login": "streamer_d",
      "display_name": "Streamer D"
    }
  ]
}`

	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	st, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom legacy state: %v", err)
	}

	if len(st.Favorites) != 2 {
		t.Fatalf("expected 2 favorites, got %d", len(st.Favorites))
	}
	if len(st.Recents) != 2 {
		t.Fatalf("expected 2 recents, got %d", len(st.Recents))
	}

	// Verify all channels loaded with PlatformTwitch as default without losing metadata.
	for i, want := range []struct {
		id, login, disp string
	}{
		{"1", "streamer_a", "Streamer A"},
		{"2", "streamer_b", "Streamer B"},
	} {
		fav := st.Favorites[i]
		if fav.Platform != PlatformTwitch || fav.ID != want.id || fav.Login != want.login || fav.DisplayName != want.disp {
			t.Fatalf("favorite[%d]: got %+v, want platform %q, id %q, login %q, display %q", i, fav, PlatformTwitch, want.id, want.login, want.disp)
		}
	}

	// Verify recents order and metadata are preserved.
	for i, want := range []struct {
		id, login, disp string
	}{
		{"3", "streamer_c", "Streamer C"},
		{"4", "streamer_d", "Streamer D"},
	} {
		rec := st.Recents[i]
		if rec.Platform != PlatformTwitch || rec.ID != want.id || rec.Login != want.login || rec.DisplayName != want.disp {
			t.Fatalf("recent[%d]: got %+v, want platform %q, id %q, login %q, display %q", i, rec, PlatformTwitch, want.id, want.login, want.disp)
		}
	}

	// Save back to disk and verify the file now includes "platform": "twitch".
	if err := SaveTo(path, st); err != nil {
		t.Fatalf("SaveTo migrated state: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved state: %v", err)
	}

	var raw map[string][]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw saved state: %v", err)
	}
	for _, fav := range raw["favorites"] {
		if fav["platform"] != PlatformTwitch {
			t.Fatalf("saved favorite missing platform: %v", fav)
		}
	}
	for _, rec := range raw["recents"] {
		if rec["platform"] != PlatformTwitch {
			t.Fatalf("saved recent missing platform: %v", rec)
		}
	}
}

func TestStore_MultiplatformChannelsAreDistinct(t *testing.T) {
	twitchCh := Channel{Platform: PlatformTwitch, ID: "100", Login: "xqc", DisplayName: "xQc"}
	kickCh := Channel{Platform: PlatformKick, ID: "100", Login: "xqc", DisplayName: "xQc"}
	ytCh := Channel{Platform: PlatformYouTube, ID: "100", Login: "xqc", DisplayName: "xQc"}
	defaultCh := Channel{ID: "100", Login: "xqc", DisplayName: "xQc"} // empty platform defaults to twitch

	t.Run("Matches respects platform distinction", func(t *testing.T) {
		if twitchCh.Matches(kickCh) {
			t.Fatalf("expected twitch channel not to match kick channel with same login/id")
		}
		if kickCh.Matches(twitchCh) {
			t.Fatalf("expected kick channel not to match twitch channel")
		}
		if kickCh.Matches(ytCh) {
			t.Fatalf("expected kick channel not to match youtube channel")
		}
		if !twitchCh.Matches(defaultCh) {
			t.Fatalf("expected twitch channel to match channel with empty platform default")
		}
		if !defaultCh.Matches(twitchCh) {
			t.Fatalf("expected channel with empty platform default to match twitch channel")
		}
		if defaultCh.Matches(kickCh) {
			t.Fatalf("expected channel with empty platform default not to match kick channel")
		}
	})

	t.Run("ToggleFavorite and IsFavorite handle distinct platforms", func(t *testing.T) {
		var st State

		if !st.ToggleFavorite(twitchCh) {
			t.Fatal("expected adding twitch channel to succeed")
		}
		if !st.IsFavorite(twitchCh) {
			t.Fatal("expected twitch channel to be favorite")
		}
		if st.IsFavorite(kickCh) {
			t.Fatal("expected kick channel not to be favorite yet")
		}

		// Adding kick channel should coexist without removing or overwriting twitch channel
		if !st.ToggleFavorite(kickCh) {
			t.Fatal("expected adding kick channel to succeed")
		}
		if !st.IsFavorite(twitchCh) || !st.IsFavorite(kickCh) {
			t.Fatalf("expected both twitch and kick channels to be favorites, got %+v", st.Favorites)
		}
		if len(st.Favorites) != 2 {
			t.Fatalf("expected 2 favorites, got %d", len(st.Favorites))
		}

		// Removing twitch channel should leave kick channel intact
		if st.ToggleFavorite(twitchCh) {
			t.Fatal("expected toggling twitch channel again to remove it")
		}
		if st.IsFavorite(twitchCh) {
			t.Fatal("expected twitch channel to no longer be favorite")
		}
		if !st.IsFavorite(kickCh) {
			t.Fatal("expected kick channel to remain favorite")
		}
		if len(st.Favorites) != 1 {
			t.Fatalf("expected 1 favorite remaining, got %d", len(st.Favorites))
		}
	})

	t.Run("AddRecent preserves both platforms and updates recents order correctly", func(t *testing.T) {
		var st State

		st.AddRecent(twitchCh, 10)
		st.AddRecent(kickCh, 10)

		if len(st.Recents) != 2 {
			t.Fatalf("expected 2 recents, got %d", len(st.Recents))
		}
		if st.Recents[0].Platform != PlatformKick || st.Recents[1].Platform != PlatformTwitch {
			t.Fatalf("expected [kick, twitch], got %+v", st.Recents)
		}

		// Re-adding twitch channel moves it to the top without removing kick channel
		st.AddRecent(twitchCh, 10)
		if len(st.Recents) != 2 {
			t.Fatalf("expected 2 recents without duplicates, got %d", len(st.Recents))
		}
		if st.Recents[0].Platform != PlatformTwitch || st.Recents[1].Platform != PlatformKick {
			t.Fatalf("expected [twitch, kick], got %+v", st.Recents)
		}
	})

	t.Run("Persistence and union across platforms", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")

		st := State{
			Favorites: []Channel{twitchCh, kickCh},
			Recents:   []Channel{kickCh, twitchCh},
		}

		if err := SaveTo(path, st); err != nil {
			t.Fatalf("SaveTo multiplatform state: %v", err)
		}

		loaded, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("LoadFrom multiplatform state: %v", err)
		}

		if len(loaded.Favorites) != 2 || !loaded.IsFavorite(twitchCh) || !loaded.IsFavorite(kickCh) {
			t.Fatalf("favorites after roundtrip: %+v", loaded.Favorites)
		}
		if len(loaded.Recents) != 2 || loaded.Recents[0].Platform != PlatformKick || loaded.Recents[1].Platform != PlatformTwitch {
			t.Fatalf("recents after roundtrip: %+v", loaded.Recents)
		}
	})
}

func TestStore_PlatformIsStoredCanonical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var st State
	st.ToggleFavorite(Channel{Platform: "Kick", ID: "1", Login: "a"})
	st.ToggleFavorite(Channel{Platform: " ", ID: "2", Login: "b"})
	st.AddRecent(Channel{Platform: " YouTube ", ID: "3", Login: "c"}, 0)
	st.Recents = append(st.Recents, Channel{Platform: "TWITCH", ID: "4", Login: "d"})

	want := map[string]string{"a": PlatformKick, "b": PlatformTwitch, "c": PlatformYouTube}
	for _, ch := range append(append([]Channel(nil), st.Favorites...), st.Recents[:1]...) {
		if ch.Platform != want[ch.Login] {
			t.Fatalf("in memory %q: platform %q, want %q", ch.Login, ch.Platform, want[ch.Login])
		}
	}

	if err := SaveTo(path, st); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	want["d"] = PlatformTwitch
	for _, ch := range append(append([]Channel(nil), loaded.Favorites...), loaded.Recents...) {
		if ch.Platform != want[ch.Login] {
			t.Fatalf("on disk %q: platform %q, want %q", ch.Login, ch.Platform, want[ch.Login])
		}
	}
}

func TestStore_ChannelWithURLRoundTripAndLegacyCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// 1. Channel with URL in Favorites and Recents saves and loads accurately.
	orig := State{
		Favorites: []Channel{
			{
				Platform:    PlatformKick,
				ID:          "kick:xqc",
				Login:       "xqc",
				DisplayName: "xqc",
				URL:         "https://kick.com/xqc",
			},
		},
		Recents: []Channel{
			{
				Platform:    PlatformYouTube,
				ID:          "youtube:dQw4w9WgXcQ",
				Login:       "dQw4w9WgXcQ",
				DisplayName: "Rick Astley",
				URL:         "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			},
			{
				Platform:    PlatformTwitch,
				ID:          "123",
				Login:       "gaules",
				DisplayName: "Gaules",
			},
		},
	}

	if err := SaveTo(path, orig); err != nil {
		t.Fatalf("SaveTo with URLs: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom with URLs: %v", err)
	}

	if len(loaded.Favorites) != 1 || loaded.Favorites[0].URL != "https://kick.com/xqc" {
		t.Fatalf("loaded favorites URL: %+v", loaded.Favorites)
	}
	if len(loaded.Recents) != 2 {
		t.Fatalf("loaded recents count: %d", len(loaded.Recents))
	}
	if loaded.Recents[0].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("recent[0] URL = %q, want YouTube URL", loaded.Recents[0].URL)
	}
	if loaded.Recents[1].URL != "" {
		t.Fatalf("recent[1] Twitch URL = %q, want empty", loaded.Recents[1].URL)
	}

	// 2. Conversion to and from twitch.Channel and catalog.Channel preserves URL and Platform.
	tCh := loaded.Favorites[0].ToChannel()
	if tCh.Platform != PlatformKick || tCh.URL != "https://kick.com/xqc" {
		t.Fatalf("ToChannel did not preserve platform or URL: %+v", tCh)
	}
	backCh := FromChannel(tCh)
	if backCh.Platform != PlatformKick || backCh.URL != "https://kick.com/xqc" {
		t.Fatalf("FromChannel did not preserve platform or URL: %+v", backCh)
	}

	catCh := loaded.Favorites[0].ToChannel()
	if catCh.Platform != PlatformKick || catCh.URL != "https://kick.com/xqc" {
		t.Fatalf("ToChannel did not preserve platform or URL: %+v", catCh)
	}
	backCatCh := FromChannel(catCh)
	if backCatCh.Platform != PlatformKick || backCatCh.URL != "https://kick.com/xqc" {
		t.Fatalf("FromChannel did not preserve platform or URL: %+v", backCatCh)
	}

	catChs := ToChannels(loaded.Favorites)
	if len(catChs) != 1 || catChs[0].Platform != PlatformKick || catChs[0].URL != "https://kick.com/xqc" {
		t.Fatalf("ToChannels did not preserve platform or URL: %+v", catChs)
	}
	backFromChs := FromChannels(catChs)
	if len(backFromChs) != 1 || backFromChs[0].Platform != PlatformKick || backFromChs[0].URL != "https://kick.com/xqc" {
		t.Fatalf("FromChannels did not preserve platform or URL: %+v", backFromChs)
	}

	// 3. Matches compares platform + stable identifier, not raw URL.
	// Kick channel with kick.com vs www.kick.com matches (no duplicate).
	kickA := Channel{Platform: PlatformKick, Login: "xqc", URL: "https://kick.com/xqc"}
	kickB := Channel{Platform: PlatformKick, Login: "xqc", URL: "https://www.kick.com/xqc"}
	kickOther := Channel{Platform: PlatformKick, Login: "other", URL: "https://kick.com/other"}
	if !kickA.Matches(kickB) {
		t.Fatalf("expected kickA to match kickB (kick.com vs www.kick.com)")
	}
	if kickA.Matches(kickOther) {
		t.Fatalf("expected kickA not to match kickOther")
	}

	// YouTube video IDs preserve case: AbC and abc are different videos.
	ytUpper := Channel{Platform: PlatformYouTube, Login: "AbC", URL: "https://youtu.be/AbC"}
	ytLower := Channel{Platform: PlatformYouTube, Login: "abc", URL: "https://youtu.be/abc"}
	ytWatch := Channel{Platform: PlatformYouTube, Login: "AbC", URL: "https://www.youtube.com/watch?v=AbC"}
	if ytUpper.Matches(ytLower) {
		t.Fatalf("expected YouTube AbC not to match abc (case-sensitive video IDs)")
	}
	if !ytUpper.Matches(ytWatch) {
		t.Fatalf("expected YouTube youtu.be/AbC to match youtube.com/watch?v=AbC")
	}
}
