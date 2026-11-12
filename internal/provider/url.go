package provider

import (
	"errors"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
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

// Browser returns nil, false because generic stream URLs have no browsing capability.
func (p *URLProvider) Browser() (catalog.Browser, bool) {
	return nil, false
}

// Searcher returns nil, false because generic stream URLs have no search capability.
func (p *URLProvider) Searcher() (catalog.Searcher, bool) {
	return nil, false
}

// Resolver returns nil, false because generic stream URLs have no resolver capability.
func (p *URLProvider) Resolver() (catalog.Resolver, bool) {
	return nil, false
}

// StreamTarget resolves a playback target for a stream URL.
func (p *URLProvider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	rawURL := strings.TrimSpace(ch.URL)
	if rawURL == "" {
		return catalog.Stream{}, errors.New("missing URL")
	}

	info, ok := streamurl.Parse(rawURL)
	if !ok {
		label := ch.PlayerLabel()
		if label == "" {
			label = rawURL
		}
		return catalog.Stream{
			URL:   rawURL,
			Label: label,
		}, nil
	}

	label := ch.PlayerLabel()
	plat := platform.Normalize(ch.Platform)
	if label == "" || label == rawURL || (plat != "" && label == plat) {
		label = info.Label
	}
	if label == "" {
		label = info.URL
	}

	return catalog.Stream{
		URL:   info.URL,
		Label: label,
	}, nil
}

// Chat returns nil, false because generic stream URLs have no chat support.
func (p *URLProvider) Chat() (catalog.Chat, bool) {
	return nil, false
}
