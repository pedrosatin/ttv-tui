package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Fixtures do campo errors em respostas HTTP 200.
const (
	// O resolver de searchFor estourou o tempo: nada de data, erro transitório.
	searchServiceTimeoutFixture = `{"errors":[{"message":"service timeout","path":["searchFor"]}],"data":{"searchFor":null}}`

	searchOKFixture = `{"data":{"searchFor":{"channels":{"cursor":"","edges":[
		{"item":{"id":"1","login":"gaules","displayName":"Gaules","stream":{"id":"s1","viewersCount":10}}}
	]}}}}`

	// O stream do segundo canal e o item do terceiro falharam; o resto veio.
	searchPartialFixture = `{"errors":[
		{"message":"service timeout","path":["searchFor","channels","edges",1,"item","stream"]},
		{"message":"service timeout","path":["searchFor","channels","edges",2,"item"]}
	],"data":{"searchFor":{"channels":{"cursor":"","edges":[
		{"item":{"id":"1","login":"gaules","displayName":"Gaules","stream":{"id":"s1","viewersCount":10}}},
		{"item":{"id":"2","login":"gaulesclone","displayName":"GaulesClone","stream":null}},
		{"item":null},
		{"item":{"id":"4","login":"gaules_off","displayName":"Gaules Off","stream":null}}
	]}}}}`

	// O resolver do segundo jogo falhou; o primeiro veio.
	searchCategoriesPartialFixture = `{"errors":[
		{"message":"service timeout","path":["searchFor","games","edges",1,"item"]}
	],"data":{"searchFor":{"games":{"edges":[
		{"item":{"id":"516575","name":"VALORANT","viewersCount":5}},
		{"item":null}
	]}}}}`

	// O stream do primeiro login falhou; o segundo está offline de verdade.
	usersPartialFixture = `{"errors":[
		{"message":"service timeout","path":["users",0,"stream"]}
	],"data":{"users":[
		{"id":"1","login":"um","displayName":"Um","stream":null},
		{"id":"2","login":"dois","displayName":"Dois","stream":null}
	]}}`

	categoriesIntegrityFixture = `{"errors":[{"message":"failed integrity check","path":["games"],"extensions":{"code":"IntegrityCheckFailed"}}],"data":{"games":null}}`
	channelsIntegrityFixture   = `{"errors":[{"message":"failed integrity check","path":["game","streams"],"extensions":{"code":"IntegrityCheckFailed"}}],"data":{"game":{"id":"509658","name":"Just Chatting","streams":null}}}`
	searchIntegrityFixture     = `{"errors":[{"message":"failed integrity check","extensions":{"code":"IntegrityCheckFailed"}}],"data":{"searchFor":null}}`
	usersIntegrityFixture      = `{"errors":[{"message":"failed integrity check","extensions":{"code":"IntegrityCheckFailed"}}],"data":{"users":null}}`
)

func newFixtureSource(t *testing.T, bodies ...string) (*GQLSource, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(atomic.AddInt32(&calls, 1))
		if n > len(bodies) {
			n = len(bodies)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[n-1]))
	}))
	t.Cleanup(srv.Close)
	src := NewGQLSource(GQLConfig{
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
		Sleep:      func(context.Context, time.Duration) error { return nil },
	})
	return src, &calls
}

func TestGraphQLServiceTimeoutIsRetried(t *testing.T) {
	t.Run("a second attempt that succeeds returns the data", func(t *testing.T) {
		src, calls := newFixtureSource(t, searchServiceTimeoutFixture, searchOKFixture)
		page, err := src.Search(context.Background(), "gaules")
		if err != nil {
			t.Fatalf("Search after service timeout: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Login != "gaules" {
			t.Fatalf("items = %+v, want gaules", page.Items)
		}
		if got := atomic.LoadInt32(calls); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})

	t.Run("a persistent timeout is Servidor after one retry", func(t *testing.T) {
		src, calls := newFixtureSource(t, searchServiceTimeoutFixture)
		_, err := src.Search(context.Background(), "gaules")
		if KindOf(err) != Servidor {
			t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Servidor, err)
		}
		if got := atomic.LoadInt32(calls); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})
}

func TestGraphQLPartialErrorKeepsTheRest(t *testing.T) {
	t.Run("search", func(t *testing.T) {
		src, calls := newFixtureSource(t, searchPartialFixture)
		page, err := src.Search(context.Background(), "gaules")
		if err != nil {
			t.Fatalf("Search with a nested error: %v", err)
		}
		got := map[string]catalog.Availability{}
		for _, ch := range page.Items {
			got[ch.Login] = ch.Availability
		}
		want := map[string]catalog.Availability{
			"gaules":      catalog.AvailabilityLive,
			"gaulesclone": catalog.AvailabilityUnknown, // stream falhou: null não é offline
			"gaules_off":  catalog.AvailabilityOffline,
		}
		if len(got) != len(want) {
			t.Fatalf("items = %+v, want %v", page.Items, want)
		}
		for login, av := range want {
			if got[login] != av {
				t.Fatalf("%s availability = %v, want %v (items=%+v)", login, got[login], av, page.Items)
			}
		}
		if got := atomic.LoadInt32(calls); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})

	t.Run("search categories", func(t *testing.T) {
		src, calls := newFixtureSource(t, searchCategoriesPartialFixture)
		got, err := src.SearchCategories(context.Background(), "valorant")
		if err != nil {
			t.Fatalf("SearchCategories with a nested error: %v", err)
		}
		if len(got) != 1 || got[0].Name != "VALORANT" {
			t.Fatalf("got = %+v, want only VALORANT", got)
		}
		if got := atomic.LoadInt32(calls); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})

	t.Run("saved logins", func(t *testing.T) {
		src, calls := newFixtureSource(t, usersPartialFixture)
		got, err := src.ChannelsByLogins(context.Background(), []string{"um", "dois"})
		if err != nil {
			t.Fatalf("ChannelsByLogins with a nested error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got = %+v, want 2 channels", got)
		}
		if got[0].Availability != catalog.AvailabilityUnknown {
			t.Fatalf("um availability = %v, want unknown when its stream failed", got[0].Availability)
		}
		if got[1].Availability != catalog.AvailabilityOffline {
			t.Fatalf("dois availability = %v, want offline", got[1].Availability)
		}
		if got := atomic.LoadInt32(calls); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})
}

func TestIntegrityCheckFailedOnFirstPageIsAnError(t *testing.T) {
	cases := []struct {
		name string
		body string
		call func(*GQLSource) error
	}{
		{"categories", categoriesIntegrityFixture, func(s *GQLSource) error {
			_, err := s.Categories(context.Background(), catalog.PageRequest{Limit: 20})
			return err
		}},
		{"channels", channelsIntegrityFixture, func(s *GQLSource) error {
			_, err := s.Channels(context.Background(), "509658", catalog.PageRequest{Limit: 20})
			return err
		}},
		{"search", searchIntegrityFixture, func(s *GQLSource) error {
			_, err := s.Search(context.Background(), "gaules")
			return err
		}},
		{"search categories", searchIntegrityFixture, func(s *GQLSource) error {
			_, err := s.SearchCategories(context.Background(), "valorant")
			return err
		}},
		{"saved logins", usersIntegrityFixture, func(s *GQLSource) error {
			_, err := s.ChannelsByLogins(context.Background(), []string{"gaules"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, calls := newFixtureSource(t, tc.body)
			err := tc.call(src)
			if KindOf(err) != Contrato {
				t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Contrato, err)
			}
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Fatalf("attempts = %d, want 1", got)
			}
		})
	}
}

func TestChannelsByLoginsSplitsIntoBatchesOf100(t *testing.T) {
	var mu sync.Mutex
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		logins, _ := req.Variables["logins"].([]any)
		mu.Lock()
		batches = append(batches, len(logins))
		mu.Unlock()
		users := make([]string, 0, len(logins))
		for i, l := range logins {
			users = append(users, fmt.Sprintf(`{"id":"%d","login":%q,"displayName":%q,"stream":null}`, i, l, l))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"users":[%s]}}`, strings.Join(users, ","))
	}))
	defer srv.Close()

	logins := make([]string, 150)
	for i := range logins {
		logins[i] = fmt.Sprintf("canal%03d", i)
	}
	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	got, err := src.ChannelsByLogins(context.Background(), logins)
	if err != nil {
		t.Fatalf("ChannelsByLogins: %v", err)
	}
	if len(batches) != 2 || batches[0] != 100 || batches[1] != 50 {
		t.Fatalf("batches = %v, want [100 50]", batches)
	}
	if len(got) != 150 {
		t.Fatalf("len(got) = %d, want 150", len(got))
	}
	for i, ch := range got {
		if ch.Login != logins[i] || ch.Availability != catalog.AvailabilityOffline {
			t.Fatalf("got[%d] = %+v, want %s offline", i, ch, logins[i])
		}
	}
}

func TestGraphQLPermanentTimeoutFieldIsNotRetried(t *testing.T) {
	src, calls := newFixtureSource(t, `{"errors":[{"message":"Cannot query field \"requestTimeout\" on type \"Query\"."}]}`)
	_, err := src.Search(context.Background(), "gaules")
	if KindOf(err) != Contrato {
		t.Fatalf("error = %v, want Contrato", err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestGraphQLPartialPermanentErrorIsNotRetried(t *testing.T) {
	src, calls := newFixtureSource(t, strings.ReplaceAll(searchPartialFixture, "service timeout", "resolver failed"))
	page, err := src.Search(context.Background(), "gaules")
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("items = %+v, error = %v", page.Items, err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestGraphQLPartialErrorPathIndex(t *testing.T) {
	for _, tc := range []struct {
		name, index string
		want        ErrorKind
	}{
		{"numeric string", `"0"`, ""},
		{"out of range", "2", Contrato},
		{"out of range string", `"2"`, Contrato},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.ReplaceAll(usersPartialFixture, "service timeout", "resolver failed")
			body = strings.Replace(body, `["users",0,"stream"]`, `["users",`+tc.index+`,"stream"]`, 1)
			src, calls := newFixtureSource(t, body)
			items, err := src.ChannelsByLogins(context.Background(), []string{"um", "dois"})
			if KindOf(err) != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if tc.want == "" && (len(items) != 2 || items[0].Availability != catalog.AvailabilityUnknown) {
				t.Fatalf("items = %+v, want first unknown", items)
			}
			if tc.want != "" && len(items) != 0 {
				t.Fatalf("items = %+v, want none", items)
			}
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Fatalf("attempts = %d, want 1", got)
			}
		})
	}
}

func TestGraphQLPartialTimeoutRetriesAllLists(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		call       func(*GQLSource) error
	}{
		{"categories", `{"errors":[{"message":"SERVICE TIMEOUT","path":["games","edges",0,"node","viewersCount"]}],"data":{"games":{"edges":[{"node":{"id":"1","name":"Game"}}],"pageInfo":{}}}}`, func(s *GQLSource) error {
			_, err := s.Categories(context.Background(), catalog.PageRequest{Limit: 20})
			return err
		}},
		{"channels", `{"errors":[{"message":"service timeout","path":["game","streams","edges",0,"node","viewersCount"]}],"data":{"game":{"id":"1","name":"Game","streams":{"edges":[{"node":{"id":"s1","broadcaster":{"id":"1","login":"um","displayName":"Um"}}}],"pageInfo":{}}}}}`, func(s *GQLSource) error {
			_, err := s.Channels(context.Background(), "1", catalog.PageRequest{Limit: 20})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, calls := newFixtureSource(t, tc.body)
			if err := tc.call(src); err != nil {
				t.Fatal(err)
			}
			if got := atomic.LoadInt32(calls); got != 2 {
				t.Fatalf("attempts = %d, want 2", got)
			}
		})
	}
}

func TestGraphQLPartialTimeoutRetryReturnsFreshData(t *testing.T) {
	src, calls := newFixtureSource(t, searchPartialFixture, searchOKFixture)
	page, err := src.Search(context.Background(), "gaules")
	if err != nil || len(page.Items) != 1 || page.Items[0].Login != "gaules" {
		t.Fatalf("items = %+v, error = %v, want only gaules", page.Items, err)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}
