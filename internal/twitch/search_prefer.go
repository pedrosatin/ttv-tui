package twitch

import (
	"sort"
	"strings"
)

// PreferSearchChannels reorders Search hits for navigation without changing the
// Source contract (Search still returns Page[Channel]).
//
// Chosen rule (v1.2 ticket 01):
//  1. exact login match first (case-insensitive, trimmed query and login),
//     even when that channel is offline and a clone is live;
//  2. among the remaining results, live before offline/unknown;
//  3. otherwise keep the input order (Twitch relevance).
//
// Applied by GQLSource.Search before caching/returning, and again by the UI when
// it handles SearchLoadedMsg, so Enter and settleHighlightAfterLoad see the
// preferred order for every Source.
func PreferSearchChannels(query string, items []Channel) []Channel {
	if len(items) < 2 {
		return items
	}
	q := strings.TrimSpace(query)
	out := append([]Channel(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		ei := loginEqualsQuery(out[i].Login, q)
		ej := loginEqualsQuery(out[j].Login, q)
		if ei != ej {
			return ei
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

func loginEqualsQuery(login, query string) bool {
	if query == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(login), query)
}
