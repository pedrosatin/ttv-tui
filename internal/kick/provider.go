package kick

import (
	"errors"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Provider implements provider.Provider for Kick without importing provider.
type Provider struct {
	browser  catalog.Browser
	searcher catalog.Searcher
	resolver catalog.Resolver
	chat     catalog.Chat
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// WithBrowser attaches what lists the live categories and their channels.
func WithBrowser(b catalog.Browser) ProviderOption {
	return func(p *Provider) { p.browser = b }
}

// WithSearcher attaches what finds channels by name.
func WithSearcher(s catalog.Searcher) ProviderOption {
	return func(p *Provider) { p.searcher = s }
}

// WithResolver attaches what resolves the live state of saved channels.
func WithResolver(r catalog.Resolver) ProviderOption {
	return func(p *Provider) { p.resolver = r }
}

// WithChat attaches the chat implementation the provider returns from Chat.
func WithChat(c catalog.Chat) ProviderOption {
	return func(p *Provider) { p.chat = c }
}

// NewProvider returns a Kick provider. Without options it only builds playback
// targets: a channel then arrives by URL, from the favorites or from the recents.
func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ID returns the canonical platform identifier for Kick ("kick").
func (p *Provider) ID() string { return platform.Kick }

// Browser returns the listing of live categories and their channels.
func (p *Provider) Browser() (catalog.Browser, bool) {
	if p.browser != nil {
		return p.browser, true
	}
	return nil, false
}

// Searcher returns the search of channels by name.
func (p *Provider) Searcher() (catalog.Searcher, bool) {
	if p.searcher != nil {
		return p.searcher, true
	}
	return nil, false
}

// Resolver returns the live-state lookup for saved channels.
func (p *Provider) Resolver() (catalog.Resolver, bool) {
	if p.resolver != nil {
		return p.resolver, true
	}
	return nil, false
}

// StreamTarget builds the playback target of a Kick channel. A URL the channel
// carries (a recording, a clip) plays as it is; a channel known only by its
// slug plays its live page. Every target carries the ad-filtering kick plugin;
// recordings and clips go through it unchanged.
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
	return catalog.Stream{
		URL:     target,
		Plugins: []catalog.StreamlinkPlugin{adFilterPlugin()},
		Label:   label,
	}, nil
}

// Chat returns the chat of Kick.
func (p *Provider) Chat() (catalog.Chat, bool) {
	if p.chat != nil {
		return p.chat, true
	}
	return nil, false
}
