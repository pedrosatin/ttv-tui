package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/store"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// recentOrderSource backs the ticket 08 PTY: a category plus one live channel
// so refresh has something to reload while recents dominate the list order.
type recentOrderSource struct{}

func (recentOrderSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-order", Name: "OrderCat"}}}, nil
}

func (recentOrderSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-live", Login: "livechan", DisplayName: "LiveChan", Viewers: twitch.IntPtr(42)},
	}}, nil
}

func (recentOrderSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-live", Login: "livechan", DisplayName: "LiveChan"},
	}}, nil
}

func (recentOrderSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

func seedRecentOrderState(statePath string) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}
	st := store.State{
		Favorites: []store.Channel{{ID: "fav-1", Login: "favorite_one", DisplayName: "Favorite One"}},
		Recents: []store.Channel{
			{ID: "rec-a", Login: "alice", DisplayName: "Alice"},
			{ID: "rec-b", Login: "bob", DisplayName: "Bob"},
			{ID: "rec-c", Login: "carol", DisplayName: "Carol"},
		},
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0o600)
}

func recentNamesOrder(screen string) []string {
	names := []string{"Alice", "Bob", "Carol"}
	type hit struct {
		name string
		idx  int
	}
	var hits []hit
	for _, n := range names {
		i := strings.Index(screen, n)
		if i >= 0 {
			hits = append(hits, hit{n, i})
		}
	}
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[j].idx < hits[i].idx {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.name
	}
	return out
}

// TestOpenChannelDoesNotReorderRecents_PTY opens Carol (third recent) and
// checks the on-screen recent order stays Alice → Bob → Carol; after refresh
// it becomes Carol → Alice → Bob.
func TestOpenChannelDoesNotReorderRecents_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_RECENT_ORDER=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for {
		screen := p.screenSince(0)
		if strings.Contains(screen, "Alice") && strings.Contains(screen, "Carol") && strings.Contains(screen, "OrderCat") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("initial list missing recents/category:\n%s", screen)
		}
		time.Sleep(20 * time.Millisecond)
	}

	initial := recentNamesOrder(p.screenSince(0))
	if want := []string{"Alice", "Bob", "Carol"}; !stringSlicesEqual(initial, want) {
		t.Fatalf("initial recent order = %v, want %v\n%s", initial, want, p.screenSince(0))
	}

	// Favorites first, then Alice (highlight). j → Bob, j → Carol.
	p.sendAndWait([]byte("j"), "▸")
	p.sendAndWait([]byte("j"), "▸")
	p.sendAndWait([]byte("j"), "▸")

	openOff := p.mark()
	p.sendAndWait([]byte("\r"), "Chat — #carol")
	deadline = time.Now().Add(10 * time.Second)
	var afterOpen string
	for {
		afterOpen = p.screenSince(openOff)
		if strings.Contains(afterOpen, "Alice") && strings.Contains(afterOpen, "Carol") && strings.Contains(afterOpen, "Chat — #carol") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after open, list/chat incomplete:\n%s", afterOpen)
		}
		time.Sleep(20 * time.Millisecond)
	}
	openedOrder := recentNamesOrder(afterOpen)
	if want := []string{"Alice", "Bob", "Carol"}; !stringSlicesEqual(openedOrder, want) {
		t.Fatalf("after opening Carol, recent order = %v, want frozen %v\n%s", openedOrder, want, afterOpen)
	}
	t.Logf("PTY after open (no reorder):\n%s", afterOpen)

	refreshOff := p.mark()
	p.send([]byte("r"))
	deadline = time.Now().Add(10 * time.Second)
	var afterRefresh string
	for {
		afterRefresh = p.screenSince(refreshOff)
		order := recentNamesOrder(afterRefresh)
		if stringSlicesEqual(order, []string{"Carol", "Alice", "Bob"}) {
			t.Logf("PTY after refresh (new order):\n%s", afterRefresh)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after refresh, recent order = %v, want Carol Alice Bob\n%s", order, afterRefresh)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
