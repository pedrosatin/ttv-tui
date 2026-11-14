//go:build unix

package app

import (
	"context"
	"encoding/json"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/store"
)

// filterClearSource offers two live channels so a local filter can hide one,
// then opening the other must restore the full list (ticket 02).
type filterClearSource struct{}

func (filterClearSource) Categories(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "cat-filter", Name: "FilterCat"}}}, nil
}

func (filterClearSource) Channels(context.Context, string, catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "ch-alpha", Login: "alphachan", DisplayName: "AlphaChan", Viewers: catalog.IntPtr(10)},
		{ID: "ch-peere", Login: "peere", DisplayName: "Peere", Viewers: catalog.IntPtr(20)},
	}}, nil
}

func (filterClearSource) Search(context.Context, string) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "ch-peere", Login: "peere", DisplayName: "Peere"},
	}}, nil
}

func (filterClearSource) SearchCategories(context.Context, string) ([]catalog.Category, error) {
	return nil, nil
}

func (filterClearSource) ChannelsByLogins(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func (filterClearSource) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func seedFilterClearFavorite(statePath string) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}
	st := store.State{
		// O favorito é um canal da categoria: lá ele aparece no lugar dele, com ★.
		Favorites: []store.Channel{{ID: "ch-alpha", Login: "alphachan", DisplayName: "AlphaChan"}},
		Recents:   []store.Channel{},
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, data, 0o600)
}

// TestFilterClearsOnChannelOpen_PTY drives the real TUI on a PTY: filter to one
// channel, open it, and confirm the filter is gone so the favorite and the
// other channel are visible again.
func TestFilterClearsOnChannelOpen_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_FILTER_CLEAR=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	p.waitScreen(0, "AlphaChan", "FilterCat")
	// Favorites render above categories; move onto FilterCat before Enter.
	p.sendAndWait([]byte("j"), "▸")
	p.sendAndWait([]byte("\r"), "Peere")
	p.waitScreen(0, "Peere", "AlphaChan")

	p.sendAndWait([]byte("/"), "Filter:")
	p.sendAndWait([]byte("peere"), "Filter: peere")

	openOff := p.mark()
	oldPid := p.playerPid
	p.sendAndWait([]byte("\r"), "Chat — #peere")
	p.followPlayer(oldPid)
	screen := p.waitScreen(openOff, "★", "AlphaChan", "Peere")
	if strings.Contains(screen, "Filter:") {
		t.Fatalf("after opening peere, filter prompt remains; screen since open:\n%s", screen)
	}
	t.Logf("PTY after open channel:\n%s", screen)
}
