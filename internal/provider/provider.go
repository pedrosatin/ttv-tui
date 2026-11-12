package provider

import (
	"fmt"
	"sort"
	"sync"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/streamurl"
)

// Target is an alias for catalog.Stream.
type Target = catalog.Stream

// Chat is an alias for catalog.Chat.
type Chat = catalog.Chat

// Provider defines the interface implemented by streaming platforms.
type Provider interface {
	// ID returns the canonical platform identifier (e.g. "twitch", "kick", "youtube", "url").
	ID() string
	// Browser returns the optional capability for browsing categories and channels.
	Browser() (catalog.Browser, bool)
	// Searcher returns the optional capability for searching live channels.
	Searcher() (catalog.Searcher, bool)
	// Resolver returns the optional capability for resolving stream state of saved channels.
	Resolver() (catalog.Resolver, bool)
	// StreamTarget builds the playback Target for channel (URL, extra streamlink args, and qualified label).
	StreamTarget(channel catalog.Channel) (catalog.Stream, error)
	// Chat returns the optional platform chat interface (minimal placeholder until the chat moves behind the provider).
	Chat() (catalog.Chat, bool)
}

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

// Get returns the registered provider for the given platform ID, if any.
func (r *Registry) Get(id string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	norm := platform.Normalize(id)
	if norm == "url" {
		if r.url != nil {
			return r.url, true
		}
		return nil, false
	}
	p, ok := r.providers[norm]
	return p, ok
}

// IDs returns a deterministic, sorted slice of all registered platform IDs.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers)+1)
	for id := range r.providers {
		ids = append(ids, id)
	}
	if r.url != nil {
		ids = append(ids, "url")
	}
	sort.Strings(ids)
	return ids
}

// ProviderFor returns the appropriate provider for the given channel.
// When the channel's platform is not registered, it returns an explicit error
// unless the channel has a stream URL that can be handled by the generic URL provider.
func (r *Registry) ProviderFor(ch catalog.Channel) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plat := platform.Normalize(ch.Platform)
	if ch.Platform == "" && ch.URL != "" {
		if info, ok := streamurl.Parse(ch.URL); ok {
			plat = platform.Normalize(info.Platform)
		}
	}

	if p, ok := r.providers[plat]; ok {
		return p, nil
	}

	if ch.URL != "" && r.url != nil {
		return r.url, nil
	}

	return nil, fmt.Errorf("no provider registered for platform %q", plat)
}

// StreamTarget builds the playback Target by selecting the appropriate provider for ch.
func (r *Registry) StreamTarget(ch catalog.Channel) (catalog.Stream, error) {
	p, err := r.ProviderFor(ch)
	if err != nil {
		return catalog.Stream{}, err
	}
	return p.StreamTarget(ch)
}
