package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// DefaultMaxRecents is the default maximum number of recent channels saved.
const DefaultMaxRecents = 50

// ErrCorrupt indicates that the state file was found to be corrupted (invalid JSON or empty)
// and was renamed to a backup file (.bak).
var ErrCorrupt = errors.New("state file corrupted")

// Platform constants for supported streaming platforms.
const (
	PlatformTwitch  = "twitch"
	PlatformKick    = "kick"
	PlatformYouTube = "youtube"
)

// DefaultPlatform is the platform assigned to channels when none is specified.
const DefaultPlatform = PlatformTwitch

// Channel represents the persistent metadata of a saved channel.
type Channel struct {
	Platform    string `json:"platform"`
	ID          string `json:"id,omitempty"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name,omitempty"`
}

// CanonicalPlatform returns the trimmed, lowercase platform name for c, or
// DefaultPlatform when nothing is left after trimming.
func (c Channel) CanonicalPlatform() string {
	p := strings.ToLower(strings.TrimSpace(c.Platform))
	if p == "" {
		return DefaultPlatform
	}
	return p
}

// Matches reports whether two channels represent the same channel on the same platform.
func (c Channel) Matches(other Channel) bool {
	if c.CanonicalPlatform() != other.CanonicalPlatform() {
		return false
	}
	if c.ID != "" && other.ID != "" && c.ID == other.ID {
		return true
	}
	return strings.EqualFold(c.Login, other.Login)
}

// ToTwitch converts store.Channel to twitch.Channel.
func (c Channel) ToTwitch() twitch.Channel {
	disp := c.DisplayName
	if disp == "" {
		disp = c.Login
	}
	id := c.ID
	if id == "" {
		id = c.Login
	}
	return twitch.Channel{
		ID:          id,
		Login:       c.Login,
		DisplayName: disp,
	}
}

// FromTwitch converts twitch.Channel to store.Channel.
func FromTwitch(ch twitch.Channel) Channel {
	disp := ch.DisplayName
	if disp == "" {
		disp = ch.Login
	}
	id := ch.ID
	if id == "" {
		id = ch.Login
	}
	return Channel{
		Platform:    PlatformTwitch,
		ID:          id,
		Login:       ch.Login,
		DisplayName: disp,
	}
}

// ToTwitchChannels converts a slice of store.Channel to []twitch.Channel.
func ToTwitchChannels(channels []Channel) []twitch.Channel {
	out := make([]twitch.Channel, len(channels))
	for i, c := range channels {
		out[i] = c.ToTwitch()
	}
	return out
}

// FromTwitchChannels converts a slice of twitch.Channel to []store.Channel.
func FromTwitchChannels(channels []twitch.Channel) []Channel {
	out := make([]Channel, len(channels))
	for i, c := range channels {
		out[i] = FromTwitch(c)
	}
	return out
}

// State represents the persistent application state containing favorites and recents.
type State struct {
	Favorites []Channel `json:"favorites"`
	Recents   []Channel `json:"recents"`
	// snapshot is local bookkeeping, never serialized. Copies share successful-save
	// acknowledgements because SaveTo accepts State by value.
	snapshot *favoriteSnapshot
}

type favoriteSnapshot struct {
	mu        sync.Mutex
	favorites []Channel
}

// AddRecent prepends ch to recents, removing any prior duplicate, and clamps to maxRecents.
func (s *State) AddRecent(ch Channel, maxRecents int) {
	ch.Platform = ch.CanonicalPlatform()
	if maxRecents <= 0 {
		maxRecents = DefaultMaxRecents
	}
	filtered := make([]Channel, 0, len(s.Recents))
	for _, r := range s.Recents {
		if !r.Matches(ch) {
			filtered = append(filtered, r)
		}
	}
	s.Recents = append([]Channel{ch}, filtered...)
	if len(s.Recents) > maxRecents {
		s.Recents = s.Recents[:maxRecents]
	}
}

// ToggleFavorite toggles ch in favorites. If present, removes it and returns false.
// If absent, appends it and returns true.
func (s *State) ToggleFavorite(ch Channel) bool {
	ch.Platform = ch.CanonicalPlatform()
	if s.snapshot == nil {
		s.snapshot = &favoriteSnapshot{favorites: append([]Channel(nil), s.Favorites...)}
	}
	for i, fav := range s.Favorites {
		if fav.Matches(ch) {
			s.Favorites = append(s.Favorites[:i], s.Favorites[i+1:]...)
			return false
		}
	}
	s.Favorites = append(s.Favorites, ch)
	return true
}

// IsFavorite reports whether ch is currently in favorites.
func (s *State) IsFavorite(ch Channel) bool {
	for _, fav := range s.Favorites {
		if fav.Matches(ch) {
			return true
		}
	}
	return false
}

// DefaultPath resolves the default state file path (state.json in config.DefaultStateDir).
func DefaultPath() (string, error) {
	dir, err := config.DefaultStateDir()
	if err != nil {
		return "", fmt.Errorf("resolve default state dir: %w", err)
	}
	return filepath.Join(dir, "state.json"), nil
}

// LoadFrom reads state from the JSON file at path.
// If the file does not exist, an empty State is returned without error.
// If the file is empty or contains invalid JSON, the file is renamed to path+".bak",
// an empty State is returned, and ErrCorrupt is returned.
func LoadFrom(path string) (st State, err error) {
	release, err := lockState(path)
	if err != nil {
		return State{}, err
	}
	defer func() { err = errors.Join(err, release()) }()
	return loadFrom(path)
}

func loadFrom(path string) (State, error) {
	emptyState := State{
		Favorites: []Channel{},
		Recents:   []Channel{},
		snapshot:  &favoriteSnapshot{},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyState, nil
		}
		return emptyState, fmt.Errorf("read state %s: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		bakPath := path + ".bak"
		if err := os.Rename(path, bakPath); err != nil {
			return emptyState, fmt.Errorf("back up corrupt state: %w", err)
		}
		return emptyState, fmt.Errorf("%w: file is empty, backed up to %s", ErrCorrupt, bakPath)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		bakPath := path + ".bak"
		if err := os.Rename(path, bakPath); err != nil {
			return emptyState, fmt.Errorf("back up corrupt state: %w", err)
		}
		return emptyState, fmt.Errorf("%w: invalid JSON (%v), backed up to %s", ErrCorrupt, err, bakPath)
	}

	if st.Favorites == nil {
		st.Favorites = []Channel{}
	}
	if st.Recents == nil {
		st.Recents = []Channel{}
	}
	st.Favorites = normalizeChannels(st.Favorites)
	st.Recents = normalizeChannels(st.Recents)
	st.snapshot = &favoriteSnapshot{favorites: append([]Channel(nil), st.Favorites...)}
	return st, nil
}

// SaveTo merges state with the latest disk contents under an exclusive lock.
// Loaded states apply only changes since the last successful load/save, so stale
// favorites cannot resurrect another pane's removals. Untracked literals add their
// favorites to disk. Incoming recents precede disk-only recents.
func SaveTo(path string, st State) error {
	return saveTo(path, st, DefaultMaxRecents)
}

func saveTo(path string, st State, maxRecents int) error {
	if st.snapshot == nil {
		return mergeTo(path, st, maxRecents, st.Favorites, nil)
	}
	st.snapshot.mu.Lock()
	defer st.snapshot.mu.Unlock()
	return saveSnapshot(path, st, maxRecents, st.snapshot)
}

// saveSnapshot acknowledges only the local snapshot, not the merged disk state:
// channels added by another pane must not become implicit local additions.
// The caller holds snapshot.mu.
func saveSnapshot(path string, st State, maxRecents int, snapshot *favoriteSnapshot) error {
	added := withoutChannels(st.Favorites, snapshot.favorites)
	removed := withoutChannels(snapshot.favorites, st.Favorites)
	if err := mergeTo(path, st, maxRecents, added, removed); err != nil {
		return err
	}
	snapshot.favorites = append([]Channel(nil), st.Favorites...)
	return nil
}

func mergeTo(path string, st State, maxRecents int, added, removed []Channel) (err error) {
	release, err := lockState(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	disk, err := loadFrom(path)
	if err != nil && !errors.Is(err, ErrCorrupt) {
		return err
	}
	if errors.Is(err, ErrCorrupt) {
		// The file went to .bak; the session state is the only source left.
		// Another pane's changes since our load stay in .bak only.
		added = st.Favorites
	}
	st.Favorites = unionChannels(withoutChannels(disk.Favorites, removed), added)
	st.Recents = unionChannels(st.Recents, disk.Recents)
	if len(st.Recents) > maxRecents {
		st.Recents = st.Recents[:maxRecents]
	}
	return writeState(path, st)
}

// withoutChannels uses the same identity rules for removals and additions.
func withoutChannels(channels, excluded []Channel) []Channel {
	out := make([]Channel, 0, len(channels))
	for _, ch := range channels {
		matched := false
		for _, other := range excluded {
			if ch.Matches(other) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, ch)
		}
	}
	return out
}

// unionChannels preserves the first occurrence and its metadata, including
// case-insensitive login and ID aliases recognized by Channel.Matches.
func unionChannels(first, second []Channel) []Channel {
	out := make([]Channel, 0, len(first)+len(second))
	for _, channels := range [][]Channel{first, second} {
		for _, ch := range channels {
			found := false
			for _, existing := range out {
				if existing.Matches(ch) {
					found = true
					break
				}
			}
			if !found {
				out = append(out, ch)
			}
		}
	}
	return out
}

// normalizeChannels stores the canonical platform on every channel, so what is
// written matches what Matches compares.
func normalizeChannels(channels []Channel) []Channel {
	for i := range channels {
		channels[i].Platform = channels[i].CanonicalPlatform()
	}
	return channels
}

// writeState replaces path with a complete file in the same directory.
// Its caller must hold the state lock.
func writeState(path string, st State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp state in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp state: %w", err)
	}

	st.Favorites = normalizeChannels(st.Favorites)
	st.Recents = normalizeChannels(st.Recents)

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp state to %s: %w", path, err)
	}
	cleanup = false

	return nil
}

// Store manages persistent state at a given path.
type Store struct {
	path       string
	maxRecents int
	snapshot   favoriteSnapshot
}

// New creates a new Store instance for path.
func New(path string) *Store {
	return &Store{
		path:       path,
		maxRecents: DefaultMaxRecents,
	}
}

// Path returns the configured state file path.
func (s *Store) Path() string {
	return s.path
}

// MaxRecents returns the maximum number of recent channels to retain.
func (s *Store) MaxRecents() int {
	if s.maxRecents <= 0 {
		return DefaultMaxRecents
	}
	return s.maxRecents
}

// SetMaxRecents sets the maximum number of recent channels to retain.
func (s *Store) SetMaxRecents(max int) {
	s.maxRecents = max
}

// Load loads state from the store's path.
func (s *Store) Load() (State, error) {
	s.snapshot.mu.Lock()
	defer s.snapshot.mu.Unlock()
	st, err := LoadFrom(s.path)
	if err == nil || errors.Is(err, ErrCorrupt) {
		s.snapshot.favorites = append([]Channel(nil), st.Favorites...)
	}
	return st, err
}

// Save saves state to the store's path.
func (s *Store) Save(st State) error {
	s.snapshot.mu.Lock()
	defer s.snapshot.mu.Unlock()
	return saveSnapshot(s.path, st, s.MaxRecents(), &s.snapshot)
}
