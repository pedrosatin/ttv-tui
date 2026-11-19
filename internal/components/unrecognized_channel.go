package components

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// UnrecognizedChannelKey returns the opaque identity stored in
// trial_unrecognized_channels for raw (session login, label or URL). It is the
// lowercase hex SHA-256 of a canonical channel key and never embeds a URL,
// userinfo or query string.
func UnrecognizedChannelKey(raw string) string {
	sum := sha256.Sum256([]byte(canonicalizeUnrecognizedChannel(raw)))
	return hex.EncodeToString(sum[:])
}

// canonicalizeUnrecognizedChannel maps logins, labels and URLs of the same
// channel to one key before hashing. Twitch, Kick and generic hosts are
// case-insensitive; YouTube video IDs keep their case; distinct platforms with
// the same name stay separate.
func canonicalizeUnrecognizedChannel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "?"
	}
	if info, ok := streamurl.Parse(raw); ok {
		return canonicalFromInfo(info.Platform, info.Login)
	}
	if cleaned, ok := stripURLSecrets(raw); ok {
		if info, ok := streamurl.Parse(cleaned); ok {
			return canonicalFromInfo(info.Platform, info.Login)
		}
		if u, err := url.Parse(cleaned); err == nil && u.Host != "" {
			host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
			path := strings.ToLower(strings.Trim(u.EscapedPath(), "/"))
			if path == "" {
				return host
			}
			return host + ":" + path
		}
	}
	if plat, id, ok := splitPrefixedIdentity(raw); ok {
		return canonicalFromInfo(plat, id)
	}
	lower := platform.LowerASCII(raw)
	if platform.ValidTwitchLogin(lower) {
		return platform.Twitch + ":" + lower
	}
	return strings.ToLower(raw)
}

func canonicalFromInfo(plat, id string) string {
	plat = strings.TrimSpace(plat)
	id = strings.TrimSpace(id)
	switch platform.Normalize(plat) {
	case platform.Twitch:
		plat = platform.Twitch
		id = platform.LowerASCII(id)
	case platform.Kick:
		plat = platform.Kick
		id = platform.LowerASCII(id)
	case platform.YouTube:
		plat = platform.YouTube
		if !platform.ValidYouTubeVideoID(id) {
			id = platform.LowerASCII(id)
		}
	default:
		plat = strings.TrimPrefix(strings.ToLower(plat), "www.")
		id = strings.ToLower(id)
	}
	if id == "" {
		return plat
	}
	return plat + ":" + id
}

// splitPrefixedIdentity reads "kick:xqc" / "youtube:ID" style labels. It
// refuses http(s) schemes so URLs stay on the Parse / stripURLSecrets path.
func splitPrefixedIdentity(raw string) (plat, id string, ok bool) {
	i := strings.IndexByte(raw, ':')
	if i <= 0 || i == len(raw)-1 {
		return "", "", false
	}
	plat = raw[:i]
	id = raw[i+1:]
	if strings.Contains(plat, "/") || strings.EqualFold(plat, "http") || strings.EqualFold(plat, "https") {
		return "", "", false
	}
	return plat, id, true
}

// stripURLSecrets drops userinfo, query and fragment so a pasted URL with
// credentials or tokens can still yield a channel key without retaining them.
func stripURLSecrets(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.Scheme + "://" + u.Host + u.EscapedPath(), true
}

// isUnrecognizedChannelKey reports whether entry has the shape
// UnrecognizedChannelKey produces: 64 lowercase hexadecimal characters.
func isUnrecognizedChannelKey(entry string) bool {
	if len(entry) != 64 {
		return false
	}
	for _, r := range entry {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
