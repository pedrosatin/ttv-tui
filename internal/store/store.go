package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// DefaultMaxRecents is the default maximum number of recent channels saved.
const DefaultMaxRecents = 50

// ErrCorrupt indicates that the state file was found to be corrupted (invalid JSON or empty)
// and was renamed to a backup file (.bak).
var ErrCorrupt = errors.New("state file corrupted")

// Channel represents the persistent metadata of a saved Twitch channel.
type Channel struct {
	ID          string `json:"id,omitempty"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name,omitempty"`
}

// Matches reports whether two channels represent the same Twitch channel.
func (c Channel) Matches(other Channel) bool {
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
}

// AddRecent prepends ch to recents, removing any prior duplicate, and clamps to maxRecents.
func (s *State) AddRecent(ch Channel, maxRecents int) {
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

// DefaultPath resolves the default state file path (~/.local/state/ttv-tui/state.json).
func DefaultPath() (string, error) {
	dir, err := config.DefaultStateDir()
	if err != nil {
		return "", fmt.Errorf("resolve default state dir: %w", err)
	}
	return filepath.Join(dir, "state.json"), nil
}

// Load reads state from DefaultPath().
func Load() (State, error) {
	path, err := DefaultPath()
	if err != nil {
		return State{}, fmt.Errorf("resolve default state path: %w", err)
	}
	return LoadFrom(path)
}

// LoadFrom reads state from the JSON file at path.
// If the file does not exist, an empty State is returned without error.
// If the file is empty or contains invalid JSON, the file is renamed to path+".bak",
// an empty State is returned, and ErrCorrupt is returned.
func LoadFrom(path string) (State, error) {
	emptyState := State{
		Favorites: []Channel{},
		Recents:   []Channel{},
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
		_ = os.Rename(path, bakPath)
		return emptyState, fmt.Errorf("%w: file is empty, backed up to %s", ErrCorrupt, bakPath)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		bakPath := path + ".bak"
		_ = os.Rename(path, bakPath)
		return emptyState, fmt.Errorf("%w: invalid JSON (%v), backed up to %s", ErrCorrupt, err, bakPath)
	}

	if st.Favorites == nil {
		st.Favorites = []Channel{}
	}
	if st.Recents == nil {
		st.Recents = []Channel{}
	}
	return st, nil
}

// Save writes state to DefaultPath().
func Save(st State) error {
	path, err := DefaultPath()
	if err != nil {
		return fmt.Errorf("resolve default state path: %w", err)
	}
	return SaveTo(path, st)
}

// SaveTo writes state atomically to path with 0600 permissions.
func SaveTo(path string, st State) error {
	if st.Favorites == nil {
		st.Favorites = []Channel{}
	}
	if st.Recents == nil {
		st.Recents = []Channel{}
	}

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

	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod state %s: %w", path, err)
	}
	return nil
}

// Store manages persistent state at a given path.
type Store struct {
	path       string
	maxRecents int
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
	return LoadFrom(s.path)
}

// Save saves state to the store's path.
func (s *Store) Save(st State) error {
	return SaveTo(s.path, st)
}
