package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCategoriesSuccessSortedAndOptionalViewers(t *testing.T) {
	var gotClientID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID = r.Header.Get("Client-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"games": {
					"edges": [
						{"cursor": "cur-1", "node": {"id": "33214", "name": "Fortnite", "viewersCount": 120000}},
						{"cursor": "cur-2", "node": {"id": "99999", "name": "Mystery Game", "viewersCount": null}},
						{"cursor": "cur-3", "node": {"id": "509658", "name": "Just Chatting", "viewersCount": 560000}},
						{"cursor": "cur-4", "node": {"id": "21779", "name": "League of Legends", "viewersCount": 430000}}
					],
					"pageInfo": {"hasNextPage": true}
				}
			}
		}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})

	page, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 4})
	if err != nil {
		t.Fatalf("Categories returned unexpected error: %v", err)
	}
	if gotClientID != DefaultClientID {
		t.Fatalf("Client-Id header = %q, want %q", gotClientID, DefaultClientID)
	}
	if !page.HasMore {
		t.Fatal("expected page.HasMore to be true")
	}
	if page.Cursor != "cur-4" {
		t.Fatalf("page.Cursor = %q, want %q", page.Cursor, "cur-4")
	}
	if len(page.Items) != 4 {
		t.Fatalf("len(page.Items) = %d, want 4", len(page.Items))
	}

	wantOrder := []struct {
		id      string
		name    string
		viewers *int
	}{
		{id: "509658", name: "Just Chatting", viewers: catalog.IntPtr(560000)},
		{id: "21779", name: "League of Legends", viewers: catalog.IntPtr(430000)},
		{id: "33214", name: "Fortnite", viewers: catalog.IntPtr(120000)},
		{id: "99999", name: "Mystery Game", viewers: nil},
	}

	for i, want := range wantOrder {
		got := page.Items[i]
		if got.ID != want.id || got.Name != want.name {
			t.Fatalf("item[%d] = (%q, %q), want (%q, %q)", i, got.ID, got.Name, want.id, want.name)
		}
		if want.viewers == nil {
			if got.Viewers != nil {
				t.Fatalf("item[%d].Viewers = %v, want nil", i, *got.Viewers)
			}
		} else {
			if got.Viewers == nil || *got.Viewers != *want.viewers {
				t.Fatalf("item[%d].Viewers = %v, want %d", i, got.Viewers, *want.viewers)
			}
		}
	}
}

func TestCategoriesClampsLimitToMaxGamesFirst(t *testing.T) {
	var gotN int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch n := req.Variables["n"].(type) {
		case float64:
			gotN = int(n)
		case int:
			gotN = n
		default:
			t.Errorf("variables[n] type %T = %v", req.Variables["n"], req.Variables["n"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"games":{"edges":[{"cursor":"c1","node":{"id":"1","name":"Just Chatting","viewersCount":1}}],"pageInfo":{"hasNextPage":false}}}}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})
	if _, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 120}); err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if gotN != MaxGamesFirst {
		t.Fatalf("games(first:) n = %d, want clamped %d", gotN, MaxGamesFirst)
	}
}

func TestCategoriesPaginationPassesCursor(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum := atomic.AddInt32(&calls, 1)
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if callNum == 1 {
			if _, hasAfter := req.Variables["after"]; hasAfter {
				t.Errorf("first page unexpectedly sent after=%v", req.Variables["after"])
			}
			_, _ = w.Write([]byte(`{
				"data": {
					"games": {
						"edges": [
							{"cursor": "cursor-page-1", "node": {"id": "1", "name": "Cat One", "viewersCount": 900}}
						],
						"pageInfo": {"hasNextPage": true}
					}
				}
			}`))
			return
		}
		if gotAfter := req.Variables["after"]; gotAfter != "cursor-page-1" {
			t.Errorf("second page variables[after] = %v, want %q", gotAfter, "cursor-page-1")
		}
		_, _ = w.Write([]byte(`{
			"data": {
				"games": {
					"edges": [
						{"cursor": "cursor-page-2", "node": {"id": "2", "name": "Cat Two", "viewersCount": 400}}
					],
					"pageInfo": {"hasNextPage": false}
				}
			}
		}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})

	p1, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 1})
	if err != nil {
		t.Fatalf("page 1 error: %v", err)
	}
	if !p1.HasMore || p1.Cursor != "cursor-page-1" {
		t.Fatalf("page 1 = (%+v), want HasMore=true, Cursor=cursor-page-1", p1)
	}

	p2, err := src.Categories(context.Background(), catalog.PageRequest{Cursor: p1.Cursor, Limit: 1})
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if p2.HasMore {
		t.Fatal("expected page 2 HasMore=false")
	}
	if len(p2.Items) != 1 || p2.Items[0].ID != "2" {
		t.Fatalf("unexpected page 2 items: %+v", p2.Items)
	}
}

func TestCategoriesRateLimit429RespectsReset(t *testing.T) {
	baseNow := time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
	resetUnix := baseNow.Add(12 * time.Second).Unix()

	t.Run("waits for Ratelimit-Reset and succeeds on retry", func(t *testing.T) {
		var attempts int32
		var slept []time.Duration
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				w.Header().Set("Ratelimit-Reset", strconv.FormatInt(resetUnix, 10))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"games": {
						"edges": [{"cursor": "c1", "node": {"id": "10", "name": "Dota 2", "viewersCount": 50000}}],
						"pageInfo": {"hasNextPage": false}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Now:        func() time.Time { return baseNow },
			Sleep: func(_ context.Context, d time.Duration) error {
				slept = append(slept, d)
				return nil
			},
		})

		page, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
		if err != nil {
			t.Fatalf("expected retry after Ratelimit-Reset to succeed, got: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].ID != "10" {
			t.Fatalf("unexpected items: %+v", page.Items)
		}
		if len(slept) != 1 || slept[0] != 12*time.Second {
			t.Fatalf("slept durations = %v, want [12s]", slept)
		}
	})

	t.Run("persistent 429 returns RateLimit error with ResetAfter", func(t *testing.T) {
		var attempts int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.Header().Set("Ratelimit-Reset", strconv.FormatInt(resetUnix, 10))
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Now:        func() time.Time { return baseNow },
			Sleep:      func(_ context.Context, _ time.Duration) error { return nil },
		})

		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
		if err == nil {
			t.Fatal("expected error on persistent 429, got nil")
		}
		var te *Error
		if !errors.As(err, &te) {
			t.Fatalf("expected *twitch.Error, got %T (%v)", err, err)
		}
		if te.Kind != RateLimit {
			t.Fatalf("error Kind = %q, want %q", te.Kind, RateLimit)
		}
		if te.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("StatusCode = %d, want 429", te.StatusCode)
		}
		if te.ResetAfter != 12*time.Second {
			t.Fatalf("ResetAfter = %v, want 12s", te.ResetAfter)
		}
	})

	t.Run("429 without Ratelimit-Reset does not retry", func(t *testing.T) {
		var attempts int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Sleep: func(_ context.Context, _ time.Duration) error {
				t.Fatal("unexpected sleep when Ratelimit-Reset is absent")
				return nil
			},
		})

		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
		if KindOf(err) != RateLimit {
			t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), RateLimit, err)
		}
		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Fatalf("attempts = %d, want 1", got)
		}
	})
}

func TestCategoriesServer500RetriesOnceWithBackoff(t *testing.T) {
	t.Run("recovers when second attempt succeeds", func(t *testing.T) {
		var attempts int32
		var slept []time.Duration
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&attempts, 1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"games": {
						"edges": [{"cursor": "c1", "node": {"id": "1", "name": "Valorant", "viewersCount": 80000}}],
						"pageInfo": {"hasNextPage": false}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:     srv.URL,
			HTTPClient:   srv.Client(),
			RetryBackoff: 75 * time.Millisecond,
			Sleep: func(_ context.Context, d time.Duration) error {
				slept = append(slept, d)
				return nil
			},
		})

		page, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 5})
		if err != nil {
			t.Fatalf("unexpected error on recovered 500: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Name != "Valorant" {
			t.Fatalf("unexpected items: %+v", page.Items)
		}
		if got := atomic.LoadInt32(&attempts); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
		if len(slept) != 1 || slept[0] != 75*time.Millisecond {
			t.Fatalf("slept = %v, want [75ms]", slept)
		}
	})

	t.Run("fails with Servidor after single retry", func(t *testing.T) {
		var attempts int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Sleep:      func(_ context.Context, _ time.Duration) error { return nil },
		})

		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 5})
		if KindOf(err) != Servidor {
			t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Servidor, err)
		}
		if got := atomic.LoadInt32(&attempts); got != 2 {
			t.Fatalf("attempts = %d, want 2 (initial + 1 retry)", got)
		}
	})
}

func TestCategoriesPartialJSONAndContractErrorsDoNotRetry(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "truncated partial json",
			body: `{"data":{"games":{"edges":[{"cursor":"c1","node":{"id":"1","name":"Broken"`,
		},
		{
			name: "graphql errors array",
			body: `{"errors":[{"message":"PersistedQueryNotFound"}]}`,
		},
		{
			name: "missing data.games",
			body: `{"data":{"games":null}}`,
		},
		{
			name: "missing node identity",
			body: `{"data":{"games":{"edges":[{"cursor":"c1","node":{"id":"","name":"NoID"}}],"pageInfo":{"hasNextPage":false}}}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&attempts, 1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			src := NewGQLSource(GQLConfig{
				Endpoint:   srv.URL,
				HTTPClient: srv.Client(),
				Sleep: func(_ context.Context, _ time.Duration) error {
					t.Fatal("Contrato error must not trigger retry backoff")
					return nil
				},
			})

			_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 5})
			if KindOf(err) != Contrato {
				t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Contrato, err)
			}
			if got := atomic.LoadInt32(&attempts); got != 1 {
				t.Fatalf("attempts = %d, want 1 (no retry on Contrato)", got)
			}
		})
	}
}

// stallingTransport is an http.RoundTripper that never touches the network. Each
// call is counted synchronously, then blocks until the request context ends and
// returns that context's error, like an upstream that accepted the connection and
// never answered. Counting happens before any wait, so the number of attempts does
// not depend on scheduling, connection setup or how fast a server goroutine runs.
type stallingTransport struct {
	calls  atomic.Int32
	onCall func(n int32)

	mu        sync.Mutex
	deadlines []time.Time
}

func (tr *stallingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	n := tr.calls.Add(1)
	if dl, ok := req.Context().Deadline(); ok {
		tr.mu.Lock()
		tr.deadlines = append(tr.deadlines, dl)
		tr.mu.Unlock()
	}
	if tr.onCall != nil {
		tr.onCall(n)
	}
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// stallingSource builds a GQLSource over a stallingTransport with a short
// per-request timeout and a Sleep that records the retry backoff without waiting.
func stallingSource(tr *stallingTransport, slept *[]time.Duration) *GQLSource {
	return NewGQLSource(GQLConfig{
		Endpoint:       "http://gql.invalid/gql",
		HTTPClient:     &http.Client{Transport: tr},
		RequestTimeout: 5 * time.Millisecond,
		RetryBackoff:   7 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) error {
			*slept = append(*slept, d)
			return nil
		},
	})
}

// assertTimeoutRetriedOnce checks the contract of a per-request timeout: exactly
// two attempts, each with its own fresh deadline, one backoff between them, and
// the final error classified as Rede while still wrapping DeadlineExceeded.
func assertTimeoutRetriedOnce(t *testing.T, label string, tr *stallingTransport, slept []time.Duration, err error) {
	t.Helper()
	if KindOf(err) != Rede {
		t.Fatalf("%s: KindOf(err) = %q, want %q (err=%v)", label, KindOf(err), Rede, err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%s: err = %v, want it to wrap context.DeadlineExceeded", label, err)
	}
	if got := tr.calls.Load(); got != 2 {
		t.Fatalf("%s: attempts = %d, want 2", label, got)
	}
	tr.mu.Lock()
	deadlines := append([]time.Time(nil), tr.deadlines...)
	tr.mu.Unlock()
	if len(deadlines) != 2 {
		t.Fatalf("%s: requests with deadline = %d, want 2 (per-request timeout on every attempt)", label, len(deadlines))
	}
	if !deadlines[1].After(deadlines[0]) {
		t.Fatalf("%s: retry deadline %v not after first deadline %v; retry must get a fresh timeout", label, deadlines[1], deadlines[0])
	}
	if len(slept) != 1 || slept[0] != 7*time.Millisecond {
		t.Fatalf("%s: slept = %v, want [7ms] (single backoff before the retry)", label, slept)
	}
}

func TestCategoriesTimeoutAndContextCancellation(t *testing.T) {
	t.Run("per-request timeout retries once and returns Rede", func(t *testing.T) {
		tr := &stallingTransport{}
		var slept []time.Duration
		src := stallingSource(tr, &slept)

		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 5})
		assertTimeoutRetriedOnce(t, "Categories", tr, slept, err)
	})

	t.Run("caller context cancellation aborts without retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// The caller cancels while the first attempt is in flight. The
		// per-request timeout is far away, so only the caller can end it.
		tr := &stallingTransport{onCall: func(int32) { cancel() }}

		src := NewGQLSource(GQLConfig{
			Endpoint:       "http://gql.invalid/gql",
			HTTPClient:     &http.Client{Transport: tr},
			RequestTimeout: time.Hour,
			Sleep: func(_ context.Context, _ time.Duration) error {
				t.Error("canceled caller context must not retry")
				return nil
			},
		})

		_, err := src.Categories(ctx, catalog.PageRequest{Limit: 5})
		if KindOf(err) != Rede {
			t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Rede, err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want it to wrap context.Canceled", err)
		}
		if got := tr.calls.Load(); got != 1 {
			t.Fatalf("attempts = %d, want 1", got)
		}
	})
}

func TestChannelsSuccessAndEmptyFixtures(t *testing.T) {
	t.Run("success parses login displayName viewers title and caches", func(t *testing.T) {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			var req gqlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if gotID := req.Variables["id"]; gotID != "509658" {
				t.Errorf("variables[id] = %v, want 509658", gotID)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"game": {
						"id": "509658",
						"name": "Just Chatting",
						"streams": {
							"edges": [
								{
									"cursor": "ch-cur-1",
									"node": {
										"id": "stream-1",
										"broadcaster": {"id": "181077473", "login": "gaules", "displayName": "Gaules"},
										"viewersCount": 42500,
										"title": "  Major   CS2   Ao Vivo \n\t !loja  "
									}
								},
								{
									"cursor": "ch-cur-2",
									"node": {
										"broadcaster": {"login": "nobru", "displayName": "Nobru"},
										"viewersCount": null,
										"title": null
									}
								}
							],
							"pageInfo": {"hasNextPage": true}
						}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			CacheTTL:   time.Minute,
		})

		page, err := src.Channels(context.Background(), "509658", catalog.PageRequest{Limit: 10})
		if err != nil {
			t.Fatalf("Channels returned unexpected error: %v", err)
		}
		if !page.HasMore || page.Cursor != "ch-cur-2" {
			t.Fatalf("page pagination = (HasMore=%v, Cursor=%q), want (true, ch-cur-2)", page.HasMore, page.Cursor)
		}
		if len(page.Items) != 2 {
			t.Fatalf("len(page.Items) = %d, want 2", len(page.Items))
		}

		first := page.Items[0]
		if first.ID != "181077473" || first.Login != "gaules" || first.DisplayName != "Gaules" {
			t.Fatalf("first identity = %+v, want ID=181077473 Login=gaules DisplayName=Gaules", first)
		}
		if first.Viewers == nil || *first.Viewers != 42500 {
			t.Fatalf("first.Viewers = %v, want 42500", first.Viewers)
		}
		if first.Availability != catalog.AvailabilityLive {
			t.Fatalf("first.Availability = %v, want catalog.AvailabilityLive (game streams are live)", first.Availability)
		}
		if first.Title == nil || *first.Title != "Major CS2 Ao Vivo !loja" {
			t.Fatalf("first.Title = %v, want normalized title", first.Title)
		}
		if first.CategoryID == nil || *first.CategoryID != "509658" {
			t.Fatalf("first.CategoryID = %v, want 509658", first.CategoryID)
		}
		if first.CategoryName == nil || *first.CategoryName != "Just Chatting" {
			t.Fatalf("first.CategoryName = %v, want Just Chatting", first.CategoryName)
		}

		second := page.Items[1]
		if second.ID != "nobru" || second.Login != "nobru" || second.DisplayName != "Nobru" {
			t.Fatalf("second identity = %+v, want fallback ID=nobru Login=nobru DisplayName=Nobru", second)
		}
		if second.Viewers != nil || second.Title != nil {
			t.Fatalf("second optional metadata = (Viewers=%v, Title=%v), want both nil", second.Viewers, second.Title)
		}

		// Repeat call hits in-memory cache.
		if _, err := src.Channels(context.Background(), "509658", catalog.PageRequest{Limit: 10}); err != nil {
			t.Fatalf("cached Channels error: %v", err)
		}
		if got := atomic.LoadInt32(&hits); got != 1 {
			t.Fatalf("hits = %d, want 1", got)
		}
	})

	t.Run("empty category returns empty slice and HasMore false without error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"game": {
						"id": "509658",
						"name": "Empty catalog.Category",
						"streams": {
							"edges": [],
							"pageInfo": {"hasNextPage": false}
						}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		page, err := src.Channels(context.Background(), "Empty catalog.Category", catalog.PageRequest{Limit: 20})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(page.Items) != 0 || page.HasMore || page.Cursor != "" {
			t.Fatalf("empty page = %+v, want 0 items, HasMore=false, empty cursor", page)
		}
	})
}

func TestChannelsErrorClassificationsAndIntegrityCheck(t *testing.T) {
	baseNow := time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
	resetUnix := baseNow.Add(8 * time.Second).Unix()

	t.Run("429 respects Ratelimit-Reset and retries once", func(t *testing.T) {
		var attempts int32
		var slept []time.Duration
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&attempts, 1) == 1 {
				w.Header().Set("Ratelimit-Reset", strconv.FormatInt(resetUnix, 10))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"game":{"id":"1","name":"G","streams":{"edges":[{"cursor":"c1","node":{"broadcaster":{"id":"10","login":"ch1","displayName":"Ch1"},"viewersCount":100,"title":"Live"}}],"pageInfo":{"hasNextPage":false}}}}}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Now:        func() time.Time { return baseNow },
			Sleep: func(_ context.Context, d time.Duration) error {
				slept = append(slept, d)
				return nil
			},
		})

		page, err := src.Channels(context.Background(), "1", catalog.PageRequest{Limit: 5})
		if err != nil {
			t.Fatalf("expected retry after 429 to succeed, got: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Login != "ch1" {
			t.Fatalf("unexpected items: %+v", page.Items)
		}
		if len(slept) != 1 || slept[0] != 8*time.Second {
			t.Fatalf("slept = %v, want [8s]", slept)
		}
	})

	t.Run("500 retries once and returns Servidor on persistent failure", func(t *testing.T) {
		var attempts int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Sleep:      func(_ context.Context, _ time.Duration) error { return nil },
		})

		_, err := src.Channels(context.Background(), "509658", catalog.PageRequest{Limit: 5})
		if KindOf(err) != Servidor {
			t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Servidor, err)
		}
		if got := atomic.LoadInt32(&attempts); got != 2 {
			t.Fatalf("attempts = %d, want 2", got)
		}
	})

	t.Run("partial JSON and contract violations return Contrato without retry", func(t *testing.T) {
		cases := []struct {
			name string
			body string
		}{
			{name: "partial json", body: `{"data":{"game":{"streams":`},
			{name: "graphql error", body: `{"errors":[{"message":"PersistedQueryNotFound"}]}`},
			{name: "missing game", body: `{"data":{"game":null}}`},
			{name: "null edge node", body: `{"data":{"game":{"streams":{"edges":[{"node":null}],"pageInfo":{"hasNextPage":false}}}}}`},
			{name: "missing broadcaster login", body: `{"data":{"game":{"streams":{"edges":[{"node":{"broadcaster":{"login":"","displayName":"NoLogin"}}}],"pageInfo":{"hasNextPage":false}}}}}`},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var attempts int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&attempts, 1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
				}))
				defer srv.Close()

				src := NewGQLSource(GQLConfig{
					Endpoint:   srv.URL,
					HTTPClient: srv.Client(),
					Sleep: func(_ context.Context, _ time.Duration) error {
						t.Fatal("Contrato error must not trigger retry")
						return nil
					},
				})

				_, err := src.Channels(context.Background(), "509658", catalog.PageRequest{Limit: 5})
				if KindOf(err) != Contrato {
					t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Contrato, err)
				}
				if got := atomic.LoadInt32(&attempts); got != 1 {
					t.Fatalf("attempts = %d, want 1", got)
				}
			})
		}
	})

	t.Run("IntegrityCheckFailed returns empty page with HasMore false and no error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errors":[{"message":"failed integrity check","path":["game","streams"],"extensions":{"code":"IntegrityCheckFailed"}}],"data":{"game":{"id":"509658","name":"Just Chatting","streams":null}}}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		page, err := src.Channels(context.Background(), "509658", catalog.PageRequest{Cursor: "c1", Limit: 20})
		if err != nil {
			t.Fatalf("unexpected error on IntegrityCheckFailed: %v", err)
		}
		if len(page.Items) != 0 || page.HasMore || page.Cursor != "" {
			t.Fatalf("page = %+v, want empty items, HasMore=false, Cursor=\"\"", page)
		}
	})
}

func TestSearchFixturesSuccessEmptyAndErrors(t *testing.T) {
	t.Run("success uses inline fragment on User and returns displayName and login", func(t *testing.T) {
		var gotQuery, gotVarQ string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req gqlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			gotQuery = req.Query
			if q, ok := req.Variables["q"].(string); ok {
				gotVarQ = q
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"searchFor": {
						"channels": {
							"cursor": "MTA=",
							"edges": [
								{"item": {"id": "181077473", "login": "gaules", "displayName": "Gaules"}},
								{"item": {"login": "gaulestv", "displayName": "gaulestv"}}
							]
						}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		page, err := src.Search(context.Background(), "  gaules  ")
		if err != nil {
			t.Fatalf("Search returned unexpected error: %v", err)
		}
		if !strings.Contains(gotQuery, "... on User") {
			t.Fatalf("expected inline fragment '... on User' in query, got: %s", gotQuery)
		}
		if gotVarQ != "gaules" {
			t.Fatalf("variables[q] = %q, want %q", gotVarQ, "gaules")
		}
		if page.HasMore {
			t.Fatalf("page.HasMore = %v, want false", page.HasMore)
		}
		if len(page.Items) != 2 {
			t.Fatalf("len(page.Items) = %d, want 2", len(page.Items))
		}
		if page.Items[0].ID != "181077473" || page.Items[0].Login != "gaules" || page.Items[0].DisplayName != "Gaules" {
			t.Fatalf("item[0] = %+v, want ID=181077473 Login=gaules DisplayName=Gaules", page.Items[0])
		}
		if page.Items[1].ID != "gaulestv" || page.Items[1].Login != "gaulestv" || page.Items[1].DisplayName != "gaulestv" {
			t.Fatalf("item[1] = %+v, want ID=gaulestv Login=gaulestv DisplayName=gaulestv", page.Items[1])
		}
	})

	t.Run("empty search result returns empty slice without error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"cursor":"","edges":[]}}}}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		page, err := src.Search(context.Background(), "nonexistent_xyz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(page.Items) != 0 || page.HasMore {
			t.Fatalf("empty search page = %+v, want 0 items and HasMore=false", page)
		}
	})

	t.Run("429 RateLimit, 500 Servidor, partial JSON Contrato, and IntegrityCheckFailed Contrato", func(t *testing.T) {
		baseNow := time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
		resetUnix := baseNow.Add(5 * time.Second).Unix()

		// 429
		var rlAttempts int32
		srv429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&rlAttempts, 1)
			w.Header().Set("Ratelimit-Reset", strconv.FormatInt(resetUnix, 10))
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv429.Close()

		src429 := NewGQLSource(GQLConfig{
			Endpoint:   srv429.URL,
			HTTPClient: srv429.Client(),
			Now:        func() time.Time { return baseNow },
			Sleep:      func(_ context.Context, _ time.Duration) error { return nil },
		})
		if _, err := src429.Search(context.Background(), "gaules"); KindOf(err) != RateLimit {
			t.Fatalf("429 KindOf(err) = %q, want %q (err=%v)", KindOf(err), RateLimit, err)
		}
		if got := atomic.LoadInt32(&rlAttempts); got != 2 {
			t.Fatalf("429 attempts = %d, want 2", got)
		}

		// 500
		var srvAttempts int32
		srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&srvAttempts, 1)
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv500.Close()

		src500 := NewGQLSource(GQLConfig{
			Endpoint:   srv500.URL,
			HTTPClient: srv500.Client(),
			Sleep:      func(_ context.Context, _ time.Duration) error { return nil },
		})
		if _, err := src500.Search(context.Background(), "gaules"); KindOf(err) != Servidor {
			t.Fatalf("500 KindOf(err) = %q, want %q (err=%v)", KindOf(err), Servidor, err)
		}
		if got := atomic.LoadInt32(&srvAttempts); got != 2 {
			t.Fatalf("500 attempts = %d, want 2", got)
		}

		// Partial JSON and contract violations -> Contrato without retry
		contractCases := []struct {
			name string
			body string
		}{
			{name: "partial_json", body: `{"data":{"searchFor":`},
			{name: "graphql_error", body: `{"errors":[{"message":"PersistedQueryNotFound"}]}`},
			{name: "missing_searchFor_channels", body: `{"data":{"searchFor":null}}`},
			{name: "null_edge_item", body: `{"data":{"searchFor":{"channels":{"cursor":"","edges":[{"item":null}]}}}}`},
			{name: "missing_item_login", body: `{"data":{"searchFor":{"channels":{"cursor":"","edges":[{"item":{"login":"","displayName":"NoLogin"}}]}}}}`},
		}
		for _, tc := range contractCases {
			t.Run(tc.name, func(t *testing.T) {
				var contractAttempts int32
				srvContract := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&contractAttempts, 1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.body))
				}))
				defer srvContract.Close()

				srcContract := NewGQLSource(GQLConfig{
					Endpoint:   srvContract.URL,
					HTTPClient: srvContract.Client(),
					Sleep: func(_ context.Context, _ time.Duration) error {
						t.Fatal("Contrato error in Search must not trigger retry")
						return nil
					},
				})
				if _, err := srcContract.Search(context.Background(), "gaules"); KindOf(err) != Contrato {
					t.Fatalf("%s: KindOf(err) = %q, want %q (err=%v)", tc.name, KindOf(err), Contrato, err)
				}
				if got := atomic.LoadInt32(&contractAttempts); got != 1 {
					t.Fatalf("%s: attempts = %d, want 1", tc.name, got)
				}
			})
		}

		// IntegrityCheckFailed: a busca não pagina, então é erro visível e não "sem resultados".
		srvIntegrity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errors":[{"message":"failed integrity check","extensions":{"code":"IntegrityCheckFailed"}}],"data":{"searchFor":null}}`))
		}))
		defer srvIntegrity.Close()

		srcIntegrity := NewGQLSource(GQLConfig{
			Endpoint:   srvIntegrity.URL,
			HTTPClient: srvIntegrity.Client(),
		})
		if _, err := srcIntegrity.Search(context.Background(), "gaules"); KindOf(err) != Contrato {
			t.Fatalf("IntegrityCheckFailed KindOf(err) = %q, want %q (err=%v)", KindOf(err), Contrato, err)
		}
	})
}

func TestCategoriesIntegrityCheckFailedReturnsEmptyPageWithoutLooping(t *testing.T) {
	const integrityFixture = `{"errors":[{"message":"failed integrity check","path":["games"],"extensions":{"code":"IntegrityCheckFailed"}}],"data":{"games":null}}`

	t.Run("fixture returns empty page without error and caches result", func(t *testing.T) {
		var serverHits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&serverHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(integrityFixture))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			CacheTTL:   time.Minute,
		})

		ctx := context.Background()
		page, err := src.Categories(ctx, catalog.PageRequest{Cursor: "cursor-page-1", Limit: 20})
		if err != nil {
			t.Fatalf("Categories returned unexpected error: %v", err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("len(page.Items) = %d, want 0", len(page.Items))
		}
		if page.HasMore {
			t.Fatalf("page.HasMore = %v, want false", page.HasMore)
		}
		if page.Cursor != "" {
			t.Fatalf("page.Cursor = %q, want empty string", page.Cursor)
		}
		if hits := atomic.LoadInt32(&serverHits); hits != 1 {
			t.Fatalf("serverHits = %d, want 1", hits)
		}

		// A consumer pagination loop terminates immediately because HasMore=false and Cursor is empty.
		loopCalls := 0
		curr := page
		for curr.HasMore && curr.Cursor != "" {
			loopCalls++
			var nextErr error
			curr, nextErr = src.Categories(ctx, catalog.PageRequest{Cursor: curr.Cursor, Limit: 20})
			if nextErr != nil {
				t.Fatalf("unexpected error in pagination loop: %v", nextErr)
			}
		}
		if loopCalls != 0 {
			t.Fatalf("pagination loop made %d calls, want 0", loopCalls)
		}

		// A subsequent call with the same cursor hits the cache and avoids repeated network requests.
		repeatPage, err := src.Categories(ctx, catalog.PageRequest{Cursor: "cursor-page-1", Limit: 20})
		if err != nil {
			t.Fatalf("repeat call returned unexpected error: %v", err)
		}
		if len(repeatPage.Items) != 0 || repeatPage.HasMore || repeatPage.Cursor != "" {
			t.Fatalf("repeat call returned unexpected page: %+v", repeatPage)
		}
		if hits := atomic.LoadInt32(&serverHits); hits != 1 {
			t.Fatalf("serverHits after repeat call = %d, want 1", hits)
		}
	})

	t.Run("pagination stops cleanly on integrity check failure without infinite loop", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&calls, 1)
			w.Header().Set("Content-Type", "application/json")
			if n == 1 {
				_, _ = w.Write([]byte(`{
					"data": {
						"games": {
							"edges": [{"cursor": "c1", "node": {"id": "1", "name": "Game 1", "viewersCount": 100}}],
							"pageInfo": {"hasNextPage": true}
						}
					}
				}`))
				return
			}
			_, _ = w.Write([]byte(integrityFixture))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		ctx := context.Background()
		cursor := ""
		hasMore := true
		var collected []catalog.Category
		requestCount := 0

		for hasMore {
			requestCount++
			p, err := src.Categories(ctx, catalog.PageRequest{Cursor: cursor, Limit: 10})
			if err != nil {
				t.Fatalf("unexpected error at request %d: %v", requestCount, err)
			}
			collected = append(collected, p.Items...)
			hasMore = p.HasMore
			cursor = p.Cursor
		}

		if requestCount != 2 {
			t.Fatalf("requestCount = %d, want 2 (initial page + 1 pagination attempt that ends)", requestCount)
		}
		if len(collected) != 1 || collected[0].Name != "Game 1" {
			t.Fatalf("collected = %+v, want 1 item", collected)
		}
		if hasMore {
			t.Fatalf("hasMore = %v, want false", hasMore)
		}
		if cursor != "" {
			t.Fatalf("cursor = %q, want empty string", cursor)
		}
	})
}

func TestChannelsAndSearchTimeoutRetriesOnceAndReturnsRede(t *testing.T) {
	trCh := &stallingTransport{}
	var sleptCh []time.Duration
	_, err := stallingSource(trCh, &sleptCh).Channels(context.Background(), "509658", catalog.PageRequest{Limit: 5})
	assertTimeoutRetriedOnce(t, "Channels", trCh, sleptCh, err)

	trSearch := &stallingTransport{}
	var sleptSearch []time.Duration
	_, err = stallingSource(trSearch, &sleptSearch).Search(context.Background(), "gaules")
	assertTimeoutRetriedOnce(t, "Search", trSearch, sleptSearch, err)
}

func TestSearchStreamFieldDrivesAvailability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !strings.Contains(req.Query, "stream {") {
			t.Errorf("search query must request stream to prove availability, got: %s", req.Query)
		}
		if !strings.Contains(req.Query, "game") {
			t.Errorf("search stream must request game { id name }, got: %s", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"cursor":"","edges":[
			{"item":{"id":"1","login":"live_one","displayName":"Live One","stream":{"id":"s1","viewersCount":327,"title":"  Lobby   ao vivo ","game":{"id":"509658","name":"Just Chatting"}}}},
			{"item":{"id":"2","login":"off_one","displayName":"Off One","stream":null}},
			{"item":{"id":"3","login":"unknown_one","displayName":"Unknown One"}}
		]}}}}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	page, err := src.Search(context.Background(), "one")
	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len(page.Items) = %d, want 3", len(page.Items))
	}

	live := page.Items[0]
	if live.Availability != catalog.AvailabilityLive {
		t.Fatalf("live.Availability = %v, want catalog.AvailabilityLive", live.Availability)
	}
	if live.Viewers == nil || *live.Viewers != 327 {
		t.Fatalf("live.Viewers = %v, want 327", live.Viewers)
	}
	if live.Title == nil || *live.Title != "Lobby ao vivo" {
		t.Fatalf("live.Title = %v, want normalized title", live.Title)
	}
	if live.CategoryID == nil || *live.CategoryID != "509658" {
		t.Fatalf("live.CategoryID = %v, want 509658", live.CategoryID)
	}
	if live.CategoryName == nil || *live.CategoryName != "Just Chatting" {
		t.Fatalf("live.CategoryName = %v, want Just Chatting", live.CategoryName)
	}

	if off := page.Items[1]; off.Availability != catalog.AvailabilityOffline || off.Viewers != nil {
		t.Fatalf("stream:null must be offline evidence without viewers, got %+v", off)
	}
	if unk := page.Items[2]; unk.Availability != catalog.AvailabilityUnknown {
		t.Fatalf("absent stream field must stay unknown, got %v", unk.Availability)
	}
}

func TestSearchPrefersExactLoginFromTwitchOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Twitch relevance returns the clone first.
		_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"cursor":"","edges":[
			{"item":{"id":"c1","login":"landonorris0481","displayName":"landonorris0481","stream":null}},
			{"item":{"id":"exact","login":"landonorris","displayName":"LandoNorris","stream":null}},
			{"item":{"id":"c2","login":"landonorris16","displayName":"landonorris16","stream":null}}
		]}}}}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	page, err := src.Search(context.Background(), "landonorris")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len = %d, want 3", len(page.Items))
	}
	if page.Items[0].Login != "landonorris" {
		t.Fatalf("Items[0].Login = %q, want landonorris", page.Items[0].Login)
	}
}

func TestSearchCategoriesFixturesSuccessEmptyAndOrdering(t *testing.T) {
	t.Run("success uses games inline fragment and ranks exact then viewers", func(t *testing.T) {
		var gotQuery, gotVarQ string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req gqlRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			gotQuery = req.Query
			if q, ok := req.Variables["q"].(string); ok {
				gotVarQ = q
			}
			if strings.Contains(req.Query, "categories") && !strings.Contains(req.Query, "games") {
				t.Errorf("SearchCategories must query searchFor.games, not categories")
			}
			w.Header().Set("Content-Type", "application/json")
			// Deliberately out of preferred order: Manager 2, Tactics, Manager(null).
			_, _ = w.Write([]byte(`{
				"data": {
					"searchFor": {
						"games": {
							"edges": [
								{"item": {"id": "528672376", "name": "Teamfight Manager 2", "viewersCount": 3}},
								{"item": {"id": "513143", "name": "Teamfight Tactics", "viewersCount": 4063}},
								{"item": {"id": "1049329847", "name": "Teamfight Manager", "viewersCount": null}}
							]
						}
					}
				}
			}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		cats, err := src.SearchCategories(context.Background(), "  teamfight tactics  ")
		if err != nil {
			t.Fatalf("SearchCategories returned unexpected error: %v", err)
		}
		if !strings.Contains(gotQuery, "... on Game") || !strings.Contains(gotQuery, "games") {
			t.Fatalf("expected games inline fragment in query, got: %s", gotQuery)
		}
		if strings.Contains(gotQuery, "categories {") {
			t.Fatalf("query must not request non-existent searchFor.categories: %s", gotQuery)
		}
		if gotVarQ != "teamfight tactics" {
			t.Fatalf("variables[q] = %q, want trimmed teamfight tactics", gotVarQ)
		}
		if len(cats) != 3 {
			t.Fatalf("len(cats) = %d, want 3", len(cats))
		}
		if cats[0].Name != "Teamfight Tactics" || cats[0].Viewers == nil || *cats[0].Viewers != 4063 {
			t.Fatalf("cats[0] = %+v, want exact Teamfight Tactics with 4063", cats[0])
		}
		if cats[1].Name != "Teamfight Manager 2" || cats[1].Viewers == nil || *cats[1].Viewers != 3 {
			t.Fatalf("cats[1] = %+v, want Teamfight Manager 2 with 3", cats[1])
		}
		if cats[2].Name != "Teamfight Manager" || cats[2].Viewers != nil {
			t.Fatalf("cats[2] = %+v, want Teamfight Manager with null viewers last", cats[2])
		}
	})

	t.Run("empty search result returns empty slice without error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"searchFor":{"games":{"edges":[]}}}}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		cats, err := src.SearchCategories(context.Background(), "nonexistent_xyz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cats) != 0 {
			t.Fatalf("empty search cats = %+v, want 0 items", cats)
		}
	})

	t.Run("blank query skips network", func(t *testing.T) {
		var calls int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})
		cats, err := src.SearchCategories(context.Background(), "   ")
		if err != nil {
			t.Fatalf("blank query error: %v", err)
		}
		if len(cats) != 0 || atomic.LoadInt32(&calls) != 0 {
			t.Fatalf("blank query cats=%v calls=%d, want empty and no HTTP", cats, calls)
		}
	})

	t.Run("missing games is Contrato", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"searchFor":{"channels":{"edges":[]}}}}`))
		}))
		defer srv.Close()

		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})
		_, err := src.SearchCategories(context.Background(), "tft")
		if KindOf(err) != Contrato {
			t.Fatalf("KindOf(err) = %v, want Contrato; err=%v", KindOf(err), err)
		}
	})
}

func TestChannelsByLoginsLiveOfflineAndAbsent(t *testing.T) {
	var calls int32
	var gotLogins any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.Contains(req.Query, "users(logins:") {
			t.Errorf("query must use users(logins:), got: %s", req.Query)
		}
		gotLogins = req.Variables["logins"]
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(req.Query, "game") {
			t.Errorf("usersByLogins stream must request game { id name }, got: %s", req.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"users":[
			{"id":"1","login":"live_one","displayName":"Live One","stream":{"id":"s1","viewersCount":327,"title":"  Lobby   ao vivo ","game":{"id":"509658","name":"Just Chatting"}}},
			{"id":"2","login":"off_one","displayName":"Off One","stream":null},
			null
		]}}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	got, err := src.ChannelsByLogins(context.Background(), []string{"Live_One", "off_one", "missing_zzz", "live_one"})
	if err != nil {
		t.Fatalf("ChannelsByLogins: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("HTTP calls = %d, want 1 for N logins", calls)
	}
	logins, ok := gotLogins.([]any)
	if !ok {
		t.Fatalf("variables.logins type = %T, want []any", gotLogins)
	}
	if len(logins) != 3 {
		t.Fatalf("sent %d logins, want 3 unique (duplicates collapsed): %v", len(logins), logins)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (absent omitted)", len(got))
	}

	live := got[0]
	if live.Login != "live_one" || live.Availability != catalog.AvailabilityLive {
		t.Fatalf("live = %+v, want live_one / catalog.AvailabilityLive", live)
	}
	if live.Viewers == nil || *live.Viewers != 327 {
		t.Fatalf("live.Viewers = %v, want 327", live.Viewers)
	}
	if live.Title == nil || *live.Title != "Lobby ao vivo" {
		t.Fatalf("live.Title = %v, want normalized title", live.Title)
	}
	if live.CategoryID == nil || *live.CategoryID != "509658" {
		t.Fatalf("live.CategoryID = %v, want 509658", live.CategoryID)
	}
	if live.CategoryName == nil || *live.CategoryName != "Just Chatting" {
		t.Fatalf("live.CategoryName = %v, want Just Chatting", live.CategoryName)
	}

	off := got[1]
	if off.Login != "off_one" || off.Availability != catalog.AvailabilityOffline || off.Viewers != nil {
		t.Fatalf("off = %+v, want offline without viewers", off)
	}
}

func TestChannelsByLoginsEmptyDoesNotRequest(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	got, err := src.ChannelsByLogins(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil logins error: %v", err)
	}
	if got != nil {
		t.Fatalf("nil logins result = %+v, want nil", got)
	}
	got, err = src.ChannelsByLogins(context.Background(), []string{"", "  "})
	if err != nil {
		t.Fatalf("blank logins error: %v", err)
	}
	if got != nil {
		t.Fatalf("blank logins result = %+v, want nil", got)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("HTTP calls = %d, want 0 for empty batch", calls)
	}
}

func TestChannelsByLoginsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{
		Endpoint:     srv.URL,
		HTTPClient:   srv.Client(),
		RetryBackoff: time.Millisecond,
		Sleep: func(ctx context.Context, d time.Duration) error {
			return nil
		},
	})
	got, err := src.ChannelsByLogins(context.Background(), []string{"gaules"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if KindOf(err) != Servidor {
		t.Fatalf("KindOf(err) = %q, want %q (err=%v)", KindOf(err), Servidor, err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil on failure", got)
	}
}

func TestGQLResponseBodyLimit9MiB(t *testing.T) {
	chunk := make([]byte, 64*1024)
	for i := range chunk {
		chunk[i] = 'a'
	}
	const totalBytes = 9 << 20

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		var written int
		for written < totalBytes {
			toWrite := len(chunk)
			if written+toWrite > totalBytes {
				toWrite = totalBytes - written
			}
			n, err := w.Write(chunk[:toWrite])
			written += n
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})

	_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
	if err == nil {
		t.Fatal("esperava erro em resposta acima de 8 MiB, retornou nil")
	}
	if KindOf(err) != Contrato {
		t.Fatalf("KindOf(err) = %q, esperava %q (err=%v)", KindOf(err), Contrato, err)
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("esperava erro envolvendo ErrResponseTooLarge, obteve: %v", err)
	}
}

func TestGQLRateLimitResetExtremoRetornaEmMenosDeUmSegundo(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.Header().Set("Ratelimit-Reset", "999999999")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	t.Run("sem sleep personalizado retorna erro em menos de 1 s", func(t *testing.T) {
		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
		})

		start := time.Now()
		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
		elapsed := time.Since(start)

		if KindOf(err) != RateLimit {
			t.Fatalf("KindOf(err) = %q, esperava %q (err=%v)", KindOf(err), RateLimit, err)
		}
		if elapsed >= time.Second {
			t.Fatalf("chamada demorou %v, esperava menos de 1 s", elapsed)
		}
	})

	t.Run("nao invoca funcao de sleep quando reset excede 60 s", func(t *testing.T) {
		atomic.StoreInt32(&attempts, 0)
		slept := false
		src := NewGQLSource(GQLConfig{
			Endpoint:   srv.URL,
			HTTPClient: srv.Client(),
			Sleep: func(_ context.Context, _ time.Duration) error {
				slept = true
				return nil
			},
		})

		start := time.Now()
		_, err := src.Categories(context.Background(), catalog.PageRequest{Limit: 10})
		elapsed := time.Since(start)

		if KindOf(err) != RateLimit {
			t.Fatalf("KindOf(err) = %q, esperava %q (err=%v)", KindOf(err), RateLimit, err)
		}
		if elapsed >= time.Second {
			t.Fatalf("chamada demorou %v, esperava menos de 1 s", elapsed)
		}
		if slept {
			t.Fatal("funcao sleep nao deveria ter sido chamada para espera acima de 60 s")
		}
		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Fatalf("tentativas = %d, esperava 1", got)
		}
	})
}
