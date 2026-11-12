package kick

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// The catalog comes from three hosts of the site, none with a contract. The
// routes, their limits and the commands that proved them are in ADR 0017.
const (
	// DefaultWebBaseURL serves the live streams of a category.
	DefaultWebBaseURL = "https://web.kick.com"
	// DefaultSearchBaseURL serves the search of channels and of categories.
	DefaultSearchBaseURL = "https://search.kick.com"

	// maxCategoryLimit is the page size the categories route stops at: a larger
	// limit is answered with 32 rows.
	maxCategoryLimit = 32
	// maxCategoryPages is the last page the categories route serves: a later
	// page is answered with this one again.
	maxCategoryPages = 20
	// maxChannelLimit is the largest limit the live streams route accepts; 101
	// is answered with 400.
	maxChannelLimit = 100
	// searchLimit is how many hits one search asks for. The routes accept up to
	// 100 and have no next page.
	searchLimit = 20
	// channelSort is the order the site itself asks for in a category. The
	// route refuses a request without one.
	channelSort = "viewer_count_desc"
)

// subcategoriesEnvelope is the answer of /api/v1/subcategories: a page of the
// categories ordered by viewers.
type subcategoriesEnvelope struct {
	CurrentPage int `json:"current_page"`
	Data        *[]struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Viewers *int   `json:"viewers"`
	} `json:"data"`
	NextPageURL *string `json:"next_page_url"`
}

// livestreamsEnvelope is the answer of web.kick.com/api/v1/livestreams.
type livestreamsEnvelope struct {
	Data *struct {
		Livestreams *[]struct {
			Title         string `json:"title"`
			ViewerCount   *int   `json:"viewer_count"`
			ShowViewCount *bool  `json:"show_view_count"`
			Channel       *struct {
				Slug     string `json:"slug"`
				Username string `json:"username"`
			} `json:"channel"`
			Category *struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"category"`
			Language string `json:"language"`
		} `json:"livestreams"`
		Pagination *struct {
			NextCursor string `json:"next_cursor"`
		} `json:"pagination"`
	} `json:"data"`
}

// channelSearchEnvelope is the answer of search.kick.com/api/v1/channels/search.
// A hit says whether the channel is live and nothing about the stream.
type channelSearchEnvelope struct {
	Data *[]struct {
		Slug     string `json:"slug"`
		Username string `json:"username"`
		IsLive   *bool  `json:"is_live"`
	} `json:"data"`
}

// categorySearchEnvelope is the answer of search.kick.com/api/v1/categories/search.
type categorySearchEnvelope struct {
	Data *[]struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		ViewerCount *int   `json:"viewer_count"`
	} `json:"data"`
}

// Categories implements catalog.Browser: one page of the categories with the
// most viewers. The cursor is the number of the page to fetch. The route serves
// at most maxCategoryPages pages of maxCategoryLimit rows and lists every
// category of the site, so the listing ends at the first one nobody watches.
func (c *Client) Categories(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	const op = "categories"
	number := 1
	if cursor := strings.TrimSpace(page.Cursor); cursor != "" {
		n, err := strconv.Atoi(cursor)
		if err != nil || n < 1 {
			return catalog.Page[catalog.Category]{}, contractError(op, fmt.Errorf("invalid cursor %q", page.Cursor))
		}
		number = n
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(clampLimit(page.Limit, maxCategoryLimit)))
	query.Set("page", strconv.Itoa(number))
	body, err := c.getURL(ctx, op, c.baseURL+"/api/v1/subcategories?"+query.Encode(), nil)
	if err != nil {
		return catalog.Page[catalog.Category]{}, err
	}
	return parseCategories(number, body)
}

func parseCategories(requested int, body []byte) (catalog.Page[catalog.Category], error) {
	const op = "categories"
	var env subcategoriesEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return catalog.Page[catalog.Category]{}, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	if env.Data == nil {
		return catalog.Page[catalog.Category]{}, contractError(op, errors.New("answer without data"))
	}
	if env.CurrentPage <= 0 {
		// Without the page number a repeated page cannot be told from a new one.
		return catalog.Page[catalog.Category]{}, contractError(op, errors.New("answer without current_page"))
	}
	out := catalog.Page[catalog.Category]{Items: []catalog.Category{}}
	if env.CurrentPage != requested {
		// Past its last page the route answers with that page again. Those rows
		// were already delivered.
		return out, nil
	}
	watched := true
	readable := 0
	for _, row := range *env.Data {
		name := strings.TrimSpace(row.Name)
		if row.ID <= 0 || name == "" {
			continue
		}
		if row.Viewers != nil {
			readable++
		}
		if row.Viewers == nil || *row.Viewers <= 0 {
			watched = false
			continue
		}
		out.Items = append(out.Items, catalog.Category{ID: strconv.FormatInt(row.ID, 10), Name: name, Viewers: row.Viewers})
	}
	if err := unreadable(op, len(*env.Data), readable); err != nil {
		return catalog.Page[catalog.Category]{}, err
	}
	more := env.NextPageURL != nil && strings.TrimSpace(*env.NextPageURL) != ""
	if more && watched && len(out.Items) > 0 && requested < maxCategoryPages {
		out.Cursor = strconv.Itoa(requested + 1)
		out.HasMore = true
	}
	return out, nil
}

// Channels implements catalog.Browser: one page of the channels live in the
// category, most watched first. categoryID is the number Categories and
// SearchCategories return; the route refuses anything else.
func (c *Client) Channels(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	const op = "channels"
	id := strings.TrimSpace(categoryID)
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
		return catalog.Page[catalog.Channel]{}, contractError(op, fmt.Errorf("invalid category id %q", categoryID))
	}
	query := url.Values{}
	query.Set("limit", strconv.Itoa(clampLimit(page.Limit, maxChannelLimit)))
	query.Set("sort", channelSort)
	query.Set("category_id", id)
	if cursor := strings.TrimSpace(page.Cursor); cursor != "" {
		query.Set("after", cursor)
	}
	body, err := c.getURL(ctx, op, c.webBaseURL+"/api/v1/livestreams?"+query.Encode(), nil)
	if err != nil {
		return catalog.Page[catalog.Channel]{}, err
	}
	return parseChannels(body)
}

func parseChannels(body []byte) (catalog.Page[catalog.Channel], error) {
	const op = "channels"
	var env livestreamsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return catalog.Page[catalog.Channel]{}, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	if env.Data == nil || env.Data.Livestreams == nil {
		return catalog.Page[catalog.Channel]{}, contractError(op, errors.New("answer without livestreams"))
	}
	out := catalog.Page[catalog.Channel]{Items: []catalog.Channel{}}
	for _, row := range *env.Data.Livestreams {
		if row.Channel == nil {
			continue
		}
		ch, ok := listedChannel(row.Channel.Slug, row.Channel.Username)
		if !ok {
			continue
		}
		ch.Availability = catalog.AvailabilityLive
		// A channel may hide its count; the site then shows none, and so does the list.
		if row.ShowViewCount == nil || *row.ShowViewCount {
			ch.Viewers = row.ViewerCount
		}
		if title := strings.TrimSpace(row.Title); title != "" {
			ch.Title = &title
		}
		if cat := row.Category; cat != nil && strings.TrimSpace(cat.Name) != "" {
			ch.CategoryID = catalog.StringPtr(strconv.FormatInt(cat.ID, 10))
			ch.CategoryName = catalog.StringPtr(strings.TrimSpace(cat.Name))
		}
		ch.Language = catalog.LanguagePtr(row.Language)
		out.Items = append(out.Items, ch)
	}
	if err := unreadable(op, len(*env.Data.Livestreams), len(out.Items)); err != nil {
		return catalog.Page[catalog.Channel]{}, err
	}
	if p := env.Data.Pagination; p != nil && strings.TrimSpace(p.NextCursor) != "" && len(out.Items) > 0 {
		out.Cursor = strings.TrimSpace(p.NextCursor)
		out.HasMore = true
	}
	return out, nil
}

// Search implements catalog.Searcher: the channels whose name matches query,
// live or not. The route reports only whether each one is live; viewers and
// title come from the livestream lookup when a hit is opened.
func (c *Client) Search(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
	const op = "search"
	q := strings.TrimSpace(query)
	if q == "" {
		return catalog.Page[catalog.Channel]{Items: []catalog.Channel{}}, nil
	}
	body, err := c.getURL(ctx, op, c.searchBaseURL+"/api/v1/channels/search?"+searchQuery(q), nil)
	if err != nil {
		return catalog.Page[catalog.Channel]{}, err
	}
	return parseChannelSearch(q, body)
}

func parseChannelSearch(query string, body []byte) (catalog.Page[catalog.Channel], error) {
	const op = "search"
	var env channelSearchEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return catalog.Page[catalog.Channel]{}, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	if env.Data == nil {
		return catalog.Page[catalog.Channel]{}, contractError(op, errors.New("answer without data"))
	}
	items := make([]catalog.Channel, 0, len(*env.Data))
	for _, row := range *env.Data {
		ch, ok := listedChannel(row.Slug, row.Username)
		if !ok {
			continue
		}
		// A hit without the flag proves neither live nor offline.
		if row.IsLive != nil {
			ch.Availability = catalog.AvailabilityOffline
			if *row.IsLive {
				ch.Availability = catalog.AvailabilityLive
			}
		}
		items = append(items, ch)
	}
	if err := unreadable(op, len(*env.Data), len(items)); err != nil {
		return catalog.Page[catalog.Channel]{}, err
	}
	return catalog.Page[catalog.Channel]{Items: catalog.PreferSearchChannels(query, items)}, nil
}

// SearchCategories implements catalog.Browser: the categories whose name
// matches query, with or without anyone live.
func (c *Client) SearchCategories(ctx context.Context, query string) ([]catalog.Category, error) {
	const op = "searchCategories"
	q := strings.TrimSpace(query)
	if q == "" {
		return []catalog.Category{}, nil
	}
	body, err := c.getURL(ctx, op, c.searchBaseURL+"/api/v1/categories/search?"+searchQuery(q), nil)
	if err != nil {
		return nil, err
	}
	return parseCategorySearch(q, body)
}

func parseCategorySearch(query string, body []byte) ([]catalog.Category, error) {
	const op = "searchCategories"
	var env categorySearchEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, contractError(op, fmt.Errorf("decode response: %w", err))
	}
	if env.Data == nil {
		return nil, contractError(op, errors.New("answer without data"))
	}
	items := make([]catalog.Category, 0, len(*env.Data))
	for _, row := range *env.Data {
		name := strings.TrimSpace(row.Name)
		if row.ID <= 0 || name == "" {
			continue
		}
		items = append(items, catalog.Category{ID: strconv.FormatInt(row.ID, 10), Name: name, Viewers: row.ViewerCount})
	}
	if err := unreadable(op, len(*env.Data), len(items)); err != nil {
		return nil, err
	}
	return catalog.PreferSearchCategories(query, items), nil
}

// unreadable reports an answer with rows of which none could be read: a field
// the parser needs was renamed or dropped. That is a broken contract, not an
// empty listing, and the list says so instead of "no channels".
func unreadable(op string, rows, read int) error {
	if rows > 0 && read == 0 {
		return contractError(op, fmt.Errorf("none of the %d rows has the fields the app reads", rows))
	}
	return nil
}

// listedChannel builds the identity of a channel a listing names. A row without
// a usable slug is no channel the app could open.
func listedChannel(slug, username string) (catalog.Channel, bool) {
	slug = Slug(catalog.Channel{Platform: platform.Kick, Login: slug})
	if slug == "" {
		return catalog.Channel{}, false
	}
	name := strings.TrimSpace(username)
	if name == "" {
		name = slug
	}
	return catalog.Channel{
		ID:          platform.Kick + ":" + slug,
		Login:       slug,
		DisplayName: name,
		Platform:    platform.Kick,
		URL:         channelURL(slug),
	}, true
}

func searchQuery(q string) string {
	query := url.Values{}
	query.Set("query", q)
	query.Set("limit", strconv.Itoa(searchLimit))
	return query.Encode()
}

// clampLimit keeps a requested page size within what a route serves.
func clampLimit(limit, ceiling int) int {
	if limit <= 0 || limit > ceiling {
		return ceiling
	}
	return limit
}
