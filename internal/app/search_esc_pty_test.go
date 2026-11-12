//go:build unix

package app

import (
	"context"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"
	"time"
)

// searchEscSource backs the ticket 03 PTY: one category with a live channel,
// plus a distinct search hit, so Esc can return to either surface.
type searchEscSource struct{}

func (searchEscSource) Categories(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	return catalog.Page[catalog.Category]{Items: []catalog.Category{
		{ID: "cat-esc", Name: "EscCategory"},
		{ID: "cat-other", Name: "OtherCategory"},
	}}, nil
}

func (searchEscSource) Channels(context.Context, string, catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "ch-alpha", Login: "alphachan", DisplayName: "AlphaChan", Viewers: catalog.IntPtr(10)},
	}}, nil
}

func (searchEscSource) Search(context.Context, string) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "ch-search", Login: "searchhit", DisplayName: "SearchHit"},
	}}, nil
}

func (searchEscSource) SearchCategories(context.Context, string) ([]catalog.Category, error) {
	return nil, nil
}

func (searchEscSource) ChannelsByLogins(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func (searchEscSource) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

// TestEscFromSearchReturnsToOrigin_PTY drives the real TUI: Esc from a search
// opened inside a category restores that category's channels; Esc from a search
// opened on the categories list restores the categories.
func TestEscFromSearchReturnsToOrigin_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_SEARCH_ESC=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "EscCategory") || !strings.Contains(p.screenSince(0), "OtherCategory") {
		if time.Now().After(deadline) {
			t.Fatalf("initial categories missing:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Open EscCategory → channels list (do not Enter the channel).
	p.sendAndWait([]byte("\r"), "AlphaChan")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "Live Channels — EscCategory") {
		if time.Now().After(deadline) {
			t.Fatalf("category channels title missing:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Search from the open category (Ctrl+F; 0x06).
	p.sendAndWait([]byte{0x06}, "Search:")
	p.sendAndWait([]byte("hit\r"), "Search Results — hit")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "SearchHit") {
		if time.Now().After(deadline) {
			t.Fatalf("search results missing SearchHit:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	fromCategoryOff := p.mark()
	p.send([]byte{0x1b}) // Esc
	deadline = time.Now().Add(10 * time.Second)
	var backToChannels string
	for {
		backToChannels = p.screenSince(fromCategoryOff)
		if strings.Contains(backToChannels, "Live Channels — EscCategory") &&
			strings.Contains(backToChannels, "AlphaChan") &&
			!strings.Contains(backToChannels, "Search Results") {
			t.Logf("PTY CAPTURE Esc back to channels:\n%s", backToChannels)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Esc from category search must restore EscCategory channels; got:\n%s", backToChannels)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Back to categories, then search from there.
	p.sendAndWait([]byte{0x1b}, "Live Categories")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "OtherCategory") {
		if time.Now().After(deadline) {
			t.Fatalf("categories not restored before second search:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	p.sendAndWait([]byte{0x06}, "Search:") // Ctrl+F
	p.sendAndWait([]byte("hit\r"), "Search Results — hit")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "SearchHit") {
		if time.Now().After(deadline) {
			t.Fatalf("second search results missing SearchHit:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	fromCategoriesOff := p.mark()
	p.send([]byte{0x1b})
	deadline = time.Now().Add(10 * time.Second)
	var backToCategories string
	for {
		backToCategories = p.screenSince(fromCategoriesOff)
		if strings.Contains(backToCategories, "Live Categories") &&
			strings.Contains(backToCategories, "EscCategory") &&
			strings.Contains(backToCategories, "OtherCategory") &&
			!strings.Contains(backToCategories, "Search Results") {
			t.Logf("PTY CAPTURE Esc back to categories:\n%s", backToCategories)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Esc from categories search must restore category list; got:\n%s", backToCategories)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
