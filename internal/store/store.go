package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// DefaultMaxRecents is the default maximum number of recent channels saved.
const DefaultMaxRecents = 50

// ErrCorrupt indicates that the state file was found to be corrupted (invalid JSON or empty)
// and was renamed to a backup file (.bak).
var ErrCorrupt = errors.New("state file corrupted")

// Platform constants for supported streaming platforms.
const (
	PlatformTwitch  = platform.Twitch
	PlatformKick    = platform.Kick
	PlatformYouTube = platform.YouTube
)

// DefaultPlatform is the platform assigned to channels when none is specified.
const DefaultPlatform = platform.Default

// Channel represents the persistent metadata of a saved channel.
type Channel struct {
	Platform    string `json:"platform"`
	ID          string `json:"id,omitempty"`
	Login       string `json:"login"`
	DisplayName string `json:"display_name,omitempty"`
	URL         string `json:"url,omitempty"`
}

// CanonicalPlatform returns the trimmed, lowercase platform name for c, or
// DefaultPlatform when nothing is left after trimming.
func (c Channel) CanonicalPlatform() string {
	return platform.Normalize(c.Platform)
}

// Matches reports whether two channels represent the same channel on the same platform.
// For non-Twitch channels, it compares platform and stable identifier rather than raw URL.
func (c Channel) Matches(other Channel) bool {
	return catalog.SameChannel(c.ToChannel(), other.ToChannel())
}

// ToChannel converts store.Channel to catalog.Channel.
func (c Channel) ToChannel() catalog.Channel {
	disp := c.DisplayName
	if disp == "" {
		disp = c.Login
	}
	id := c.ID
	if id == "" {
		id = c.Login
	}
	return catalog.Channel{
		Platform:    c.CanonicalPlatform(),
		ID:          id,
		Login:       c.Login,
		DisplayName: disp,
		URL:         c.URL,
	}
}

// FromChannel converts catalog.Channel to store.Channel.
func FromChannel(ch catalog.Channel) Channel {
	disp := ch.DisplayName
	if disp == "" {
		disp = ch.Login
	}
	id := ch.ID
	if id == "" {
		id = ch.Login
	}
	plat := ch.Platform
	if plat == "" {
		plat = PlatformTwitch
	}
	return Channel{
		Platform:    plat,
		ID:          id,
		Login:       ch.Login,
		DisplayName: disp,
		URL:         ch.URL,
	}
}

// ToChannels converts a slice of store.Channel to []catalog.Channel.
func ToChannels(channels []Channel) []catalog.Channel {
	out := make([]catalog.Channel, len(channels))
	for i, c := range channels {
		out[i] = c.ToChannel()
	}
	return out
}

// FromChannels converts a slice of catalog.Channel to []store.Channel.
func FromChannels(channels []catalog.Channel) []Channel {
	out := make([]Channel, len(channels))
	for i, c := range channels {
		out[i] = FromChannel(c)
	}
	return out
}

// State represents the persistent application state containing favorites and recents.
type State struct {
	Favorites          []Channel  `json:"favorites"`
	FavoriteCategories []Category `json:"favorite_categories"`
	Recents            []Channel  `json:"recents"`
	// snapshot is local bookkeeping, never serialized. Copies share successful-save
	// acknowledgements because SaveTo accepts State by value.
	snapshot *favoriteSnapshot
	// discarded conta os canais que LoadFrom descartou por identificador fora
	// da gramática da plataforma. Também não é serializado.
	discarded int
}

// Discarded informa quantos recentes e favoritos o carregamento descartou por
// identificador inválido.
func (s State) Discarded() int {
	return s.discarded
}

type favoriteSnapshot struct {
	mu         sync.Mutex
	favorites  []Channel
	categories []Category
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
	var dropFav, dropRec int
	st.Favorites, dropFav = validChannels(normalizeChannels(st.Favorites))
	st.Recents, dropRec = validChannels(normalizeChannels(st.Recents))
	st.discarded = dropFav + dropRec
	st.FavoriteCategories = normalizeCategories(st.FavoriteCategories)
	st.snapshot = &favoriteSnapshot{favorites: append([]Channel(nil), st.Favorites...), categories: append([]Category(nil), st.FavoriteCategories...)}
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
		return mergeTo(path, st, maxRecents, st.Favorites, nil, st.FavoriteCategories, nil)
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
	if err := mergeTo(path, st, maxRecents, added, removed, withoutCategories(st.FavoriteCategories, snapshot.categories), withoutCategories(snapshot.categories, st.FavoriteCategories)); err != nil {
		return err
	}
	snapshot.favorites = append([]Channel(nil), st.Favorites...)
	snapshot.categories = append([]Category(nil), st.FavoriteCategories...)
	return nil
}

func mergeTo(path string, st State, maxRecents int, added, removed []Channel, addedCategories, removedCategories []Category) (err error) {
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
		addedCategories = st.FavoriteCategories
	}
	st.Favorites = unionChannels(withoutChannels(disk.Favorites, removed), added)
	st.FavoriteCategories = unionCategories(withoutCategories(disk.FavoriteCategories, removedCategories), addedCategories)
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

// validChannels descarta o canal cujo identificador foge da gramática da
// plataforma e devolve quantos saíram. Um login gravado com CR, LF, vírgula ou
// "#" viraria linha ou alvo extra no IRC ao ser reaberto. Maiúsculas ASCII
// passam, como em streamurl, porque quem consome o login já o põe em
// minúsculas; o canal fica como estava no arquivo.
func validChannels(channels []Channel) ([]Channel, int) {
	out := channels[:0]
	for _, ch := range channels {
		id := catalog.StableID(ch.Platform, ch.ID, ch.Login)
		if validStoredID(ch.Platform, platform.LowerASCII(id)) {
			out = append(out, ch)
		}
	}
	return out, len(channels) - len(out)
}

// validStoredID confere o identificador gravado de um canal. Na Kick o
// identificador pode levar subcaminho (xqc/videos/123, xqc/clip/abc), porque
// gravação e clip têm recentes próprios: o slug do canal segue a gramática e o
// resto não pode ter controle nem segmento "." ou "..".
func validStoredID(plat, id string) bool {
	if platform.Normalize(plat) != platform.Kick {
		return platform.ValidIdentifier(plat, id)
	}
	slug, rest, _ := strings.Cut(id, "/")
	if !platform.ValidKickSlug(slug) || platform.HasControl(rest) {
		return false
	}
	for _, seg := range strings.Split(rest, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
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

	var dropFav, dropRec int
	st.Favorites, dropFav = validChannels(normalizeChannels(st.Favorites))
	st.Recents, dropRec = validChannels(normalizeChannels(st.Recents))
	st.discarded = dropFav + dropRec

	st.FavoriteCategories = normalizeCategories(st.FavoriteCategories)
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
		s.snapshot.categories = append([]Category(nil), st.FavoriteCategories...)
	}
	return st, err
}

// Save saves state to the store's path.
func (s *Store) Save(st State) error {
	s.snapshot.mu.Lock()
	defer s.snapshot.mu.Unlock()
	return saveSnapshot(s.path, st, s.MaxRecents(), &s.snapshot)
}
