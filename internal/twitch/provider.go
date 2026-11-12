package twitch

import (
	"errors"
	"fmt"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// Provider implements provider.Provider for the Twitch platform without importing provider.
type Provider struct {
	source catalog.Source
	chat   catalog.Chat
}

// NewProvider returns a Twitch provider backed by the given catalog source and optional chat.
func NewProvider(source catalog.Source, chat ...catalog.Chat) *Provider {
	p := &Provider{source: source}
	if len(chat) > 0 {
		p.chat = chat[0]
	}
	return p
}

// ID returns the canonical platform identifier for Twitch ("twitch").
func (p *Provider) ID() string {
	return platform.Twitch
}

// Browser returns the browsing capability for Twitch categories and channels.
func (p *Provider) Browser() (catalog.Browser, bool) {
	if p.source != nil {
		return p.source, true
	}
	return nil, false
}

// Searcher returns the channel search capability for Twitch.
func (p *Provider) Searcher() (catalog.Searcher, bool) {
	if p.source != nil {
		return p.source, true
	}
	return nil, false
}

// Resolver returns the channel availability resolver capability for Twitch.
func (p *Provider) Resolver() (catalog.Resolver, bool) {
	if p.source != nil {
		return p.source, true
	}
	return nil, false
}

// StreamTarget builds the playback Target for a Twitch channel.
func (p *Provider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	login := ch.Login
	if login == "" && ch.URL != "" {
		if info, ok := streamurl.Parse(ch.URL); ok && info.Login != "" {
			login = info.Login
		}
	}

	if login == "" && ch.URL == "" {
		return catalog.Stream{}, errors.New("empty channel")
	}

	url := ch.URL
	if url == "" {
		url = fmt.Sprintf("https://www.twitch.tv/%s", login)
	}

	label := login
	if label == "" {
		label = ch.PlayerLabel()
	}

	return catalog.Stream{
		URL:   url,
		Label: label,
	}, nil
}

// Chat returns the chat interface for Twitch.
func (p *Provider) Chat() (catalog.Chat, bool) {
	if p.chat != nil {
		return p.chat, true
	}
	return nil, false
}
