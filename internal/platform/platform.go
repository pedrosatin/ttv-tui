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

// DisplayName returns the name a person reads for raw platform: the brand for
// the known ones, and the normalized text (a host such as "vimeo.com") for the rest.
func DisplayName(raw string) string {
	switch p := Normalize(raw); p {
	case Twitch:
		return "Twitch"
	case Kick:
		return "Kick"
	case YouTube:
		return "YouTube"
	default:
		return p
	}
}
