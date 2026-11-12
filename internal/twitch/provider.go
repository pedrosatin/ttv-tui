package twitch

import (
	"fmt"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/provider"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// Provider implements provider.Provider for the Twitch platform.
type Provider struct {
	source catalog.Source
}

// NewProvider returns a Twitch provider backed by the given catalog source.
func NewProvider(source catalog.Source) *Provider {
	return &Provider{source: source}
}

// ID returns the canonical platform identifier for Twitch ("twitch").
func (p *Provider) ID() string {
	return platform.Twitch
}

// Catalog returns the optional catalog Source for browsing Twitch categories and channels.
func (p *Provider) Catalog() (catalog.Source, bool) {
	return p.source, p.source != nil
}

// StreamTarget builds the playback Target for a Twitch channel.
func (p *Provider) StreamTarget(ch catalog.Channel) (player.Target, error) {
	login := ch.Login
	if login == "" && ch.URL != "" {
		if info, ok := streamurl.Parse(ch.URL); ok && info.Login != "" {
			login = info.Login
		}
	}

	if login == "" && ch.URL == "" {
		return player.Target{}, nil
	}

	url := ch.URL
	if url == "" {
		url = fmt.Sprintf("https://www.twitch.tv/%s", login)
	}

	label := login
	if label == "" {
		label = ch.PlayerLabel()
	}

	return player.Target{
		URL:   url,
		Label: label,
	}, nil
}

// Chat returns the chat interface for Twitch (placeholder for ticket 09).
func (p *Provider) Chat() (provider.Chat, bool) {
	return nil, false
}
