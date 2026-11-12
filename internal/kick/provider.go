package kick

import (
	"errors"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Provider implements provider.Provider for Kick without importing provider.
// It has no catalog to browse or search: a channel arrives by URL, from the
// favorites or from the recents.
type Provider struct {
	resolver catalog.Resolver
	chat     catalog.Chat
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// WithResolver attaches what resolves the live state of saved channels.
func WithResolver(r catalog.Resolver) ProviderOption {
	return func(p *Provider) { p.resolver = r }
}

// WithChat attaches the chat implementation the provider returns from Chat.
func WithChat(c catalog.Chat) ProviderOption {
	return func(p *Provider) { p.chat = c }
}

// NewProvider returns a Kick provider. Without options it only builds playback
// targets.
func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ID returns the canonical platform identifier for Kick ("kick").
func (p *Provider) ID() string { return platform.Kick }

// Browser reports no browsing capability.
func (p *Provider) Browser() (catalog.Browser, bool) { return nil, false }

// Searcher reports no search capability.
func (p *Provider) Searcher() (catalog.Searcher, bool) { return nil, false }

// Resolver returns the live-state lookup for saved channels.
func (p *Provider) Resolver() (catalog.Resolver, bool) {
	if p.resolver != nil {
		return p.resolver, true
	}
	return nil, false
}

// StreamTarget builds the playback target of a Kick channel. A URL the channel
// carries (a recording, a clip) plays as it is; a channel known only by its
// slug plays its live page.
func (p *Provider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	target := strings.TrimSpace(ch.URL)
	id := strings.TrimSpace(ch.StableID())
	if target == "" {
		if id == "" {
			return catalog.Stream{}, errors.New("empty channel")
		}
		target = DefaultBaseURL + "/" + id
	}
	label := target
	if id != "" {
		label = platform.Kick + ":" + id
	}
	return catalog.Stream{URL: target, Label: label}, nil
}

// Chat returns the chat of Kick.
func (p *Provider) Chat() (catalog.Chat, bool) {
	if p.chat != nil {
		return p.chat, true
	}
	return nil, false
}
