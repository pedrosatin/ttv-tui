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
	// adSignalUnknown: the line is about pausing or resuming stream output, in
	// words neither known line uses. A reworded end line would otherwise keep
	// the interval up until BreakMaxHold.
	adSignalUnknown
)

// The two lines that streamlink's HLS writer prints at --loglevel info,
// whole (streamlink 8.x, logger "streamlink.stream.hls").
const (
	adHoldStartLine = "[stream.hls][info] Filtering out segments and pausing stream output"
	adHoldEndLine   = "[stream.hls][info] Resuming stream output"
)

// classifyAdLine maps a streamlink log line to verifiable ad-hold evidence.
//
// Only the paired lines from streamlink's HLS writer count (streamlink 8.x,
// where the Twitch plugin filters exactly the ad segments):
//   - "Filtering out segments and pausing stream output" (start)
//   - "Resuming stream output" (end)
//
// Cada uma só vale como a linha inteira, com o prefixo "[stream.hls][info]": o
// mesmo texto dentro de outra linha (o nome de um stream, a reason phrase de um
// servidor) é texto de terceiros e não abre nem fecha o intervalo.
//
// Informational lines such as "Detected advertisement break of N seconds" or
// "Waiting for pre-roll ads to finish" have no matching end line, so on their
// own they never put the player in the interval state. Any other line that
// mentions stream output or filtering out segments is adSignalUnknown.
func classifyAdLine(line string) adSignal {
	trimmed := strings.TrimRight(line, " \r")
	switch trimmed {
	case adHoldStartLine:
		return adSignalHoldStart
	case adHoldEndLine:
		return adSignalHoldEnd
	}
	l := strings.ToLower(line)
	switch {
	case strings.Contains(l, "stream output"), strings.Contains(l, "filtering out segments"):
		return adSignalUnknown
	default:
		return adSignalNone
	}
}
