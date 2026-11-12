package ui

import (
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestVisibleItems_MalformedData(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)

	t.Run("empty logins do not match different channels", func(t *testing.T) {
		m := New(catEN,
			WithFavorites([]catalog.Channel{
				{ID: "1", Login: ""},
			}),
		)
		m.list.mode = ModeChannels
		m.list.channels = []catalog.Channel{
			{ID: "2", Login: "", Viewers: catalog.IntPtr(1)},
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
	})

	t.Run("duplicate favorites", func(t *testing.T) {
		m := New(catEN,
			WithFavorites([]catalog.Channel{
				{ID: "1", Login: "a"},
				{ID: "1", Login: "a"},
			}),
		)
		items := m.VisibleItems()
		if len(items) != 1 {
			t.Fatalf("expected 1 favorite item, got %d", len(items))
		}
	})

	t.Run("case 3 recent resolving to duplicate favorite login", func(t *testing.T) {
		m := New(catEN,
			WithFavorites([]catalog.Channel{
				{ID: "10", Login: "a"},
			}),
			WithRecents([]catalog.Channel{
				{ID: "11", Login: ""},
			}),
		)
		m.list.mode = ModeChannels
		m.list.channels = []catalog.Channel{
			{ID: "10", Login: "a", Viewers: catalog.IntPtr(5)},
			{ID: "11", Login: "a", Viewers: catalog.IntPtr(6)},
		}

		items := m.VisibleItems()
		if len(items) != 1 {
			t.Fatalf("expected 1 item with login %q, got %d", "a", len(items))
		}
		if items[0].Login() != "a" {
			t.Fatalf("items[0].Login() = %q, want %q", items[0].Login(), "a")
		}
	})

	t.Run("empty favorite ignored", func(t *testing.T) {
		m := New(catEN,
			WithFavorites([]catalog.Channel{
				{ID: "", Login: ""},
			}),
		)
		if len(m.VisibleItems()) != 0 {
			t.Fatalf("expected empty favorite to be ignored, got %d items", len(m.VisibleItems()))
		}
	})

	t.Run("empty recents ignored", func(t *testing.T) {
		m := New(catEN,
			WithRecents([]catalog.Channel{
				{ID: "", Login: ""},
				{ID: "", Login: ""},
			}),
		)
		items := m.VisibleItems()
		if len(items) != 0 {
			t.Fatalf("expected empty recents to be ignored, got %d items", len(items))
		}
	})
}

func TestVisibleItems_ReclaimedLoginPreservesListingChannel(t *testing.T) {
	catEN, _ := i18n.New(i18n.LangEN)
	m := New(catEN,
		WithFavorites([]catalog.Channel{
			{ID: "1", Login: "old"},
		}),
	)
	m.list.mode = ModeChannels
	m.list.channels = []catalog.Channel{
		{ID: "1", Login: "new", Viewers: catalog.IntPtr(5)},
		{ID: "9", Login: "old", Viewers: catalog.IntPtr(6)},
	}

	items := m.VisibleItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Type != ItemTypeFavorite || items[0].ID() != "1" || items[0].Login() != "new" {
		t.Fatalf("items[0] = %+v, want Favorite 1/new", items[0])
	}
	if items[1].Type != ItemTypeChannel || items[1].ID() != "9" || items[1].Login() != "old" {
		t.Fatalf("items[1] = %+v, want Channel 9/old", items[1])
	}
}
