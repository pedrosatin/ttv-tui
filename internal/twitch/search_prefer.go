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

// PreferSearchCategories reorders SearchCategories hits for navigation.
//
// Chosen rule (v1.4 ticket 09):
//  1. exact name match first (case-insensitive, trimmed query and name);
//  2. among the rest, higher viewersCount first;
//  3. nil or zero viewersCount last (shown as "no broadcast" in the UI).
//
// Applied by GQLSource.SearchCategories before caching/returning, and again by
// the UI when it handles SearchLoadedMsg.
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
