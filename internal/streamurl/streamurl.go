package streamurl

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Info holds the classified metadata from a parsed stream URL.
type Info struct {
	URL         string // Full original URL
	Platform    string // Canonical platform (twitch, kick, youtube, or host)
	Login       string // Channel login/slug
	DisplayName string // Display name
	Label       string // Player session label (e.g. "xqc" or "kick:xqc")
	IsTwitch    bool   // True if it points to a Twitch channel
}

var twitchReservedPaths = map[string]bool{
	"directory":     true,
	"videos":        true,
	"downloads":     true,
	"settings":      true,
	"search":        true,
	"subscriptions": true,
	"inventory":     true,
	"wallet":        true,
	"drops":         true,
	"p":             true,
	"jobs":          true,
	"turbo":         true,
	"prime":         true,
	"login":         true,
	"signup":        true,
	"broadcast":     true,
	"user":          true,
	"team":          true,
	"popout":        true,
	"moderator":     true,
	"event":         true,
	"manager":       true,
	"friends":       true,
	"messages":      true,
}

func isTwitchReserved(segment string) bool {
	return twitchReservedPaths[strings.ToLower(segment)]
}

// Parse inspects raw input. If it is an http(s) URL with a host, it extracts
// the streaming platform, channel slug, and qualified player label.
// If raw is not an http(s) URL with a host, ok is false.
func Parse(raw string) (Info, bool) {
	if raw == "" {
		return Info{}, false
	}
	// Reject URLs with whitespace or control characters.
	if strings.ContainsAny(raw, " \t\r\n\v\f") {
		return Info{}, false
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return Info{}, false
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Info{}, false
	}
	// Refuse URLs with user credentials (user:pw@)
	if u.User != nil {
		return Info{}, false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return Info{}, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Info{}, false
	}

	switch host {
	case "twitch.tv", "www.twitch.tv", "m.twitch.tv":
		cleanPath := strings.Trim(u.Path, "/")
		if cleanPath == "" {
			return Info{}, false
		}
		parts := strings.Split(cleanPath, "/")
		first := strings.ToLower(parts[0])
		if isTwitchReserved(first) {
			return Info{}, false
		}
		login := first
		return Info{
			URL:         raw,
			Platform:    platform.Twitch,
			Login:       login,
			DisplayName: login,
			Label:       login,
			IsTwitch:    true,
		}, true

	case "kick.com", "www.kick.com", "m.kick.com":
		slug := cleanSlug(u.Path)
		if slug == "" {
			return Info{}, false
		}
		slug = strings.ToLower(slug)
		return Info{
			URL:         raw,
			Platform:    platform.Kick,
			Login:       slug,
			DisplayName: slug,
			Label:       fmt.Sprintf("%s:%s", platform.Kick, slug),
			IsTwitch:    false,
		}, true

	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be":
		var slug string
		if host == "youtu.be" {
			slug = strings.Trim(u.Path, "/")
		} else {
			cleanPath := strings.Trim(u.Path, "/")
			parts := strings.Split(cleanPath, "/")
			if len(parts) > 0 && strings.EqualFold(parts[0], "watch") {
				v := u.Query().Get("v")
				if v == "" {
					return Info{}, false
				}
				slug = v
			} else if v := u.Query().Get("v"); v != "" {
				slug = v
			} else {
				slug = cleanSlug(u.Path)
			}
		}
		if slug == "" || strings.EqualFold(slug, "watch") {
			return Info{}, false
		}
		return Info{
			URL:         raw,
			Platform:    platform.YouTube,
			Login:       slug,
			DisplayName: slug,
			Label:       fmt.Sprintf("%s:%s", platform.YouTube, slug),
			IsTwitch:    false,
		}, true

	default:
		genericPlat := strings.TrimPrefix(host, "www.")
		slug := cleanSlug(u.Path)
		if slug == "" {
			slug = genericPlat
		}
		return Info{
			URL:         raw,
			Platform:    genericPlat,
			Login:       slug,
			DisplayName: slug,
			Label:       fmt.Sprintf("%s:%s", genericPlat, slug),
			IsTwitch:    false,
		}, true
	}
}

// IsStreamURL reports whether raw is a valid http(s) URL with a host.
func IsStreamURL(raw string) bool {
	_, ok := Parse(raw)
	return ok
}

// cleanSlug extracts a single identifier from a URL path.
func cleanSlug(path string) string {
	clean := strings.Trim(path, "/")
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, "/")
	// If path starts with an @ handle (e.g. /@channel/live), keep the handle.
	if strings.HasPrefix(parts[0], "@") {
		return parts[0]
	}
	// Prefer the last non-empty segment (e.g. /video/123 or /live/slug).
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p != "" && !strings.EqualFold(p, "live") && !strings.EqualFold(p, "watch") {
			return p
		}
	}
	return parts[0]
}
