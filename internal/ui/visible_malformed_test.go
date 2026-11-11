package ui

import (
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestVisibleItems_EmptyLoginsDoNotMatchDifferentChannels(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "1", Login: ""},
		}),
	)
	m.list.mode = ModeChannels
	m.list.channels = []twitch.Channel{
		{ID: "2", Login: "", Viewers: twitch.IntPtr(1)},
	}

	items := m.VisibleItems()
	if len(items) == 0 {
		t.Fatalf("expected visible items, got none")
	}
	if items[0].Type != ItemTypeFavorite {
		t.Fatalf("items[0].Type = %v, want ItemTypeFavorite", items[0].Type)
	}
	if items[0].ID() != "1" {
		t.Fatalf("items[0].ID() = %q, want %q", items[0].ID(), "1")
	}
}

func TestVisibleItems_DeduplicatesSameChannelAndIgnoresEmptyEntries(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	mFav := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "1", Login: "a"},
			{ID: "1", Login: "a"},
		}),
	)
	itemsFav := mFav.VisibleItems()
	if len(itemsFav) != 1 {
		t.Fatalf("expected 1 favorite item, got %d", len(itemsFav))
	}

	mRec := New(catEN,
		WithRecents([]twitch.Channel{
			{ID: "", Login: ""},
			{ID: "", Login: ""},
		}),
	)
	itemsRec := mRec.VisibleItems()
	if len(itemsRec) != 0 {
		t.Fatalf("expected empty channels to be ignored, got %d items", len(itemsRec))
	}

	mRecDup := New(catEN,
		WithRecents([]twitch.Channel{
			{ID: "1", Login: "a"},
			{ID: "1", Login: "a"},
		}),
	)
	itemsRecDup := mRecDup.VisibleItems()
	if len(itemsRecDup) != 1 {
		t.Fatalf("expected 1 recent item, got %d", len(itemsRecDup))
	}

	mFavEmpty := New(catEN,
		WithFavorites([]twitch.Channel{
			{ID: "", Login: ""},
		}),
	)
	if len(mFavEmpty.VisibleItems()) != 0 {
		t.Fatalf("expected empty favorite to be ignored, got %d items", len(mFavEmpty.VisibleItems()))
	}
}
