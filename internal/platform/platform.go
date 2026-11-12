package platform

import "strings"

// Platform constants supported by ttv-tui.
const (
	Twitch  = "twitch"
	Kick    = "kick"
	YouTube = "youtube"
	Default = Twitch
)

// Normalize returns the trimmed, lowercase platform name, or Default when empty.
func Normalize(raw string) string {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return Default
	}
	return p
}

// IsTwitch reports whether raw platform normalizes to Twitch.
func IsTwitch(raw string) bool {
	return Normalize(raw) == Twitch
}

// IsNonTwitch reports whether raw platform is non-empty and not Twitch.
func IsNonTwitch(raw string) bool {
	return Normalize(raw) != Twitch
}
