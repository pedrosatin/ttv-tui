package catalog

import (
	"context"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Category represents a live category (game or topic) with a stable ID.
// Viewers is optional because not every upstream adapter provides viewer counts.
type Category struct {
	ID      string
	Name    string
	Viewers *int
}

// Availability records what a Source actually knows about whether a channel is
// broadcasting. The zero value is AvailabilityUnknown: a missing viewer count or
// a channel known only by identity (favorites, recents) is never offline.
type Availability int

const (
	// AvailabilityUnknown means no evidence either way.
	AvailabilityUnknown Availability = iota
	// AvailabilityLive means the source reported an active stream.
	AvailabilityLive
	// AvailabilityOffline means the source explicitly reported no active stream.
	AvailabilityOffline
)

// Channel represents a streaming channel with stable identity fields.
// Viewers, Title, CategoryID, CategoryName, and Language are optional metadata.
// Availability is AvailabilityUnknown unless the source had evidence of the
// stream state. CategoryName is the display name of the game/topic the
// channel is (or was) streaming; when unknown the status bar omits it.
// Language is the stream language code normalized to lowercase (pt, en, es);
// nil or empty means the source did not report one.
type Channel struct {
	ID           string
	Login        string
	DisplayName  string
	Viewers      *int
	Title        *string
	CategoryID   *string
	CategoryName *string
	Language     *string
	Availability Availability
	Platform     string
	URL          string
}

// PlayerLabel returns the qualified session label for playback: platform:login
// when platform is not Twitch and non-empty, otherwise login.
func (c Channel) PlayerLabel() string {
	plat := platform.Normalize(c.Platform)
	if plat != platform.Twitch {
		if c.Login != "" {
			prefix := plat + ":"
			if strings.HasPrefix(strings.ToLower(c.Login), prefix) {
				return c.Login
			}
			return prefix + c.Login
		}
		return plat
	}
	return c.Login
}

// CanonicalPlatform returns the normalized canonical platform name for c.
func (c Channel) CanonicalPlatform() string {
	return platform.Normalize(c.Platform)
}

// StableID returns the stable channel identifier within its platform
// (the login/slug that forms the PlayerLabel).
func (c Channel) StableID() string {
	return StableID(c.Platform, c.ID, c.Login)
}

// Matches reports whether c and other represent the same channel on the same platform.
func (c Channel) Matches(other Channel) bool {
	return SameChannel(c, other)
}

// StableID returns the stable channel identifier within plat
// (the login or the slug extracted from id).
func StableID(plat, id, login string) string {
	canonical := platform.Normalize(plat)
	prefix := canonical + ":"
	if login != "" {
		if strings.HasPrefix(strings.ToLower(login), prefix) {
			return login[len(prefix):]
		}
		return login
	}
	if strings.HasPrefix(strings.ToLower(id), prefix) {
		return id[len(prefix):]
	}
	return id
}

// SameChannel reports whether two channels represent the same channel on the same platform.
// For YouTube, channel identifiers are case-sensitive; for Twitch and other platforms,
// comparison is case-insensitive.
func SameChannel(a, b Channel) bool {
	platA := platform.Normalize(a.Platform)
	platB := platform.Normalize(b.Platform)
	if platA != platB {
		return false
	}
	if platA != platform.Twitch {
		idA := a.StableID()
		idB := b.StableID()
		if idA == "" || idB == "" {
			return false
		}
		if platA == platform.YouTube {
			return idA == idB
		}
		return strings.EqualFold(idA, idB)
	}
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return true
	}
	return a.Login != "" && strings.EqualFold(a.Login, b.Login)
}

// NormalizeLanguage trims and lowercases a stream language code
// (API returns "PT", "EN", "ES"). Empty input yields "".
func NormalizeLanguage(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// LanguagePtr normalizes raw and returns a pointer, or nil when empty.
func LanguagePtr(raw string) *string {
	norm := NormalizeLanguage(raw)
	if norm == "" {
		return nil
	}
	return &norm
}

// PageRequest describes the pagination parameters for a Source query.
type PageRequest struct {
	// Languages pede só canais desses idiomas, em código normalizado ("pt").
	// Um catálogo sem filtro no servidor ignora o campo, e a UI filtra de novo. Vazio pede todos.
	Languages []string
	Cursor    string
	Limit     int
}

// Page holds a single page of results from a Source along with pagination state.
type Page[T any] struct {
	Items   []T
	Cursor  string
	HasMore bool
}

// Stream represents a neutral playback target: the stream URL, optional streamlink CLI
// arguments, optional streamlink plugins to sideload, and an optional
// human-readable label/identity for the session.
type Stream struct {
	URL       string
	ExtraArgs []string
	// Plugins are written to a private directory of the session and handed to
	// streamlink with --plugin-dir, for this playback only.
	Plugins []StreamlinkPlugin
	Label   string
}

// StreamlinkPlugin is the source of a streamlink plugin module. A Name equal to
// a built-in plugin's ("kick") replaces that plugin for the playback.
type StreamlinkPlugin struct {
	// Name is the module name, without ".py": lowercase letters, digits and
	// underscores, starting with a letter.
	Name   string
	Source []byte
}

// Identity returns the session identity: Label when non-empty, otherwise URL.
func (s Stream) Identity() string {
	if s.Label != "" {
		return s.Label
	}
	return s.URL
}

// Browser supports browsing categories and channels within a category, as well as searching categories.
type Browser interface {
	Categories(ctx context.Context, page PageRequest) (Page[Category], error)
	Channels(ctx context.Context, categoryID string, page PageRequest) (Page[Channel], error)
	// SearchCategories finds categories by name via the global search API.
	// Unlike Categories (top live page), it reaches games outside the top N.
	// Empty query yields an empty slice with no network call. Results are not
	// paginated: Twitch returns one relevance window. PreferSearchCategories
	// reorders hits (exact name, then viewers) before return.
	SearchCategories(ctx context.Context, query string) ([]Category, error)
}

// Searcher supports searching live channels.
type Searcher interface {
	Search(ctx context.Context, query string) (Page[Channel], error)
}

// Resolver resolves the current stream status/metadata for saved channels.
type Resolver interface {
	// ChannelsByIDs returns identity and Availability for the given identifiers,
	// in as many upstream batches as needed. Empty ids yields an empty result with
	// no network call. Identifiers that do not resolve are omitted so callers keep
	// AvailabilityUnknown. stream:null without an error is AvailabilityOffline.
	// A null stream with an error on the same item stays AvailabilityUnknown;
	// it is not evidence of offline. A stream object is AvailabilityLive with
	// viewers/title when present.
	ChannelsByIDs(ctx context.Context, ids []string) ([]Channel, error)
}

// Source defines the complete catalog data contract consumed by the UI,
// combining Browser, Searcher, and Resolver capabilities.
type Source interface {
	Browser
	Searcher
	Resolver
}

// IntPtr returns a pointer to v.
func IntPtr(v int) *int {
	return &v
}

// StringPtr returns a pointer to s.
func StringPtr(s string) *string {
	return &s
}
