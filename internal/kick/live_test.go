package kick

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// TestLive_Contract talks to the real Kick. It is skipped unless
// TTV_KICK_LIVE_SLUG names a channel that is live now: neither the site JSON
// nor the Pusher key has a contract, and this is how to check them by hand.
//
//	TTV_KICK_LIVE_SLUG=<slug> go test ./internal/kick -run TestLive_Contract -v
func TestLive_Contract(t *testing.T) {
	slug := os.Getenv("TTV_KICK_LIVE_SLUG")
	if slug == "" {
		t.Skip("TTV_KICK_LIVE_SLUG not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := NewClient(ClientConfig{})
	ch, err := client.Livestream(ctx, slug)
	if err != nil {
		t.Fatalf("Livestream(%q): %v", slug, err)
	}
	if ch.Availability != catalog.AvailabilityLive {
		t.Fatalf("Livestream(%q) availability = %v, want live", slug, ch.Availability)
	}
	t.Logf("live: title=%q viewers=%v category=%v", deref(ch.Title), derefInt(ch.Viewers), deref(ch.CategoryName))

	chat := NewChat(ChatOptions{Rooms: client, PusherKey: os.Getenv("TTV_KICK_LIVE_KEY")})
	defer chat.Close()
	if _, err := chat.Join(ctx, catalog.Channel{Platform: platform.Kick, Login: slug}); err != nil {
		t.Fatalf("Join: %v", err)
	}
	connected, messages := false, 0
	for messages == 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("no chat message before the deadline (connected=%v)", connected)
		case ev := <-chat.Events():
			if ev.State == catalog.ChatStateError {
				t.Fatalf("chat error: %v (reason %q)", ev.Err, catalog.ChatErrorReasonOf(ev.Err))
			}
			if ev.State == catalog.ChatStateConnected {
				connected = true
			}
			if ev.State != "" {
				t.Logf("state %s err=%v", ev.State, ev.Err)
			}
			for _, m := range ev.Messages {
				messages++
				t.Logf("message from a %d-rune name, %d runes, badges=%v", len([]rune(m.DisplayName)), len([]rune(m.Body)), m.Badges)
			}
		}
	}
	if !connected {
		t.Fatal("messages arrived without a connected state")
	}
}

// TestLive_Catalog talks to the real Kick. It is skipped unless
// TTV_KICK_LIVE_CATALOG is set: the routes of the catalog have no contract, and
// this is how to check the four of them by hand.
//
//	TTV_KICK_LIVE_CATALOG=1 go test ./internal/kick -run TestLive_Catalog -v
func TestLive_Catalog(t *testing.T) {
	if os.Getenv("TTV_KICK_LIVE_CATALOG") == "" {
		t.Skip("TTV_KICK_LIVE_CATALOG not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := NewClient(ClientConfig{})

	cats, err := client.Categories(ctx, catalog.PageRequest{})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats.Items) == 0 || !cats.HasMore || cats.Cursor != "2" {
		t.Fatalf("Categories: %d items, HasMore=%v Cursor=%q", len(cats.Items), cats.HasMore, cats.Cursor)
	}
	top := cats.Items[0]
	t.Logf("categories: %d on the first page; the top one has %d viewers", len(cats.Items), derefInt(top.Viewers))
	next, err := client.Categories(ctx, catalog.PageRequest{Cursor: cats.Cursor})
	if err != nil || len(next.Items) == 0 {
		t.Fatalf("Categories(page 2): %d items, err %v", len(next.Items), err)
	}
	past, err := client.Categories(ctx, catalog.PageRequest{Cursor: "21"})
	if err != nil || len(past.Items) != 0 || past.HasMore {
		t.Fatalf("Categories(page 21): %d items, HasMore=%v, err %v; want the end of the listing", len(past.Items), past.HasMore, err)
	}

	chans, err := client.Channels(ctx, top.ID, catalog.PageRequest{Limit: 30})
	if err != nil {
		t.Fatalf("Channels(%s): %v", top.ID, err)
	}
	if len(chans.Items) != 30 || !chans.HasMore {
		t.Fatalf("Channels(%s): %d items, HasMore=%v; want a full page of the top category", top.ID, len(chans.Items), chans.HasMore)
	}
	first := chans.Items[0]
	if first.Login == "" || first.Availability != catalog.AvailabilityLive || first.Platform != platform.Kick {
		t.Fatalf("first channel = %+v", first)
	}
	more, err := client.Channels(ctx, top.ID, catalog.PageRequest{Limit: 30, Cursor: chans.Cursor})
	if err != nil || len(more.Items) == 0 || more.Items[0].Login == first.Login {
		t.Fatalf("Channels(next page): %d items, err %v", len(more.Items), err)
	}
	t.Logf("channels: 30 + %d; the first has %d viewers, language %q", len(more.Items), derefInt(first.Viewers), deref(first.Language))

	hits, err := client.Search(ctx, first.Login)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits.Items) == 0 || hits.Items[0].Login != first.Login || hits.Items[0].Availability != catalog.AvailabilityLive {
		t.Fatalf("Search for a live channel of the listing: %d hits, first %+v", len(hits.Items), hits.Items)
	}
	found, err := client.SearchCategories(ctx, top.Name)
	if err != nil {
		t.Fatalf("SearchCategories: %v", err)
	}
	if len(found) == 0 || found[0].ID != top.ID {
		t.Fatalf("SearchCategories for the top category: %d hits, first %+v", len(found), found)
	}
	t.Logf("search: %d channels, %d categories", len(hits.Items), len(found))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return -1
	}
	return *n
}
