package kick

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// siteServer answers the two routes of the site JSON from a table keyed by path.
func siteServer(t *testing.T, routes map[string]func(w http.ResponseWriter)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(fixture(t, "channel_not_found.json"))
			return
		}
		h(w)
	}))
	t.Cleanup(srv.Close)
	return NewClient(ClientConfig{BaseURL: srv.URL})
}

func body(b []byte) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { _, _ = w.Write(b) }
}

func status(code int) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func TestLivestream_LiveFixture(t *testing.T) {
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/gaules/livestream": body(fixture(t, "livestream_live.json")),
	})
	ch, err := c.Livestream(context.Background(), "gaules")
	if err != nil {
		t.Fatalf("Livestream: %v", err)
	}
	if ch.Availability != catalog.AvailabilityLive {
		t.Errorf("availability = %v, want live", ch.Availability)
	}
	if ch.Viewers == nil || *ch.Viewers != 3681 {
		t.Errorf("viewers = %v, want 3681", ch.Viewers)
	}
	if ch.Title == nil || !strings.HasPrefix(*ch.Title, "GAMEPLAY DO ALASTRO!") {
		t.Errorf("title = %v", ch.Title)
	}
	if ch.CategoryName == nil || *ch.CategoryName != "Project Zomboid" {
		t.Errorf("category = %v, want Project Zomboid", ch.CategoryName)
	}
	if ch.Platform != platform.Kick || ch.Login != "gaules" || ch.ID != "kick:gaules" || ch.URL != "https://kick.com/gaules" {
		t.Errorf("identity = %+v", ch)
	}
	if !ch.Matches(catalog.Channel{Platform: platform.Kick, Login: "Gaules"}) {
		t.Error("resolved channel does not match the saved one")
	}
}

func TestLivestream_OfflineFixture(t *testing.T) {
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/xqc/livestream": body(fixture(t, "livestream_offline.json")),
	})
	ch, err := c.Livestream(context.Background(), "xqc")
	if err != nil {
		t.Fatalf("Livestream: %v", err)
	}
	if ch.Availability != catalog.AvailabilityOffline {
		t.Errorf("availability = %v, want offline", ch.Availability)
	}
	if ch.Viewers != nil || ch.Title != nil {
		t.Errorf("offline channel carries stream data: %+v", ch)
	}
}

func TestClient_StatusClassification(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		kind     catalog.ErrorKind
		notFound bool
	}{
		{"cloudflare block", http.StatusForbidden, catalog.Bloqueio, false},
		{"unknown channel", http.StatusNotFound, catalog.Contrato, true},
		{"rate limit", http.StatusTooManyRequests, catalog.RateLimit, false},
		{"server error", http.StatusBadGateway, catalog.Servidor, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := siteServer(t, map[string]func(http.ResponseWriter){
				"/api/v2/channels/x/livestream": status(tc.status),
				"/api/v2/channels/x":            status(tc.status),
			})
			for _, call := range []func() error{
				func() error { _, err := c.Livestream(context.Background(), "x"); return err },
				func() error { _, err := c.Room(context.Background(), "x"); return err },
			} {
				err := call()
				if got := catalog.KindOf(err); got != tc.kind {
					t.Errorf("kind = %q, want %q (err %v)", got, tc.kind, err)
				}
				if got := errors.Is(err, ErrChannelNotFound); got != tc.notFound {
					t.Errorf("is not found = %v, want %v", got, tc.notFound)
				}
				var ce *catalog.Error
				if !errors.As(err, &ce) || ce.Platform != platform.Kick || ce.StatusCode != tc.status {
					t.Errorf("error = %#v, want a kick catalog.Error with the status", err)
				}
			}
		})
	}
}

func TestClient_NetworkFailureIsRede(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := NewClient(ClientConfig{BaseURL: srv.URL})
	srv.Close()
	_, err := c.Livestream(context.Background(), "gaules")
	if got := catalog.KindOf(err); got != catalog.Rede {
		t.Fatalf("kind = %q, want Rede (err %v)", got, err)
	}
}

func TestClient_UnreadableAnswerIsContrato(t *testing.T) {
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/a/livestream": body([]byte("<html>Just a moment...</html>")),
		"/api/v2/channels/b/livestream": body([]byte(`{"data":{"session_title":"no id"}}`)),
		"/api/v2/channels/a":            body([]byte(`{"slug":"a","chatroom":null}`)),
	})
	for name, call := range map[string]func() error{
		"html":        func() error { _, err := c.Livestream(context.Background(), "a"); return err },
		"no id":       func() error { _, err := c.Livestream(context.Background(), "b"); return err },
		"no chatroom": func() error { _, err := c.Room(context.Background(), "a"); return err },
	} {
		if got := catalog.KindOf(call()); got != catalog.Contrato {
			t.Errorf("%s: kind = %q, want Contrato", name, got)
		}
	}
}

func TestRoom_Fixtures(t *testing.T) {
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/gaules": body(fixture(t, "channel_live.json")),
		"/api/v2/channels/xqc":    body(fixture(t, "channel_offline.json")),
	})
	live, err := c.Room(context.Background(), "gaules")
	if err != nil {
		t.Fatalf("Room(gaules): %v", err)
	}
	if want := (Room{Slug: "gaules", DisplayName: "Gaules", ChatroomID: 66973867}); live != want {
		t.Errorf("room = %+v, want %+v", live, want)
	}
	// An offline channel still has its chat room.
	off, err := c.Room(context.Background(), "xqc")
	if err != nil {
		t.Fatalf("Room(xqc): %v", err)
	}
	if want := (Room{Slug: "xqc", DisplayName: "xQc", ChatroomID: 668}); off != want {
		t.Errorf("room = %+v, want %+v", off, want)
	}
}

func TestChannelsByIDs(t *testing.T) {
	var asked atomic.Int32
	count := func(h func(http.ResponseWriter)) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { asked.Add(1); h(w) }
	}
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/gaules/livestream": count(body(fixture(t, "livestream_live.json"))),
		"/api/v2/channels/xqc/livestream":    count(body(fixture(t, "livestream_offline.json"))),
		"/api/v2/channels/broken/livestream": count(body([]byte("not json"))),
	})
	// Duplicates, a qualified id, a recording path and an unknown channel.
	got, err := c.ChannelsByIDs(context.Background(), []string{"gaules", "Gaules", "kick:xqc", "xqc/videos/abc", "ghost", "broken", " "})
	if err != nil {
		t.Fatalf("ChannelsByIDs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("resolved %d channels, want 2: %+v", len(got), got)
	}
	if got[0].Login != "gaules" || got[0].Availability != catalog.AvailabilityLive {
		t.Errorf("first = %+v, want gaules live", got[0])
	}
	if got[1].Login != "xqc" || got[1].Availability != catalog.AvailabilityOffline {
		t.Errorf("second = %+v, want xqc offline", got[1])
	}
	if n := asked.Load(); n != 3 {
		t.Errorf("site asked %d times for known routes, want 3 (one per unique slug)", n)
	}

	if out, err := c.ChannelsByIDs(context.Background(), nil); out != nil || err != nil {
		t.Errorf("empty ids = (%v, %v), want (nil, nil)", out, err)
	}
}

func TestChannelsByIDs_BlockFailsTheLookup(t *testing.T) {
	c := siteServer(t, map[string]func(http.ResponseWriter){
		"/api/v2/channels/gaules/livestream": body(fixture(t, "livestream_live.json")),
		"/api/v2/channels/xqc/livestream":    status(http.StatusForbidden),
	})
	got, err := c.ChannelsByIDs(context.Background(), []string{"gaules", "xqc"})
	if catalog.KindOf(err) != catalog.Bloqueio {
		t.Fatalf("err = %v, want Bloqueio", err)
	}
	if got != nil {
		t.Errorf("channels = %+v, want none with the error", got)
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		ch   catalog.Channel
		want string
	}{
		{catalog.Channel{Platform: platform.Kick, Login: "Gaules"}, "gaules"},
		{catalog.Channel{Platform: platform.Kick, ID: "kick:xqc"}, "xqc"},
		{catalog.Channel{Platform: platform.Kick, Login: "xqc/videos/0a1b"}, ""},
		{catalog.Channel{Platform: platform.Kick, Login: "xqc/clip/clip_01"}, ""},
		{catalog.Channel{Platform: platform.Kick}, ""},
	}
	for _, tc := range tests {
		if got := Slug(tc.ch); got != tc.want {
			t.Errorf("Slug(%+v) = %q, want %q", tc.ch, got, tc.want)
		}
	}
}
