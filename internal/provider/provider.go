package provider

import (
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/player"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// Provider defines the interface implemented by streaming platforms.
type Provider interface {
	// ID returns the canonical platform identifier (e.g. "twitch", "kick", "youtube", "url").
	ID() string
	// Catalog returns the optional catalog Source for browsing categories and channels.
	// Platforms without a catalog return nil, false.
	Catalog() (catalog.Source, bool)
	// StreamTarget builds the playback Target for channel (URL, extra streamlink args, and qualified label).
	StreamTarget(channel catalog.Channel) (player.Target, error)
	// Chat returns the optional platform chat interface (minimal placeholder for ticket 09).
	Chat() (Chat, bool)
}

// Chat is a minimal placeholder interface for platform chat, to be expanded in ticket 09.
type Chat interface{}

// Registry manages registered Providers by platform ID.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	url       Provider
}

// NewRegistry creates a new Registry with the provided providers.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
		url:       NewURLProvider(),
	}
	for _, p := range providers {
		r.Register(p)
	}
	return r
}

// Register adds or replaces a provider for its ID.
func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := platform.Normalize(p.ID())
	if id == "url" {
		r.url = p
	} else {
		r.providers[id] = p
	}
}

// ProviderFor returns the appropriate provider for the given channel.
func (r *Registry) ProviderFor(ch catalog.Channel) Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plat := platform.Normalize(ch.Platform)
	if ch.Platform == "" && ch.URL != "" {
		if info, ok := streamurl.Parse(ch.URL); ok {
			plat = platform.Normalize(info.Platform)
		}
	}

	if p, ok := r.providers[plat]; ok {
		return p
	}
	return r.url
}

// StreamTarget builds the playback Target by selecting the appropriate provider for ch.
func (r *Registry) StreamTarget(ch catalog.Channel) (player.Target, error) {
	p := r.ProviderFor(ch)
	return p.StreamTarget(ch)
}
