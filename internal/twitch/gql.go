package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultGQLEndpoint is Twitch's public GraphQL endpoint.
	DefaultGQLEndpoint = "https://gql.twitch.tv/gql"
	// DefaultClientID is the public web Client-Id validated in the prototype.
	DefaultClientID = "kimne78kx3ncx6brgo4mv6wki5h1ko"
	// DefaultPageLimit is the default number of items requested per page.
	DefaultPageLimit = 50
	// DefaultRequestTimeout is the per-attempt HTTP timeout.
	DefaultRequestTimeout = 10 * time.Second
	// DefaultRetryBackoff is the delay before the single retry on network or 5xx errors.
	DefaultRetryBackoff = 200 * time.Millisecond
)

const categoriesQuery = `query($n: Int!, $after: Cursor) {
  games(first: $n, after: $after) {
    edges {
      cursor
      node {
        id
        name
        viewersCount
      }
    }
    pageInfo {
      hasNextPage
    }
  }
}`

const channelsQuery = `query($id: ID, $name: String, $n: Int!, $after: Cursor) {
  game(id: $id, name: $name) {
    id
    name
    streams(first: $n, after: $after) {
      edges {
        cursor
        node {
          id
          broadcaster {
            id
            login
            displayName
          }
          viewersCount
          title
        }
      }
      pageInfo {
        hasNextPage
      }
    }
  }
}`

const searchQuery = `query($q: String!) {
  searchFor(userQuery: $q, platform: "web") {
    channels {
      cursor
      edges {
        item {
          ... on User {
            id
            login
            displayName
            stream {
              id
              viewersCount
              title
            }
          }
        }
      }
    }
  }
}`

// GQLConfig configures a GQLSource adapter.
type GQLConfig struct {
	Endpoint       string
	ClientID       string
	HTTPClient     *http.Client
	RequestTimeout time.Duration
	RetryBackoff   time.Duration
	CacheTTL       time.Duration
	Now            func() time.Time
	Sleep          func(ctx context.Context, d time.Duration) error
}

type cachedCategoriesPage struct {
	page      Page[Category]
	expiresAt time.Time
}

type cachedChannelsPage struct {
	page      Page[Channel]
	expiresAt time.Time
}

// GQLSource implements Source using Twitch's public GraphQL endpoint.
type GQLSource struct {
	endpoint       string
	clientID       string
	httpClient     *http.Client
	requestTimeout time.Duration
	retryBackoff   time.Duration
	cacheTTL       time.Duration
	now            func() time.Time
	sleep          func(ctx context.Context, d time.Duration) error

	mu            sync.Mutex
	cache         map[string]cachedCategoriesPage
	channelsCache map[string]cachedChannelsPage
	categoryNames map[string]string
}

// NewGQLSource constructs a GraphQL Source adapter with sensible defaults.
func NewGQLSource(cfg GQLConfig) *GQLSource {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultGQLEndpoint
	}
	clientID := strings.TrimSpace(cfg.ClientID)
	if clientID == "" {
		clientID = DefaultClientID
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	reqTimeout := cfg.RequestTimeout
	if reqTimeout <= 0 {
		reqTimeout = DefaultRequestTimeout
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = DefaultRetryBackoff
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	sleepFn := cfg.Sleep
	if sleepFn == nil {
		sleepFn = defaultSleep
	}

	return &GQLSource{
		endpoint:       endpoint,
		clientID:       clientID,
		httpClient:     httpClient,
		requestTimeout: reqTimeout,
		retryBackoff:   backoff,
		cacheTTL:       cfg.CacheTTL,
		now:            nowFn,
		sleep:          sleepFn,
		cache:          make(map[string]cachedCategoriesPage),
		channelsCache:  make(map[string]cachedChannelsPage),
		categoryNames:  make(map[string]string),
	}
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlErrorExtensions struct {
	Code string `json:"code"`
}

type gqlErrorItem struct {
	Message    string              `json:"message"`
	Extensions *gqlErrorExtensions `json:"extensions,omitempty"`
}

func isIntegrityCheckFailed(errs []gqlErrorItem) bool {
	for _, e := range errs {
		if e.Extensions != nil && e.Extensions.Code == "IntegrityCheckFailed" {
			return true
		}
		if strings.Contains(strings.ToLower(e.Message), "failed integrity check") {
			return true
		}
	}
	return false
}

type gqlCategoriesResponse struct {
	Data   *gqlCategoriesData `json:"data"`
	Errors []gqlErrorItem     `json:"errors"`
}

type gqlCategoriesData struct {
	Games *gqlGamesConnection `json:"games"`
}

type gqlGamesConnection struct {
	Edges    []gqlGameEdge `json:"edges"`
	PageInfo gqlPageInfo   `json:"pageInfo"`
}

type gqlGameEdge struct {
	Cursor string       `json:"cursor"`
	Node   *gqlGameNode `json:"node"`
}

type gqlGameNode struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ViewersCount *int   `json:"viewersCount"`
}

type gqlPageInfo struct {
	HasNextPage bool `json:"hasNextPage"`
}

type gqlChannelsResponse struct {
	Data   *gqlChannelsData `json:"data"`
	Errors []gqlErrorItem   `json:"errors"`
}

type gqlChannelsData struct {
	Game *gqlGameStreams `json:"game"`
}

type gqlGameStreams struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Streams *gqlStreamsConnection `json:"streams"`
}

type gqlStreamsConnection struct {
	Edges    []gqlStreamEdge `json:"edges"`
	PageInfo gqlPageInfo     `json:"pageInfo"`
}

type gqlStreamEdge struct {
	Cursor string         `json:"cursor"`
	Node   *gqlStreamNode `json:"node"`
}

type gqlStreamNode struct {
	ID           string          `json:"id"`
	Broadcaster  *gqlBroadcaster `json:"broadcaster"`
	ViewersCount *int            `json:"viewersCount"`
	Title        *string         `json:"title"`
}

type gqlBroadcaster struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"displayName"`
}

type gqlSearchResponse struct {
	Data   *gqlSearchData `json:"data"`
	Errors []gqlErrorItem `json:"errors"`
}

type gqlSearchData struct {
	SearchFor *gqlSearchFor `json:"searchFor"`
}

type gqlSearchFor struct {
	Channels *gqlSearchChannelsConnection `json:"channels"`
}

type gqlSearchChannelsConnection struct {
	Cursor string                 `json:"cursor"`
	Edges  []gqlSearchChannelEdge `json:"edges"`
}

type gqlSearchChannelEdge struct {
	Item *gqlSearchUser `json:"item"`
}

// gqlSearchUser keeps stream raw so an absent field (no evidence) differs from
// an explicit null (the user is not broadcasting).
type gqlSearchUser struct {
	ID          string          `json:"id"`
	Login       string          `json:"login"`
	DisplayName string          `json:"displayName"`
	Stream      json.RawMessage `json:"stream"`
}

type gqlSearchStream struct {
	ID           string  `json:"id"`
	ViewersCount *int    `json:"viewersCount"`
	Title        *string `json:"title"`
}

// Categories fetches a page of live categories ordered by viewer count descending.
func (s *GQLSource) Categories(ctx context.Context, page PageRequest) (Page[Category], error) {
	const op = "categories"

	if err := ctx.Err(); err != nil {
		return Page[Category]{}, &Error{
			Kind: Rede,
			Op:   op,
			Err:  fmt.Errorf("context canceled before request: %w", err),
		}
	}

	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}

	cacheKey := fmt.Sprintf("%s|%d", page.Cursor, limit)
	if cached, ok := s.getCachedCategories(cacheKey); ok {
		return cached, nil
	}

	vars := map[string]any{
		"n": limit,
	}
	if page.Cursor != "" {
		vars["after"] = page.Cursor
	}

	payload, err := json.Marshal(gqlRequest{
		Query:     categoriesQuery,
		Variables: vars,
	})
	if err != nil {
		return Page[Category]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("encode graphql request: %w", err),
		}
	}

	res, reqErr := executeWithRetry(s, ctx, op, payload, s.parseCategoriesResponse)
	if reqErr != nil {
		return Page[Category]{}, reqErr
	}
	s.rememberCategories(res.Items)
	s.setCachedCategories(cacheKey, res)
	return res, nil
}

// Channels fetches a page of live channels for the given category.
func (s *GQLSource) Channels(ctx context.Context, categoryID string, page PageRequest) (Page[Channel], error) {
	const op = "channels"

	if err := ctx.Err(); err != nil {
		return Page[Channel]{}, &Error{
			Kind: Rede,
			Op:   op,
			Err:  fmt.Errorf("context canceled before request: %w", err),
		}
	}

	catKey := strings.TrimSpace(categoryID)
	if catKey == "" {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  errors.New("categoryID is required"),
		}
	}

	limit := page.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}

	cacheKey := fmt.Sprintf("channels|%s|%s|%d", catKey, page.Cursor, limit)
	if cached, ok := s.getCachedChannels(cacheKey); ok {
		return cached, nil
	}

	vars := map[string]any{
		"n": limit,
	}
	if isNumericID(catKey) {
		vars["id"] = catKey
		if name := s.lookupCategoryName(catKey); name != "" {
			vars["name"] = name
		}
	} else {
		vars["name"] = catKey
	}
	if page.Cursor != "" {
		vars["after"] = page.Cursor
	}

	payload, err := json.Marshal(gqlRequest{
		Query:     channelsQuery,
		Variables: vars,
	})
	if err != nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("encode graphql request: %w", err),
		}
	}

	res, reqErr := executeWithRetry(s, ctx, op, payload, func(body []byte) (Page[Channel], *Error) {
		return s.parseChannelsResponse(body, catKey)
	})
	if reqErr != nil {
		return Page[Channel]{}, reqErr
	}
	s.setCachedChannels(cacheKey, res)
	return res, nil
}

// Search searches channels globally by user query.
func (s *GQLSource) Search(ctx context.Context, query string) (Page[Channel], error) {
	const op = "search"

	if err := ctx.Err(); err != nil {
		return Page[Channel]{}, &Error{
			Kind: Rede,
			Op:   op,
			Err:  fmt.Errorf("context canceled before request: %w", err),
		}
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return Page[Channel]{
			Items:   []Channel{},
			Cursor:  "",
			HasMore: false,
		}, nil
	}

	cacheKey := fmt.Sprintf("search|%s", q)
	if cached, ok := s.getCachedChannels(cacheKey); ok {
		return cached, nil
	}

	vars := map[string]any{
		"q": q,
	}

	payload, err := json.Marshal(gqlRequest{
		Query:     searchQuery,
		Variables: vars,
	})
	if err != nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("encode graphql request: %w", err),
		}
	}

	res, reqErr := executeWithRetry(s, ctx, op, payload, s.parseSearchResponse)
	if reqErr != nil {
		return Page[Channel]{}, reqErr
	}
	// Prefer exact login, then live among clones; see PreferSearchChannels.
	res.Items = PreferSearchChannels(q, res.Items)
	s.setCachedChannels(cacheKey, res)
	return res, nil
}

func executeWithRetry[T any](
	s *GQLSource,
	ctx context.Context,
	op string,
	payload []byte,
	parse func(body []byte) (Page[T], *Error),
) (Page[T], *Error) {
	var lastErr *Error
	for attempt := 0; attempt < 2; attempt++ {
		res, reqErr := doAttempt(s, ctx, op, payload, parse)
		if reqErr == nil {
			return res, nil
		}
		lastErr = reqErr

		// Never retry if the caller's context was canceled or expired.
		if ctx.Err() != nil {
			return Page[T]{}, lastErr
		}

		// Only attempt a single retry on attempt 0.
		if attempt > 0 {
			break
		}

		switch lastErr.Kind {
		case Rede, Servidor:
			if sleepErr := s.sleep(ctx, s.retryBackoff); sleepErr != nil {
				return Page[T]{}, &Error{
					Kind: Rede,
					Op:   op,
					Err:  fmt.Errorf("retry backoff interrupted: %w", sleepErr),
				}
			}
		case RateLimit:
			// Respect Ratelimit-Reset when present on HTTP 429.
			if lastErr.ResetAfter <= 0 && lastErr.ResetAt.IsZero() {
				return Page[T]{}, lastErr
			}
			if sleepErr := s.sleep(ctx, lastErr.ResetAfter); sleepErr != nil {
				return Page[T]{}, &Error{
					Kind:       RateLimit,
					Op:         op,
					StatusCode: lastErr.StatusCode,
					ResetAfter: lastErr.ResetAfter,
					ResetAt:    lastErr.ResetAt,
					Err:        fmt.Errorf("rate limit wait interrupted: %w", sleepErr),
				}
			}
		default:
			// Contrato errors are never retried.
			return Page[T]{}, lastErr
		}
	}

	return Page[T]{}, lastErr
}

func doAttempt[T any](
	s *GQLSource,
	ctx context.Context,
	op string,
	payload []byte,
	parse func(body []byte) (Page[T], *Error),
) (Page[T], *Error) {
	reqCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Page[T]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("build http request: %w", err),
		}
	}
	req.Header.Set("Client-Id", s.clientID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Page[T]{}, &Error{
			Kind: Rede,
			Op:   op,
			Err:  fmt.Errorf("execute http request: %w", err),
		}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusTooManyRequests {
		resetAfter, resetAt, hasReset := s.parseRateLimitReset(resp.Header)
		msg := "rate limit exceeded (HTTP 429)"
		if hasReset {
			msg = fmt.Sprintf("rate limit exceeded (HTTP 429, reset in %s)", resetAfter.Round(time.Second))
		}
		return Page[T]{}, &Error{
			Kind:       RateLimit,
			Op:         op,
			StatusCode: resp.StatusCode,
			ResetAfter: resetAfter,
			ResetAt:    resetAt,
			Err:        errors.New(msg),
		}
	}

	if resp.StatusCode >= 500 {
		return Page[T]{}, &Error{
			Kind:       Servidor,
			Op:         op,
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("upstream server error: status %d", resp.StatusCode),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return Page[T]{}, &Error{
			Kind:       Contrato,
			Op:         op,
			StatusCode: resp.StatusCode,
			Err:        fmt.Errorf("unexpected status %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Page[T]{}, &Error{
			Kind: Rede,
			Op:   op,
			Err:  fmt.Errorf("read response body: %w", err),
		}
	}

	return parse(body)
}

func (s *GQLSource) parseCategoriesResponse(body []byte) (Page[Category], *Error) {
	const op = "categories"

	var gqlResp gqlCategoriesResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return Page[Category]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("decode graphql json: %w", err),
		}
	}

	if isIntegrityCheckFailed(gqlResp.Errors) {
		// Paginar categorias exige token de integridade que não temos.
		return Page[Category]{
			Items:   []Category{},
			Cursor:  "",
			HasMore: false,
		}, nil
	}

	if len(gqlResp.Errors) > 0 {
		msg := strings.TrimSpace(gqlResp.Errors[0].Message)
		if msg == "" {
			msg = "graphql error"
		}
		return Page[Category]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("graphql error: %s", msg),
		}
	}

	if gqlResp.Data == nil || gqlResp.Data.Games == nil {
		return Page[Category]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  errors.New("missing data.games in graphql response"),
		}
	}

	edges := gqlResp.Data.Games.Edges
	items := make([]Category, 0, len(edges))
	var lastCursor string
	for i, edge := range edges {
		if edge.Cursor != "" {
			lastCursor = edge.Cursor
		}
		if edge.Node == nil {
			return Page[Category]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d has null node", i),
			}
		}
		id := strings.TrimSpace(edge.Node.ID)
		name := strings.TrimSpace(edge.Node.Name)
		if id == "" || name == "" {
			return Page[Category]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d missing required category identity (id=%q, name=%q)", i, id, name),
			}
		}
		var viewers *int
		if edge.Node.ViewersCount != nil {
			v := *edge.Node.ViewersCount
			viewers = &v
		}
		items = append(items, Category{
			ID:      id,
			Name:    name,
			Viewers: viewers,
		})
	}

	// Sort categories from highest viewer count to lowest; categories without
	// viewer counts are placed after categories with viewer counts.
	sort.SliceStable(items, func(i, j int) bool {
		vi, vj := items[i].Viewers, items[j].Viewers
		if vi == nil && vj == nil {
			return false
		}
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return *vi > *vj
	})

	hasMore := gqlResp.Data.Games.PageInfo.HasNextPage && len(items) > 0
	return Page[Category]{
		Items:   items,
		Cursor:  lastCursor,
		HasMore: hasMore,
	}, nil
}

func (s *GQLSource) parseChannelsResponse(body []byte, fallbackCategoryID string) (Page[Channel], *Error) {
	const op = "channels"

	var gqlResp gqlChannelsResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("decode graphql json: %w", err),
		}
	}

	if isIntegrityCheckFailed(gqlResp.Errors) {
		return Page[Channel]{
			Items:   []Channel{},
			Cursor:  "",
			HasMore: false,
		}, nil
	}

	if len(gqlResp.Errors) > 0 {
		msg := strings.TrimSpace(gqlResp.Errors[0].Message)
		if msg == "" {
			msg = "graphql error"
		}
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("graphql error: %s", msg),
		}
	}

	if gqlResp.Data == nil || gqlResp.Data.Game == nil || gqlResp.Data.Game.Streams == nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  errors.New("missing data.game.streams in graphql response"),
		}
	}

	resolvedCatID := strings.TrimSpace(gqlResp.Data.Game.ID)
	if resolvedCatID == "" {
		resolvedCatID = fallbackCategoryID
	}

	edges := gqlResp.Data.Game.Streams.Edges
	items := make([]Channel, 0, len(edges))
	var lastCursor string
	for i, edge := range edges {
		if edge.Cursor != "" {
			lastCursor = edge.Cursor
		}
		if edge.Node == nil || edge.Node.Broadcaster == nil {
			return Page[Channel]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d has null stream node or broadcaster", i),
			}
		}
		login := strings.TrimSpace(edge.Node.Broadcaster.Login)
		displayName := strings.TrimSpace(edge.Node.Broadcaster.DisplayName)
		if login == "" || displayName == "" {
			return Page[Channel]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d missing required channel identity (login=%q, displayName=%q)", i, login, displayName),
			}
		}
		id := strings.TrimSpace(edge.Node.Broadcaster.ID)
		if id == "" {
			id = strings.TrimSpace(edge.Node.ID)
		}
		if id == "" {
			id = login
		}
		var viewers *int
		if edge.Node.ViewersCount != nil {
			v := *edge.Node.ViewersCount
			viewers = &v
		}
		var title *string
		if edge.Node.Title != nil {
			t := strings.Join(strings.Fields(*edge.Node.Title), " ")
			if t != "" {
				title = &t
			}
		}
		var catIDPtr *string
		if resolvedCatID != "" {
			catCopy := resolvedCatID
			catIDPtr = &catCopy
		}
		items = append(items, Channel{
			ID:          id,
			Login:       login,
			DisplayName: displayName,
			Viewers:     viewers,
			Title:       title,
			CategoryID:  catIDPtr,
			// Game streams list only active broadcasts.
			Availability: AvailabilityLive,
		})
	}

	hasMore := gqlResp.Data.Game.Streams.PageInfo.HasNextPage && len(items) > 0
	return Page[Channel]{
		Items:   items,
		Cursor:  lastCursor,
		HasMore: hasMore,
	}, nil
}

func (s *GQLSource) parseSearchResponse(body []byte) (Page[Channel], *Error) {
	const op = "search"

	var gqlResp gqlSearchResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("decode graphql json: %w", err),
		}
	}

	if isIntegrityCheckFailed(gqlResp.Errors) {
		return Page[Channel]{
			Items:   []Channel{},
			Cursor:  "",
			HasMore: false,
		}, nil
	}

	if len(gqlResp.Errors) > 0 {
		msg := strings.TrimSpace(gqlResp.Errors[0].Message)
		if msg == "" {
			msg = "graphql error"
		}
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  fmt.Errorf("graphql error: %s", msg),
		}
	}

	if gqlResp.Data == nil || gqlResp.Data.SearchFor == nil || gqlResp.Data.SearchFor.Channels == nil {
		return Page[Channel]{}, &Error{
			Kind: Contrato,
			Op:   op,
			Err:  errors.New("missing data.searchFor.channels in graphql response"),
		}
	}

	edges := gqlResp.Data.SearchFor.Channels.Edges
	items := make([]Channel, 0, len(edges))
	for i, edge := range edges {
		if edge.Item == nil {
			return Page[Channel]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d has null item", i),
			}
		}
		login := strings.TrimSpace(edge.Item.Login)
		displayName := strings.TrimSpace(edge.Item.DisplayName)
		if login == "" || displayName == "" {
			return Page[Channel]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d missing required channel identity (login=%q, displayName=%q)", i, login, displayName),
			}
		}
		id := strings.TrimSpace(edge.Item.ID)
		if id == "" {
			id = login
		}
		ch := Channel{
			ID:          id,
			Login:       login,
			DisplayName: displayName,
		}
		if err := applySearchStream(&ch, edge.Item.Stream); err != nil {
			return Page[Channel]{}, &Error{
				Kind: Contrato,
				Op:   op,
				Err:  fmt.Errorf("edge %d stream: %w", i, err),
			}
		}
		items = append(items, ch)
	}

	return Page[Channel]{
		Items:   items,
		Cursor:  "",
		HasMore: false,
	}, nil
}

func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (s *GQLSource) rememberCategories(cats []Category) {
	if len(cats) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range cats {
		if c.ID != "" && c.Name != "" {
			s.categoryNames[c.ID] = c.Name
		}
	}
}

func (s *GQLSource) lookupCategoryName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.categoryNames[id]
}

func (s *GQLSource) parseRateLimitReset(h http.Header) (time.Duration, time.Time, bool) {
	raw := strings.TrimSpace(h.Get("Ratelimit-Reset"))
	if raw == "" {
		return 0, time.Time{}, false
	}
	now := s.now()

	if val, err := strconv.ParseFloat(raw, 64); err == nil && !math.IsNaN(val) && !math.IsInf(val, 0) && val > 0 {
		// Values >= 1e9 are treated as Unix epoch timestamps in seconds;
		// smaller positive numbers are treated as relative seconds.
		if val >= 1_000_000_000 {
			sec := int64(val)
			nsec := int64((val - float64(sec)) * float64(time.Second))
			resetAt := time.Unix(sec, nsec)
			wait := resetAt.Sub(now)
			if wait < 0 {
				wait = 0
			}
			return wait, resetAt, true
		}
		wait := time.Duration(val * float64(time.Second))
		return wait, now.Add(wait), true
	}

	if t, err := http.ParseTime(raw); err == nil {
		wait := t.Sub(now)
		if wait < 0 {
			wait = 0
		}
		return wait, t, true
	}

	return 0, time.Time{}, false
}

func (s *GQLSource) getCachedCategories(key string) (Page[Category], bool) {
	if s.cacheTTL <= 0 {
		return Page[Category]{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || !s.now().Before(entry.expiresAt) {
		delete(s.cache, key)
		return Page[Category]{}, false
	}
	return cloneCategoryPage(entry.page), true
}

func (s *GQLSource) setCachedCategories(key string, page Page[Category]) {
	if s.cacheTTL <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = cachedCategoriesPage{
		page:      cloneCategoryPage(page),
		expiresAt: s.now().Add(s.cacheTTL),
	}
}

func cloneCategoryPage(p Page[Category]) Page[Category] {
	items := make([]Category, len(p.Items))
	for i, c := range p.Items {
		items[i] = Category{
			ID:   c.ID,
			Name: c.Name,
		}
		if c.Viewers != nil {
			v := *c.Viewers
			items[i].Viewers = &v
		}
	}
	return Page[Category]{
		Items:   items,
		Cursor:  p.Cursor,
		HasMore: p.HasMore,
	}
}

func (s *GQLSource) getCachedChannels(key string) (Page[Channel], bool) {
	if s.cacheTTL <= 0 {
		return Page[Channel]{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.channelsCache[key]
	if !ok || !s.now().Before(entry.expiresAt) {
		delete(s.channelsCache, key)
		return Page[Channel]{}, false
	}
	return cloneChannelPage(entry.page), true
}

func (s *GQLSource) setCachedChannels(key string, page Page[Channel]) {
	if s.cacheTTL <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channelsCache[key] = cachedChannelsPage{
		page:      cloneChannelPage(page),
		expiresAt: s.now().Add(s.cacheTTL),
	}
}

func cloneChannelPage(p Page[Channel]) Page[Channel] {
	items := make([]Channel, len(p.Items))
	for i, ch := range p.Items {
		items[i] = Channel{
			ID:          ch.ID,
			Login:       ch.Login,
			DisplayName: ch.DisplayName,
		}
		if ch.Viewers != nil {
			v := *ch.Viewers
			items[i].Viewers = &v
		}
		if ch.Title != nil {
			t := *ch.Title
			items[i].Title = &t
		}
		if ch.CategoryID != nil {
			c := *ch.CategoryID
			items[i].CategoryID = &c
		}
	}
	return Page[Channel]{
		Items:   items,
		Cursor:  p.Cursor,
		HasMore: p.HasMore,
	}
}

// applySearchStream fills availability from a search item's stream field:
// absent leaves it unknown, null is explicit offline evidence, an object is live.
func applySearchStream(ch *Channel, raw json.RawMessage) error {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "":
		return nil
	case "null":
		ch.Availability = AvailabilityOffline
		return nil
	}
	var st gqlSearchStream
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Errorf("decode stream: %w", err)
	}
	ch.Availability = AvailabilityLive
	if st.ViewersCount != nil {
		v := *st.ViewersCount
		ch.Viewers = &v
	}
	if st.Title != nil {
		t := strings.Join(strings.Fields(*st.Title), " ")
		if t != "" {
			ch.Title = &t
		}
	}
	return nil
}
