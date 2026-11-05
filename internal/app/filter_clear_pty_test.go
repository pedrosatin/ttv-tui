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

// filterClearSource offers two live channels so a local filter can hide one,
// then opening the other must restore the full list (ticket 02).
type filterClearSource struct{}

func (filterClearSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-filter", Name: "FilterCat"}}}, nil
}

func (filterClearSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-alpha", Login: "alphachan", DisplayName: "AlphaChan", Viewers: twitch.IntPtr(10)},
		{ID: "ch-peere", Login: "peere", DisplayName: "Peere", Viewers: twitch.IntPtr(20)},
	}}, nil
}

func (filterClearSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-peere", Login: "peere", DisplayName: "Peere"},
	}}, nil
}

func (filterClearSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (filterClearSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

func seedFilterClearFavorite(statePath string) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}
	st := store.State{
		Favorites: []store.Channel{{ID: "fav-1", Login: "favorite_one", DisplayName: "Favorite One"}},
		Recents:   []store.Channel{},
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0o600)
}

// TestFilterClearsOnChannelOpen_PTY drives the real TUI on a PTY: filter to one
// channel, open it, and confirm the filter is gone so favorites and the other
// channel are visible again.
func TestFilterClearsOnChannelOpen_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_FILTER_CLEAR=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "Favorite One") || !strings.Contains(p.screenSince(0), "FilterCat") {
		if time.Now().After(deadline) {
			t.Fatalf("initial categories missing favorite or category:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Favorites render above categories; move onto FilterCat before Enter.
	p.sendAndWait([]byte("j"), "▸")
	p.sendAndWait([]byte("\r"), "AlphaChan")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "Peere") || !strings.Contains(p.screenSince(0), "Favorite O") {
		if time.Now().After(deadline) {
			t.Fatalf("channel list incomplete before filter:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	p.sendAndWait([]byte("/"), "Filter:")
	p.sendAndWait([]byte("peere"), "Filter: peere")

	openOff := p.mark()
	p.sendAndWait([]byte("\r"), "Chat — #peere")
	deadline = time.Now().Add(10 * time.Second)
	for {
		// Only redraws after Enter: filter prompt must be gone and the full
		// list (favorite + both channels) must be visible again. The favorite
		// display name may truncate before [unverified] on the ~43-col list pane.
		screen := p.screenSince(openOff)
		noFilterPrompt := !strings.Contains(screen, "Filter:")
		hasFav := strings.Contains(screen, "Favorite O") && strings.Contains(screen, "Favorites")
		hasAlpha := strings.Contains(screen, "AlphaChan")
		hasPeere := strings.Contains(screen, "Peere")
		if noFilterPrompt && hasFav && hasAlpha && hasPeere {
			t.Logf("PTY after open channel:\n%s", screen)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after opening peere, want full list without filter; screen since open:\n%s", screen)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
