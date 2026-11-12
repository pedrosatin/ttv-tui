package catalog

import (
	"sort"
	"strings"
)

// PreferSearchChannels reorders Search hits for navigation without changing the
// Source contract (Search still returns Page[Channel]).
//
// Ordering rules:
//  1. exact login match first (case-insensitive, trimmed query and login),
//     even when that channel is offline and a clone is live;
//  2. then exact display name match, under the same conditions;
//  3. among the remaining results, live before offline/unknown;
//  4. otherwise keep the input order (upstream relevance).
func PreferSearchChannels(query string, items []Channel) []Channel {
	if len(items) < 2 {
		return items
	}
	q := strings.TrimSpace(query)
	out := append([]Channel(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		ei := exactChannelRank(out[i], q)
		ej := exactChannelRank(out[j], q)
		if ei != ej {
			return ei < ej
		}
		li := out[i].Availability == AvailabilityLive
		lj := out[j].Availability == AvailabilityLive
		if li != lj {
			return li
		}
		return false
	})
	return out
}

// PreferSearchCategories reorders SearchCategories hits for navigation.
//
// Ordering rules:
//  1. exact name match first (case-insensitive, trimmed query and name);
//  2. among the rest, higher viewersCount first;
//  3. nil or zero viewersCount last (shown as "no broadcast" in the UI).
func PreferSearchCategories(query string, items []Category) []Category {
	if len(items) < 2 {
		return items
	}
	q := strings.TrimSpace(query)
	out := append([]Category(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		ei := nameEqualsQuery(out[i].Name, q)
		ej := nameEqualsQuery(out[j].Name, q)
		if ei != ej {
			return ei
		}
		hi, vi := categoryHasViewers(out[i])
		hj, vj := categoryHasViewers(out[j])
		if hi != hj {
			return hi
		}
		if hi && hj && vi != vj {
			return vi > vj
		}
		return false
	})
	return out
}

// ExactChannelMatch reports whether query names ch exactly, by login or display
// name (case-insensitive, trimmed). The UI lists such a channel ahead of the
// category hits, because the user typed the channel's own name.
func ExactChannelMatch(query string, ch Channel) bool {
	return exactChannelRank(ch, strings.TrimSpace(query)) != exactRankNone
}

const (
	exactRankLogin = iota
	exactRankDisplayName
	exactRankNone
)

func exactChannelRank(ch Channel, query string) int {
	switch {
	case loginEqualsQuery(ch.Login, query):
		return exactRankLogin
	case nameEqualsQuery(ch.DisplayName, query):
		return exactRankDisplayName
	default:
		return exactRankNone
	}
}

func loginEqualsQuery(login, query string) bool {
	if query == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(login), query)
}

func nameEqualsQuery(name, query string) bool {
	if query == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(name), query)
}

// categoryHasViewers reports whether c has a positive viewer count. Nil and
// zero both count as "no broadcast" for ranking and display.
func categoryHasViewers(c Category) (ok bool, n int) {
	if c.Viewers == nil || *c.Viewers <= 0 {
		return false, 0
	}
	return true, *c.Viewers
}
