package player

import "strings"

// adSignal is what one streamlink log line proves about the ad hold.
type adSignal int

const (
	adSignalNone adSignal = iota
	// adSignalHoldStart: streamlink stopped writing video because it is
	// discarding ad segments.
	adSignalHoldStart
	// adSignalHoldEnd: streamlink wrote real content again and resumed output.
	adSignalHoldEnd
)

// classifyAdLine maps a streamlink log line to verifiable ad-hold evidence.
//
// Only the paired lines from streamlink's HLS writer count (streamlink 8.x,
// where the Twitch plugin filters exactly the ad segments):
//   - "Filtering out segments and pausing stream output" (start)
//   - "Resuming stream output" (end)
//
// Informational lines such as "Detected advertisement break of N seconds" or
// "Waiting for pre-roll ads to finish" have no matching end line, so on their
// own they never put the player in the interval state.
func classifyAdLine(line string) adSignal {
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "filtering out segments and pausing stream output"):
		return adSignalHoldStart
	case strings.Contains(l, "resuming stream output"):
		return adSignalHoldEnd
	default:
		return adSignalNone
	}
}
