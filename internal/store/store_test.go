package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
			{ID: "101", Login: "streamer_a", DisplayName: "Streamer A"},
			{ID: "102", Login: "streamer_b", DisplayName: "Streamer B"},
		},
		Recents: []Channel{
			{ID: "201", Login: "streamer_c", DisplayName: "Streamer C"},
		},
	}

	if err := SaveTo(path, want); err != nil {
		t.Fatalf("SaveTo returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
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
	st := State{Favorites: []Channel{FromTwitch(live)}, Recents: []Channel{FromTwitch(live)}}
	if err := SaveTo(path, st); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	for _, ch := range append(loaded.Favorites, loaded.Recents...) {
		tw := ch.ToTwitch()
		if tw.Viewers != nil {
			t.Fatalf("store must keep identity only, got viewers %v", *tw.Viewers)
		}
		if tw.Availability != twitch.AvailabilityUnknown {
			t.Fatalf("channel loaded from store without viewers = %v, want unknown (never offline)", tw.Availability)
		}
	}
}
