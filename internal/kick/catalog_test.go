package kick

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// catalogServer answers the catalog routes from a table keyed by path and
// keeps the query of each request. The three hosts of the site are one server
// here: the paths do not collide.
type catalogServer struct {
	*Client
	mu      sync.Mutex
	queries map[string][]url.Values
}

func newCatalogServer(t *testing.T, routes map[string]func(w http.ResponseWriter)) *catalogServer {
	t.Helper()
	s := &catalogServer{queries: make(map[string][]url.Values)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.queries[r.URL.Path] = append(s.queries[r.URL.Path], r.URL.Query())
		s.mu.Unlock()
		h, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h(w)
	}))
	t.Cleanup(srv.Close)
	s.Client = NewClient(ClientConfig{BaseURL: srv.URL, WebBaseURL: srv.URL, SearchBaseURL: srv.URL})
	return s
}

// asked returns the queries the route at path received, in order.
func (s *catalogServer) asked(path string) []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.queries[path]...)
}

// lastQuery returns the query of the only request the route at path received.
func (s *catalogServer) lastQuery(t *testing.T, path string) url.Values {
	t.Helper()
	asked := s.asked(path)
	if len(asked) != 1 {
		t.Fatalf("%s asked %d times, want 1", path, len(asked))
	}
	return asked[0]
}

const (
	categoriesPath     = "/api/v1/subcategories"
	channelsPath       = "/api/v1/livestreams"
	channelSearchPath  = "/api/v1/channels/search"
	categorySearchPath = "/api/v1/categories/search"
)

var (
	_ catalog.Browser  = (*Client)(nil)
	_ catalog.Searcher = (*Client)(nil)
)

func TestCategories_PageFixture(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body(fixture(t, "subcategories_page.json")),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(page.Items) != 6 {
		t.Fatalf("got %d categories, want 6: %+v", len(page.Items), page.Items)
	}
	first := page.Items[0]
	if first.ID != "15" || first.Name != "Just Chatting" || first.Viewers == nil || *first.Viewers != 477276 {
		t.Errorf("first category = %+v (viewers %v)", first, derefInt(first.Viewers))
	}
	if !page.HasMore || page.Cursor != "2" {
		t.Errorf("next page: HasMore=%v Cursor=%q, want true and 2", page.HasMore, page.Cursor)
	}
	q := s.lastQuery(t, categoriesPath)
	// The route serves 32 rows at most, whatever the list asks for.
	if q.Get("limit") != "32" || q.Get("page") != "1" {
		t.Errorf("query = %v, want limit=32 page=1", q)
	}
}

func TestCategories_CursorIsThePageNumber(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body([]byte(`{"current_page":3,"data":[{"id":7,"name":"Chess","viewers":12}],"next_page_url":"https://kick.com/api/v1/subcategories?page=4"}`)),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{Cursor: "3", Limit: 10})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	q := s.lastQuery(t, categoriesPath)
	if q.Get("page") != "3" || q.Get("limit") != "10" {
		t.Errorf("query = %v, want page=3 limit=10", q)
	}
	if !page.HasMore || page.Cursor != "4" {
		t.Errorf("next page: HasMore=%v Cursor=%q, want true and 4", page.HasMore, page.Cursor)
	}
}

// Past page 20 the site answers with page 20 again.
func TestCategories_ClampedPageEndsTheListing(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body(fixture(t, "subcategories_clamped.json")),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{Cursor: "21"})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(page.Items) != 0 || page.HasMore || page.Cursor != "" {
		t.Errorf("repeated page delivered again: %+v", page)
	}
}

func TestCategories_LastServedPageHasNoNext(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body(fixture(t, "subcategories_clamped.json")),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{Cursor: "20"})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("got %d categories, want 3", len(page.Items))
	}
	// The answer still names a page 21; asking for it returns this one.
	if page.HasMore || page.Cursor != "" {
		t.Errorf("page 20 offers a next page: %+v", page)
	}
}

// The route lists every category of the site; the live ones end at the first
// with no viewers.
func TestCategories_StopsAtTheFirstUnwatched(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body([]byte(`{"current_page":1,"data":[
			{"id":7,"name":"Chess","viewers":2},
			{"id":8,"name":"Quiet","viewers":0},
			{"id":9,"name":"Unknown"},
			{"id":0,"name":"No id","viewers":5},
			{"id":10,"name":"  ","viewers":5}
		],"next_page_url":"https://kick.com/api/v1/subcategories?page=2"}`)),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "Chess" {
		t.Errorf("categories = %+v, want only Chess", page.Items)
	}
	if page.HasMore {
		t.Error("a page that reached the unwatched categories offers a next one")
	}
}

// The page number is what tells a repeated page from a new one; an answer
// without it is not a listing the client can page through.
func TestCategories_AnswerWithoutThePageNumber(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath: body([]byte(`{"data":[{"id":7,"name":"Chess","viewers":2}]}`)),
	})
	page, err := s.Categories(context.Background(), catalog.PageRequest{})
	if catalog.KindOf(err) != catalog.Contrato {
		t.Fatalf("Categories = %+v, %v; want Contrato", page, err)
	}
}

// A renamed field leaves rows the parser cannot read. An answer with rows and
// none readable is a broken contract, not an empty listing: the list must say
// "unexpected response", not "no categories".
func TestCatalog_RenamedFieldIsAContractError(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categoriesPath:     body([]byte(`{"current_page":1,"next_page_url":null,"data":[{"id":7,"name":"Chess","viewer_total":2},{"id":8,"name":"Slots","viewer_total":1}]}`)),
		channelsPath:       body([]byte(`{"data":{"livestreams":[{"title":"t","viewer_count":9,"streamer":{"slug":"gaules"}}],"pagination":{"next_cursor":""}}}`)),
		channelSearchPath:  body([]byte(`{"data":[{"channel_slug":"gaules","is_live":true}]}`)),
		categorySearchPath: body([]byte(`{"data":[{"category_id":7,"title":"Chess"}]}`)),
	})
	ctx := context.Background()
	if page, err := s.Categories(ctx, catalog.PageRequest{}); catalog.KindOf(err) != catalog.Contrato {
		t.Errorf("Categories = %+v, %v; want Contrato", page, err)
	}
	if page, err := s.Channels(ctx, "1", catalog.PageRequest{}); catalog.KindOf(err) != catalog.Contrato {
		t.Errorf("Channels = %+v, %v; want Contrato", page, err)
	}
	if page, err := s.Search(ctx, "gaules"); catalog.KindOf(err) != catalog.Contrato {
		t.Errorf("Search = %+v, %v; want Contrato", page, err)
	}
	if cats, err := s.SearchCategories(ctx, "chess"); catalog.KindOf(err) != catalog.Contrato {
		t.Errorf("SearchCategories = %+v, %v; want Contrato", cats, err)
	}
}

func TestCategories_InvalidCursorAsksNothing(t *testing.T) {
	s := newCatalogServer(t, nil)
	for _, cursor := range []string{"abc", "0", "-1"} {
		_, err := s.Categories(context.Background(), catalog.PageRequest{Cursor: cursor})
		if catalog.KindOf(err) != catalog.Contrato {
			t.Errorf("cursor %q: err = %v, want Contrato", cursor, err)
		}
	}
	if n := len(s.asked(categoriesPath)); n != 0 {
		t.Errorf("asked the site %d times for an invalid cursor", n)
	}
}

func TestChannels_PageFixture(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelsPath: body(fixture(t, "livestreams_page.json")),
	})
	page, err := s.Channels(context.Background(), "28", catalog.PageRequest{Limit: 500})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("got %d channels, want 5", len(page.Items))
	}
	first := page.Items[0]
	if first.ID != "kick:timthetatman" || first.Login != "timthetatman" || first.DisplayName != "TimTheTatman" ||
		first.Platform != platform.Kick || first.URL != "https://kick.com/timthetatman" {
		t.Errorf("identity = %+v", first)
	}
	if first.Availability != catalog.AvailabilityLive || first.Viewers == nil || *first.Viewers != 5846 {
		t.Errorf("state = %v, viewers %d", first.Availability, derefInt(first.Viewers))
	}
	if deref(first.Title) != "$350k START - ROOBET TRIP DAY 2 #RoobetPartner" {
		t.Errorf("title = %q", deref(first.Title))
	}
	if deref(first.CategoryID) != "28" || deref(first.CategoryName) != "Slots & Casino" || deref(first.Language) != "en" {
		t.Errorf("category %q/%q language %q", deref(first.CategoryID), deref(first.CategoryName), deref(first.Language))
	}
	// The slug is what opens the channel; the name is only what the row shows.
	if second := page.Items[1]; second.Login != "danludan-88" || second.DisplayName != "danludan_88" {
		t.Errorf("slug and name mixed up: %+v", second)
	}
	// This channel hides its count on the site.
	if hidden := page.Items[3]; hidden.Login != "dobrodziej" || hidden.Viewers != nil || hidden.Availability != catalog.AvailabilityLive {
		t.Errorf("hidden count: %+v (viewers %d)", hidden, derefInt(hidden.Viewers))
	}
	if !page.HasMore || page.Cursor != "eyJvIjoxMDB9" {
		t.Errorf("next page: HasMore=%v Cursor=%q", page.HasMore, page.Cursor)
	}
	q := s.lastQuery(t, channelsPath)
	if q.Get("category_id") != "28" || q.Get("sort") != "viewer_count_desc" || q.Get("limit") != "100" || q.Has("after") {
		t.Errorf("query = %v, want category_id=28 sort=viewer_count_desc limit=100 and no cursor", q)
	}
}

func TestChannels_NextPageSendsTheCursor(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelsPath: body(fixture(t, "livestreams_last.json")),
	})
	page, err := s.Channels(context.Background(), "68", catalog.PageRequest{Cursor: "eyJvIjoxMDB9", Limit: 30})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	q := s.lastQuery(t, channelsPath)
	if q.Get("after") != "eyJvIjoxMDB9" || q.Get("limit") != "30" {
		t.Errorf("query = %v, want the cursor in after and limit=30", q)
	}
	if len(page.Items) != 3 || page.HasMore || page.Cursor != "" {
		t.Errorf("last page: %d channels, HasMore=%v Cursor=%q", len(page.Items), page.HasMore, page.Cursor)
	}
}

func TestChannels_EmptyCategory(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelsPath: body(fixture(t, "livestreams_empty.json")),
	})
	page, err := s.Channels(context.Background(), "999999999", catalog.PageRequest{})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.HasMore {
		t.Errorf("empty category: %+v", page)
	}
}

// The route answers 400 to a category that is not a number.
func TestChannels_InvalidCategoryAsksNothing(t *testing.T) {
	s := newCatalogServer(t, nil)
	for _, id := range []string{"", "slots", "-3", "0"} {
		_, err := s.Channels(context.Background(), id, catalog.PageRequest{})
		if catalog.KindOf(err) != catalog.Contrato {
			t.Errorf("category %q: err = %v, want Contrato", id, err)
		}
	}
	if n := len(s.asked(channelsPath)); n != 0 {
		t.Errorf("asked the site %d times for an invalid category", n)
	}
}

func TestChannels_SkipsRowsWithoutAChannel(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelsPath: body([]byte(`{"data":{"livestreams":[
			{"title":"no channel","viewer_count":9},
			{"title":"no slug","viewer_count":9,"channel":{"username":"Nobody"}},
			{"title":"  ","channel":{"slug":"Solo"}}
		],"pagination":{"next_cursor":"abc"}}}`)),
	})
	page, err := s.Channels(context.Background(), "1", catalog.PageRequest{})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d channels, want 1: %+v", len(page.Items), page.Items)
	}
	solo := page.Items[0]
	if solo.Login != "solo" || solo.DisplayName != "solo" || solo.Title != nil || solo.Viewers != nil || solo.CategoryName != nil || solo.Language != nil {
		t.Errorf("bare row = %+v", solo)
	}
	if solo.Availability != catalog.AvailabilityLive {
		t.Error("a channel in a live listing is live")
	}
}

func TestSearch_Fixture(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelSearchPath: body(fixture(t, "search_channels.json")),
	})
	page, err := s.Search(context.Background(), "  gaules ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) != 6 || page.HasMore {
		t.Fatalf("got %d hits (HasMore=%v), want 6 and no next page", len(page.Items), page.HasMore)
	}
	exact := page.Items[0]
	if exact.ID != "kick:gaules" || exact.Login != "gaules" || exact.DisplayName != "Gaules" ||
		exact.Platform != platform.Kick || exact.URL != "https://kick.com/gaules" {
		t.Errorf("exact hit = %+v", exact)
	}
	if exact.Availability != catalog.AvailabilityLive {
		t.Errorf("gaules availability = %v, want live", exact.Availability)
	}
	// The search says who is live and nothing about the stream.
	if exact.Viewers != nil || exact.Title != nil {
		t.Errorf("search hit carries stream data: %+v", exact)
	}
	if other := page.Items[1]; other.Login != "gaulestv" || other.Availability != catalog.AvailabilityOffline {
		t.Errorf("second hit = %+v", other)
	}
	q := s.lastQuery(t, channelSearchPath)
	if q.Get("query") != "gaules" || q.Get("limit") != "20" {
		t.Errorf("query = %v, want query=gaules limit=20", q)
	}
}

func TestSearch_OrdersExactNameThenLive(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelSearchPath: body([]byte(`{"data":[
			{"slug":"abcd","username":"abcd","is_live":false},
			{"slug":"abcx","username":"abcx","is_live":true},
			{"slug":"abc","username":"ABC","is_live":false},
			{"slug":"abcy","username":"abcy"}
		]}`)),
	})
	page, err := s.Search(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var order []string
	for _, ch := range page.Items {
		order = append(order, ch.Login)
	}
	want := []string{"abc", "abcx", "abcd", "abcy"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	// A hit without the flag proves neither state.
	if last := page.Items[3]; last.Availability != catalog.AvailabilityUnknown {
		t.Errorf("hit without is_live = %v, want unknown", last.Availability)
	}
}

func TestSearch_NoHits(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		channelSearchPath: body(fixture(t, "search_channels_empty.json")),
	})
	page, err := s.Search(context.Background(), "zzqqxxyyww1234")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Errorf("hits = %+v, want an empty list", page.Items)
	}
}

func TestSearchCategories_Fixture(t *testing.T) {
	s := newCatalogServer(t, map[string]func(http.ResponseWriter){
		categorySearchPath: body(fixture(t, "search_categories.json")),
	})
	cats, err := s.SearchCategories(context.Background(), "call of duty")
	if err != nil {
		t.Fatalf("SearchCategories: %v", err)
	}
	if len(cats) != 6 {
		t.Fatalf("got %d categories, want 6", len(cats))
	}
	// The exact name leads even with fewer viewers; the rest go by viewers.
	if cats[0].ID != "14755" || cats[0].Name != "Call of Duty" || derefInt(cats[0].Viewers) != 33 {
		t.Errorf("first = %+v (viewers %d)", cats[0], derefInt(cats[0].Viewers))
	}
	if cats[1].Name != "Call of Duty: Warzone" || derefInt(cats[1].Viewers) != 4298 {
		t.Errorf("second = %+v (viewers %d)", cats[1], derefInt(cats[1].Viewers))
	}
	if last := cats[5]; derefInt(last.Viewers) != 0 {
		t.Errorf("last = %+v, want a category with nobody live", last)
	}
	q := s.lastQuery(t, categorySearchPath)
	if q.Get("query") != "call of duty" || q.Get("limit") != "20" {
		t.Errorf("query = %v, want query=call of duty limit=20", q)
	}
}

func TestSearch_EmptyQueryAsksNothing(t *testing.T) {
	s := newCatalogServer(t, nil)
	page, err := s.Search(context.Background(), "   ")
	if err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Errorf("Search(blank) = %+v, %v", page, err)
	}
	cats, err := s.SearchCategories(context.Background(), "")
	if err != nil || cats == nil || len(cats) != 0 {
		t.Errorf("SearchCategories(blank) = %+v, %v", cats, err)
	}
	if n := len(s.asked(channelSearchPath)) + len(s.asked(categorySearchPath)); n != 0 {
		t.Errorf("asked the site %d times for a blank query", n)
	}
}

// catalogCalls runs each catalog operation against s and returns its error.
func catalogCalls(s *catalogServer) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		categoriesPath: func() error {
			_, err := s.Categories(ctx, catalog.PageRequest{})
			return err
		},
		channelsPath: func() error {
			_, err := s.Channels(ctx, "28", catalog.PageRequest{})
			return err
		},
		channelSearchPath: func() error {
			_, err := s.Search(ctx, "gaules")
			return err
		},
		categorySearchPath: func() error {
			_, err := s.SearchCategories(ctx, "chess")
			return err
		},
	}
}

func TestCatalog_ErrorKinds(t *testing.T) {
	tests := []struct {
		name string
		h    func(http.ResponseWriter)
		kind catalog.ErrorKind
		code int
	}{
		{"cloudflare 403", status(http.StatusForbidden), catalog.Bloqueio, http.StatusForbidden},
		{"route gone", status(http.StatusNotFound), catalog.Contrato, http.StatusNotFound},
		{"route asks for a login", status(http.StatusUnauthorized), catalog.Contrato, http.StatusUnauthorized},
		{"invalid request", status(http.StatusBadRequest), catalog.Contrato, http.StatusBadRequest},
		{"rate limit", status(http.StatusTooManyRequests), catalog.RateLimit, http.StatusTooManyRequests},
		{"server error", status(http.StatusInternalServerError), catalog.Servidor, http.StatusInternalServerError},
		{"challenge page", body([]byte("<html>Just a moment...</html>")), catalog.Contrato, 0},
		{"null data", body([]byte(`{"data":null}`)), catalog.Contrato, 0},
		{"other shape", body([]byte(`{"message":"ok"}`)), catalog.Contrato, 0},
	}
	for _, tc := range tests {
		for _, path := range []string{categoriesPath, channelsPath, channelSearchPath, categorySearchPath} {
			t.Run(tc.name+path, func(t *testing.T) {
				s := newCatalogServer(t, map[string]func(http.ResponseWriter){path: tc.h})
				err := catalogCalls(s)[path]()
				if catalog.KindOf(err) != tc.kind {
					t.Fatalf("err = %v, want kind %s", err, tc.kind)
				}
				var ce *catalog.Error
				if !errors.As(err, &ce) || ce.Platform != platform.Kick || ce.StatusCode != tc.code {
					t.Errorf("error = %+v, want platform kick and status %d", ce, tc.code)
				}
				// A catalog route that vanished is not a missing channel.
				if errors.Is(err, ErrChannelNotFound) {
					t.Error("catalog error wraps ErrChannelNotFound")
				}
			})
		}
	}
}

// refusedTransport fails every request the way a dropped connection does,
// without waiting for a real one to time out.
type refusedTransport struct{}

func (refusedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestCatalog_NetworkFailure(t *testing.T) {
	s := &catalogServer{Client: NewClient(ClientConfig{HTTPClient: &http.Client{Transport: refusedTransport{}}})}
	for path, call := range catalogCalls(s) {
		if err := call(); catalog.KindOf(err) != catalog.Rede {
			t.Errorf("%s: err = %v, want Rede", path, err)
		}
	}
}

// Each capability goes to its own host of the site.
func TestCatalog_HostOfEachRoute(t *testing.T) {
	hits := map[string]string{}
	var mu sync.Mutex
	host := func(name string, answer string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			hits[r.URL.Path] = name
			mu.Unlock()
			_, _ = w.Write([]byte(answer))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	c := NewClient(ClientConfig{
		BaseURL:       host("site", `{"current_page":1,"data":[]}`),
		WebBaseURL:    host("web", `{"data":{"livestreams":[]}}`),
		SearchBaseURL: host("search", `{"data":[]}`) + "/",
	})
	s := &catalogServer{Client: c}
	for path, call := range catalogCalls(s) {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	want := map[string]string{categoriesPath: "site", channelsPath: "web", channelSearchPath: "search", categorySearchPath: "search"}
	for path, name := range want {
		if hits[path] != name {
			t.Errorf("%s went to %q, want %q", path, hits[path], name)
		}
	}
}
