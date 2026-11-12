package provider

import (
	"errors"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// URLProvider implements Provider for stream URLs parsed via streamurl.
type URLProvider struct{}

// NewURLProvider returns a new generic URL provider.
func NewURLProvider() *URLProvider {
	return &URLProvider{}
}

// ID returns the canonical identifier for the generic URL provider.
func (p *URLProvider) ID() string {
	return "url"
}

// Catalog returns nil, false because generic stream URLs have no catalog.
func (p *URLProvider) Catalog() (catalog.Source, bool) {
	return nil, false
}

// StreamTarget resolves a playback target for a stream URL.
func (p *URLProvider) StreamTarget(ch catalog.Channel) (player.Target, error) {
	rawURL := strings.TrimSpace(ch.URL)
	if rawURL == "" {
		if ch.Login != "" {
			return player.Target{}, errors.New("missing URL for non-Twitch channel")
		}
		return player.Target{}, errors.New("missing URL")
	}

	info, ok := streamurl.Parse(rawURL)
	if !ok {
		label := ch.PlayerLabel()
		if label == "" {
			label = rawURL
		}
		return player.Target{
			URL:   rawURL,
			Label: label,
		}, nil
	}

	label := ch.PlayerLabel()
	if label == "" || label == rawURL {
		label = info.Label
	}
	if label == "" {
		label = info.URL
	}

	return player.Target{
		URL:   info.URL,
		Label: label,
	}, nil
}

// Chat returns nil, false because generic stream URLs have no chat support.
func (p *URLProvider) Chat() (Chat, bool) {
	return nil, false
}
