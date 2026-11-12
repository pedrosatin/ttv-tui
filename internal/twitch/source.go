package twitch

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Category is an alias for catalog.Category.
type Category = catalog.Category

// Availability is an alias for catalog.Availability.
type Availability = catalog.Availability

const (
	// AvailabilityUnknown means no evidence either way.
	AvailabilityUnknown = catalog.AvailabilityUnknown
	// AvailabilityLive means the source reported an active stream.
	AvailabilityLive = catalog.AvailabilityLive
	// AvailabilityOffline means the source explicitly reported no active stream.
	AvailabilityOffline = catalog.AvailabilityOffline
)

// Channel is an alias for catalog.Channel.
type Channel = catalog.Channel

// PageRequest is an alias for catalog.PageRequest.
type PageRequest = catalog.PageRequest

// Page is an alias for catalog.Page.
type Page[T any] = catalog.Page[T]

// Source is an alias for catalog.Source.
type Source = catalog.Source

// Browser is an alias for catalog.Browser.
type Browser = catalog.Browser

// Searcher is an alias for catalog.Searcher.
type Searcher = catalog.Searcher

// Resolver is an alias for catalog.Resolver.
type Resolver = catalog.Resolver

// Stream is an alias for catalog.Stream.
type Stream = catalog.Stream

// Chat is an alias for catalog.Chat.
type Chat = catalog.Chat

// NormalizeLanguage trims and lowercases a Twitch stream language code
// (API returns "PT", "EN", "ES"). Empty input yields "".
func NormalizeLanguage(raw string) string {
	return catalog.NormalizeLanguage(raw)
}

// LanguagePtr normalizes raw and returns a pointer, or nil when empty.
func LanguagePtr(raw string) *string {
	return catalog.LanguagePtr(raw)
}

// IntPtr returns a pointer to v.
func IntPtr(v int) *int {
	return catalog.IntPtr(v)
}

// StringPtr returns a pointer to s.
func StringPtr(s string) *string {
	return catalog.StringPtr(s)
}
