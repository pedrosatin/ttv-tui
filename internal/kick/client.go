// Package kick reads Kick without an account or a registered app: the channel
// state comes from the JSON the site itself uses, and the chat from the
// anonymous Pusher subscription the site's player opens. Neither has a public
// contract, so every answer that is not the expected one becomes a typed error.
package kick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

const (
	// DefaultBaseURL is the site whose JSON the client reads.
	DefaultBaseURL = "https://kick.com"
	// DefaultRequestTimeout bounds one request.
	DefaultRequestTimeout = 10 * time.Second
	// maxResponseBytes bounds a response body. The channel JSON of a large
	// channel is about 15 KB.
	maxResponseBytes = 1 << 20
	// resolveConcurrency is how many saved channels are looked up at once.
	resolveConcurrency = 4
	userAgent          = "ttv-tui"
)

// ErrChannelNotFound is wrapped by the error of a lookup the site answered
// with 404: no channel has that slug.
var ErrChannelNotFound = errors.New("kick: channel not found")

// ClientConfig configures a Client. Zero values select production defaults.
type ClientConfig struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Client reads the site JSON of Kick.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client for cfg.
func NewClient(cfg ClientConfig) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: DefaultRequestTimeout}
	}
	return &Client{baseURL: base, http: hc}
}

// Room identifies the chat room of a channel.
type Room struct {
	// Slug is the channel slug as the site spells it.
	Slug string
	// DisplayName is the name of the channel owner.
	DisplayName string
	// ChatroomID is the number the chat subscription is made of.
	ChatroomID int64
}

// livestreamEnvelope is the answer of /api/v2/channels/<slug>/livestream.
// Offline is {"data":null}.
type livestreamEnvelope struct {
	Data *struct {
		ID           int64  `json:"id"`
		SessionTitle string `json:"session_title"`
		Viewers      *int   `json:"viewers"`
		Category     *struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"category"`
	} `json:"data"`
}

// channelEnvelope is the part of /api/v2/channels/<slug> the chat needs.
type channelEnvelope struct {
	Slug string `json:"slug"`
	User *struct {
		Username string `json:"username"`
	} `json:"user"`
	Chatroom *struct {
		ID int64 `json:"id"`
	} `json:"chatroom"`
}

// Slug returns the channel slug of ch, or "" when ch is not a live channel
// page: a recording or a clip has a path under the channel and no live state or
// chat of its own.
func Slug(ch catalog.Channel) string {
	slug := strings.ToLower(strings.TrimSpace(ch.StableID()))
	if slug == "" || strings.ContainsAny(slug, "/?#") {
		return ""
	}
	return slug
}

// Livestream reports the live state of the channel slug. A channel that is not
// broadcasting is AvailabilityOffline; one the site does not know returns an
// error wrapping ErrChannelNotFound.
func (c *Client) Livestream(ctx context.Context, slug string) (catalog.Channel, error) {
	const op = "livestream"
	body, err := c.get(ctx, op, "/api/v2/channels/"+url.PathEscape(slug)+"/livestream")
	if err != nil {
		return catalog.Channel{}, err
	}
	return parseLivestream(slug, body)
}

func parseLivestream(slug string, body []byte) (catalog.Channel, error) {
	const op = "livestream"
	var env livestreamEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return catalog.Channel{}, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	ch := catalog.Channel{
		ID:           platform.Kick + ":" + slug,
		Login:        slug,
		DisplayName:  slug,
		Platform:     platform.Kick,
		URL:          channelURL(slug),
		Availability: catalog.AvailabilityOffline,
	}
	if env.Data == nil {
		return ch, nil
	}
	if env.Data.ID == 0 {
		// An object that is not a livestream proves neither live nor offline.
		return catalog.Channel{}, contractError(op, errors.New("livestream without id"))
	}
	ch.Availability = catalog.AvailabilityLive
	ch.Viewers = env.Data.Viewers
	if title := strings.TrimSpace(env.Data.SessionTitle); title != "" {
		ch.Title = &title
	}
	if cat := env.Data.Category; cat != nil && strings.TrimSpace(cat.Name) != "" {
		ch.CategoryID = catalog.StringPtr(fmt.Sprint(cat.ID))
		ch.CategoryName = catalog.StringPtr(strings.TrimSpace(cat.Name))
	}
	return ch, nil
}

// Room finds the chat room of the channel slug.
func (c *Client) Room(ctx context.Context, slug string) (Room, error) {
	const op = "channel"
	body, err := c.get(ctx, op, "/api/v2/channels/"+url.PathEscape(slug))
	if err != nil {
		return Room{}, err
	}
	return parseRoom(slug, body)
}

func parseRoom(slug string, body []byte) (Room, error) {
	const op = "channel"
	var env channelEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Room{}, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	if env.Chatroom == nil || env.Chatroom.ID <= 0 {
		return Room{}, contractError(op, errors.New("channel without chatroom id"))
	}
	room := Room{Slug: slug, DisplayName: slug, ChatroomID: env.Chatroom.ID}
	if s := strings.TrimSpace(env.Slug); s != "" {
		room.Slug = s
	}
	if env.User != nil && strings.TrimSpace(env.User.Username) != "" {
		room.DisplayName = strings.TrimSpace(env.User.Username)
	}
	return room, nil
}

// ChannelsByIDs implements catalog.Resolver over the livestream lookup, one
// request per channel since the site has no batch. Identifiers that are not a
// channel slug and channels the site does not know are omitted, as is a channel
// whose answer cannot be read. A lookup the site blocks, limits or cannot serve
// fails the whole call: the next channel would get the same answer.
func (c *Client) ChannelsByIDs(ctx context.Context, ids []string) ([]catalog.Channel, error) {
	slugs := uniqueSlugs(ids)
	if len(slugs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]*catalog.Channel, len(slugs))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, resolveConcurrency)
	for i, slug := range slugs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			ch, err := c.Livestream(ctx, slug)
			switch {
			case err == nil:
				results[i] = &ch
			case errors.Is(err, ErrChannelNotFound), catalog.KindOf(err) == catalog.Contrato:
				// No evidence about this channel; the others are still worth asking.
			default:
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	out := make([]catalog.Channel, 0, len(slugs))
	for _, ch := range results {
		if ch != nil {
			out = append(out, *ch)
		}
	}
	return out, nil
}

func uniqueSlugs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		slug := Slug(catalog.Channel{Platform: platform.Kick, Login: id})
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}

func channelURL(slug string) string {
	return DefaultBaseURL + "/" + slug
}

func (c *Client) get(ctx context.Context, op, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, contractError(op, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, &catalog.Error{Platform: platform.Kick, Kind: catalog.Rede, Op: op, Err: err}
	}
	defer func() { _ = res.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))

	if res.StatusCode != http.StatusOK {
		return nil, statusError(op, res.StatusCode)
	}
	if readErr != nil {
		return nil, &catalog.Error{Platform: platform.Kick, Kind: catalog.Rede, Op: op, Err: fmt.Errorf("read response: %w", readErr)}
	}
	return body, nil
}

// statusError classifies an answer that is not 200. A 403 is the anti-bot wall
// in front of the site, not an answer of the site: it carries no channel data.
func statusError(op string, status int) error {
	e := &catalog.Error{Platform: platform.Kick, Op: op, StatusCode: status, Kind: catalog.HTTPStatusKind(status)}
	switch status {
	case http.StatusForbidden:
		e.Kind = catalog.Bloqueio
		e.Err = errors.New("request refused by the site")
	case http.StatusNotFound:
		e.Err = ErrChannelNotFound
	}
	return e
}

func contractError(op string, err error) error {
	return &catalog.Error{Platform: platform.Kick, Kind: catalog.Contrato, Op: op, Err: err}
}
