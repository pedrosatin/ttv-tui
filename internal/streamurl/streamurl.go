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
	"dashboard":     true,
	"bits":          true,
	"following":     true,
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
	// A checagem de cima vê só o texto colado; %0D%0A, %00 e %1b só aparecem
	// depois de decodificar, e é o valor decodificado que vira Login e Label.
	query := u.Query()
	if decodedHasControl(u.Path, query) {
		return Info{}, false
	}

	switch host {
	case "twitch.tv", "www.twitch.tv", "m.twitch.tv":
		cleanPath := strings.Trim(u.Path, "/")
		if cleanPath == "" {
			return Info{}, false
		}
		parts := strings.Split(cleanPath, "/")
		first := platform.LowerASCII(parts[0])
		if isTwitchReserved(first) {
			return Info{}, false
		}
		if !platform.ValidTwitchLogin(first) {
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
		// The whole path is the identity: kick.com/xqc is the live channel, while
		// kick.com/xqc/videos/<id> and kick.com/xqc/clips/<id> are other streams
		// that must not share its recents entry or player session.
		segs := pathSegments(u.Path)
		if len(segs) == 0 {
			return Info{}, false
		}
		channel := platform.LowerASCII(segs[0])
		if !platform.ValidKickSlug(channel) {
			return Info{}, false
		}
		for _, seg := range segs[1:] {
			if seg == "." || seg == ".." {
				return Info{}, false
			}
		}
		id := strings.ToLower(strings.Join(segs, "/"))
		if clip := query.Get("clip"); clip != "" {
			id += "/clip/" + strings.ToLower(clip)
		}
		return Info{
			URL:         raw,
			Platform:    platform.Kick,
			Login:       id,
			DisplayName: channel,
			Label:       fmt.Sprintf("%s:%s", platform.Kick, id),
			IsTwitch:    false,
		}, true

	case "youtube.com", "www.youtube.com", "m.youtube.com", "youtu.be":
		var slug string
		segs := pathSegments(u.Path)
		if host == "youtu.be" {
			if len(segs) != 1 || !validYouTubeVideoID(segs[0]) {
				return Info{}, false
			}
			slug = segs[0]
		} else {
			if len(segs) == 0 {
				return Info{}, false
			}
			switch {
			case strings.EqualFold(segs[0], "watch"):
				v := query.Get("v")
				if !validYouTubeVideoID(v) {
					return Info{}, false
				}
				slug = v
			case strings.EqualFold(segs[0], "live"):
				if len(segs) < 2 || !validYouTubeVideoID(segs[1]) {
					return Info{}, false
				}
				slug = segs[1]
			case strings.HasPrefix(segs[0], "@"):
				if !validYouTubeHandle(segs[0]) {
					return Info{}, false
				}
				if len(segs) > 1 && (len(segs) != 2 || !strings.EqualFold(segs[1], "live")) {
					return Info{}, false
				}
				slug = segs[0]
			default:
				v := query.Get("v")
				if !validYouTubeVideoID(v) {
					return Info{}, false
				}
				slug = v
			}
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
		// An unknown site gives no channel concept, so the identity is the "v"
		// query parameter when present (facebook.com/watch/live/?v=<id>) or else
		// the whole path. Using only the last segment made different streams
		// such as two ".../watch/live/?v=" links collide.
		genericPlat := strings.TrimPrefix(host, "www.")
		id := query.Get("v")
		if id == "" {
			id = strings.Join(pathSegments(u.Path), "/")
		}
		if id == "" {
			id = genericPlat
		}
		display := cleanSlug(u.Path)
		if display == "" {
			display = id
		}
		return Info{
			URL:         raw,
			Platform:    genericPlat,
			Login:       id,
			DisplayName: display,
			Label:       fmt.Sprintf("%s:%s", genericPlat, id),
			IsTwitch:    false,
		}, true
	}
}

// ParseInput parses a pasted stream URL (via Parse) or a bare YouTube @channel
// handle. Leading/trailing spaces are NOT trimmed; callers normalize whitespace
// before invoking it, matching Parse.
func ParseInput(raw string) (Info, bool) {
	if info, ok := Parse(raw); ok {
		return info, true
	}
	if validYouTubeHandle(raw) {
		return Info{
			URL:         "https://www.youtube.com/" + raw + "/live",
			Platform:    platform.YouTube,
			Login:       raw,
			DisplayName: raw,
			Label:       fmt.Sprintf("%s:%s", platform.YouTube, raw),
			IsTwitch:    false,
		}, true
	}
	return Info{}, false
}

// IsStreamURL reports whether raw is a valid http(s) URL with a host.
func IsStreamURL(raw string) bool {
	_, ok := Parse(raw)
	return ok
}

func validYouTubeHandle(h string) bool {
	if !strings.HasPrefix(h, "@") || len(h) < 2 || len(h) > 100 {
		return false
	}
	rest := h[1:]
	if rest == "." || rest == ".." || strings.Contains(rest, "..") {
		return false
	}
	if strings.ContainsAny(h, " \t\r\n\v\f") || platform.HasControl(h) {
		return false
	}
	return !strings.ContainsAny(rest, "/\\?#:@%")
}

func validYouTubeVideoID(id string) bool {
	if id == "" || len(id) > 64 || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, " \t\r\n\v\f") || platform.HasControl(id) {
		return false
	}
	return !strings.ContainsAny(id, "/\\?#:@%&=.")
}

// decodedHasControl informa se o caminho ou algum parâmetro da query, já
// decodificados, têm caractere de controle.
func decodedHasControl(path string, query url.Values) bool {
	if platform.HasControl(path) {
		return true
	}
	for k, vs := range query {
		if platform.HasControl(k) {
			return true
		}
		for _, v := range vs {
			if platform.HasControl(v) {
				return true
			}
		}
	}
	return false
}

// pathSegments returns the non-empty segments of a URL path.
func pathSegments(path string) []string {
	var out []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
