package twitch

import "context"

// Category represents a live Twitch category (game or topic) with a stable ID.
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

// Channel represents a Twitch channel with stable identity fields.
// Viewers, Title, and CategoryID are optional metadata. Availability is
// AvailabilityUnknown unless the source had evidence of the stream state.
type Channel struct {
	ID           string
	Login        string
	DisplayName  string
	Viewers      *int
	Title        *string
	CategoryID   *string
	Availability Availability
}

// PageRequest describes the pagination parameters for a Source query.
type PageRequest struct {
	Cursor string
	Limit  int
}

// Page holds a single page of results from a Source along with pagination state.
type Page[T any] struct {
	Items   []T
	Cursor  string
	HasMore bool
}

// Source defines the data contract consumed by the UI for browsing categories,
// channels, and search results without exposing transport or wire details.
//
// ChannelsByLogins is a batch availability lookup for saved channels
// (favorites/recents). It does not replace Search, Channels, or Categories.
type Source interface {
	Categories(ctx context.Context, page PageRequest) (Page[Category], error)
	Channels(ctx context.Context, categoryID string, page PageRequest) (Page[Channel], error)
	Search(ctx context.Context, query string) (Page[Channel], error)
	// ChannelsByLogins returns identity and Availability for the given logins
	// in a single upstream request. Empty logins yields an empty result with
	// no network call. Logins that do not resolve are omitted so callers keep
	// AvailabilityUnknown. stream:null is AvailabilityOffline; a stream object
	// is AvailabilityLive with viewers/title when present.
	ChannelsByLogins(ctx context.Context, logins []string) ([]Channel, error)
}

// IntPtr returns a pointer to v.
func IntPtr(v int) *int {
	return &v
}

// StringPtr returns a pointer to s.
func StringPtr(s string) *string {
	return &s
}
