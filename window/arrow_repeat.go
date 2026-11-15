package main

import "time"

// SDL can deliver catch-up repeats together when a held key is released.
// Bound delivery by arrival time so a burst cannot jump across the list.
// Physical key presses always pass, even when faster than this interval.
type arrowRepeatGate struct {
	key  int64
	last time.Time
}

func (g *arrowRepeatGate) accept(key int64, repeat bool, now time.Time) bool {
	if repeat && key == g.key && now.Sub(g.last) < 8*time.Millisecond {
		return false
	}
	g.key, g.last = key, now
	return true
}

func (g *arrowRepeatGate) release(key int64) {
	if key == g.key {
		*g = arrowRepeatGate{}
	}
}
