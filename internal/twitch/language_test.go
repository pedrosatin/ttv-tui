package twitch

import (
	"context"
	"encoding/json"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeLanguage(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"PT", "pt"},
		{" en ", "en"},
		{"ES", "es"},
		{"", ""},
		{"  ", ""},
	}
	for _, tc := range cases {
		if got := catalog.NormalizeLanguage(tc.in); got != tc.want {
			t.Fatalf("catalog.NormalizeLanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if catalog.LanguagePtr("PT") == nil || *catalog.LanguagePtr("PT") != "pt" {
		t.Fatalf("catalog.LanguagePtr(PT) failed")
	}
	if catalog.LanguagePtr("  ") != nil {
		t.Fatalf("catalog.LanguagePtr blank must be nil")
	}
}

func TestChannelsParsesLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !containsQueryField(req.Query, "language") {
			t.Errorf("channels query missing language field: %s", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":{"game":{"id":"1","name":"Just Chatting","streams":{"edges":[
				{"cursor":"c1","node":{"id":"s1","broadcaster":{"id":"1","login":"gaules","displayName":"Gaules"},"language":"PT","viewersCount":100,"title":"oi"}},
				{"cursor":"c2","node":{"id":"s2","broadcaster":{"id":"2","login":"xqc","displayName":"xQc"},"language":"EN","viewersCount":50,"title":"hi"}},
				{"cursor":"c3","node":{"id":"s3","broadcaster":{"id":"3","login":"nolang","displayName":"NoLang"},"viewersCount":10,"title":"x"}}
			],"pageInfo":{"hasNextPage":false}}}}
		}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	page, err := src.Channels(context.Background(), "1", catalog.PageRequest{Limit: 10})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("len=%d want 3", len(page.Items))
	}
	if page.Items[0].Language == nil || *page.Items[0].Language != "pt" {
		t.Fatalf("gaules language = %v, want pt", page.Items[0].Language)
	}
	if page.Items[1].Language == nil || *page.Items[1].Language != "en" {
		t.Fatalf("xqc language = %v, want en", page.Items[1].Language)
	}
	if page.Items[2].Language != nil {
		t.Fatalf("nolang language = %v, want nil", page.Items[2].Language)
	}
}

func TestSearchParsesLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !containsQueryField(req.Query, "language") {
			t.Errorf("search query missing language field")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":{"searchFor":{"channels":{"edges":[
				{"item":{"id":"1","login":"gaules","displayName":"Gaules","stream":{"id":"s","language":"PT","viewersCount":9,"title":"live"}}},
				{"item":{"id":"2","login":"offline","displayName":"Offline","stream":null}}
			]}}}
		}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	page, err := src.Search(context.Background(), "gaules")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page.Items) < 1 {
		t.Fatalf("expected search hits")
	}
	var gaules *catalog.Channel
	for i := range page.Items {
		if page.Items[i].Login == "gaules" {
			gaules = &page.Items[i]
			break
		}
	}
	if gaules == nil || gaules.Language == nil || *gaules.Language != "pt" {
		t.Fatalf("gaules language = %+v", gaules)
	}
}

func TestChannelsByLoginsParsesLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !containsQueryField(req.Query, "language") {
			t.Errorf("usersByLogins query missing language field")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":{"users":[
				{"id":"1","login":"live_one","displayName":"Live One","stream":{"id":"s1","language":"ES","viewersCount":12,"title":"hola"}},
				{"id":"2","login":"offline_one","displayName":"Offline One","stream":null}
			]}
		}`))
	}))
	defer srv.Close()

	src := NewGQLSource(GQLConfig{Endpoint: srv.URL, HTTPClient: srv.Client()})
	got, err := src.ChannelsByLogins(context.Background(), []string{"live_one", "offline_one"})
	if err != nil {
		t.Fatalf("ChannelsByLogins: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].Language == nil || *got[0].Language != "es" {
		t.Fatalf("live language = %v, want es", got[0].Language)
	}
	if got[1].Language != nil {
		t.Fatalf("offline language = %v, want nil", got[1].Language)
	}
}

func containsQueryField(query, field string) bool {
	return len(query) > 0 && (containsWord(query, field))
}

func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		leftOK := i == 0 || !isIdentByte(s[i-1])
		rightOK := i+len(word) == len(s) || !isIdentByte(s[i+len(word)])
		if leftOK && rightOK {
			return true
		}
	}
	return false
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
