package twitch

import "github.com/pedrosatin/ttv-tui/internal/catalog"

// PreferSearchChannels reorders Search hits for navigation without changing the
// Source contract (Search still returns Page[Channel]).
func PreferSearchChannels(query string, items []catalog.Channel) []catalog.Channel {
	return catalog.PreferSearchChannels(query, items)
}

// PreferSearchCategories reorders SearchCategories hits for navigation.
func PreferSearchCategories(query string, items []catalog.Category) []catalog.Category {
	return catalog.PreferSearchCategories(query, items)
}

// ExactChannelMatch reports whether query names ch exactly, by login or display name.
func ExactChannelMatch(query string, ch catalog.Channel) bool {
	return catalog.ExactChannelMatch(query, ch)
}
