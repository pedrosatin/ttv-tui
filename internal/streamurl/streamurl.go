package streamurl

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/store"
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

// Parse inspects raw input. If it is an http(s) URL with a host, it extracts
// the streaming platform, channel slug, and qualified player label.
// If raw is not an http(s) URL with a host, ok is false.
func Parse(raw string) (Info, bool) {
	if raw == "" {
		return Info{}, false
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Info{}, false
	}
	if strings.IndexFunc(trimmed, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return Info{}, false
	}

	u, err := url.Parse(trimmed)
	if err != nil {
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
		if cleanPath == "" || strings.Contains(cleanPath, "/") {
			// Bare home page or sub-pages (e.g. /directory/game) are not channel URLs.
			return Info{}, false
		}
		login := strings.ToLower(cleanPath)
		return Info{
			URL:         trimmed,
			Platform:    store.PlatformTwitch,
			Login:       login,
			DisplayName: login,
			Label:       login,
			IsTwitch:    true,
		}, true

	case "kick.com", "www.kick.com":
		slug := cleanSlug(u.Path)
		if slug == "" {
			slug = store.PlatformKick
		}
		return Info{
			URL:         trimmed,
			Platform:    store.PlatformKick,
			Login:       slug,
			DisplayName: slug,
			Label:       fmt.Sprintf("%s:%s", store.PlatformKick, slug),
			IsTwitch:    false,
		}, true

	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be":
		var slug string
		if host == "youtu.be" {
			slug = strings.Trim(u.Path, "/")
		} else if v := u.Query().Get("v"); v != "" {
			slug = v
		} else {
			slug = cleanSlug(u.Path)
		}
		if slug == "" {
			slug = store.PlatformYouTube
		}
		return Info{
			URL:         trimmed,
			Platform:    store.PlatformYouTube,
			Login:       slug,
			DisplayName: slug,
			Label:       fmt.Sprintf("%s:%s", store.PlatformYouTube, slug),
			IsTwitch:    false,
		}, true

	default:
		genericPlat := strings.TrimPrefix(host, "www.")
		slug := cleanSlug(u.Path)
		if slug == "" {
			slug = genericPlat
		}
		return Info{
			URL:         trimmed,
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
