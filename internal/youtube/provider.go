package youtube

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Provider implements provider.Provider for YouTube without importing provider.
// YouTube exposes no live catalog (Browser and Searcher are always nil/false);
// channels arrive via pasted URLs or @handles and saved lists.
type Provider struct {
	resolver catalog.Resolver
	chat     catalog.Chat
}

// ProviderOption configures a Provider.
type ProviderOption func(*Provider)

// WithResolver attaches the live-state lookup for saved and pasted YouTube targets.
func WithResolver(r catalog.Resolver) ProviderOption {
	return func(p *Provider) { p.resolver = r }
}

// WithChat attaches the chat implementation returned by Chat.
func WithChat(c catalog.Chat) ProviderOption {
	return func(p *Provider) { p.chat = c }
}

// NewProvider returns a YouTube provider configured by opts.
func NewProvider(opts ...ProviderOption) *Provider {
	p := &Provider{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ID returns the canonical platform identifier for YouTube ("youtube").
func (p *Provider) ID() string { return platform.YouTube }

// Browser reports that YouTube exposes no category/channel browser.
func (p *Provider) Browser() (catalog.Browser, bool) { return nil, false }

// Searcher reports that YouTube exposes no live channel search.
func (p *Provider) Searcher() (catalog.Searcher, bool) { return nil, false }

// Resolver returns the live-state lookup for saved and pasted YouTube targets.
func (p *Provider) Resolver() (catalog.Resolver, bool) {
	if p.resolver != nil {
		return p.resolver, true
	}
	return nil, false
}

// StreamTarget builds the playback target for ch. Channel handles (@channel)
// always target /@channel/live because streamlink's youtube plugin rejects a
// bare /@channel page; watch/youtu.be//live URLs keep their canonical video URL.
func (p *Provider) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	raw := strings.TrimSpace(ch.URL)
	if raw == "" {
		raw = strings.TrimSpace(ch.StableID())
	}
	if raw == "" {
		return catalog.Stream{}, errors.New("empty channel")
	}

	target, err := ParseTarget(raw)
	if err != nil {
		return catalog.Stream{}, fmt.Errorf("youtube: %w", err)
	}

	labelID := strings.TrimSpace(ch.StableID())
	if labelID == "" {
		labelID = target.ID
	}
	return catalog.Stream{
		URL:   target.PlaybackURL,
		Label: platform.YouTube + ":" + labelID,
	}, nil
}

// Chat returns YouTube's read-only live chat, if configured.
func (p *Provider) Chat() (catalog.Chat, bool) {
	if p.chat != nil {
		return p.chat, true
	}
	return nil, false
}
