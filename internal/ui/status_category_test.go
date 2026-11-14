package ui

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

func TestStatusBarShowsChannelCategoryNotLastVisitedList(t *testing.T) {
	cat := mustCatalog(t, i18n.LangPTBR)
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{
				Items: []catalog.Category{
					{ID: "cat-lol", Name: "League of Legends", Viewers: catalog.IntPtr(200000)},
					{ID: "cat-fn", Name: "Fortnite", Viewers: catalog.IntPtr(150000)},
				},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			switch categoryID {
			case "cat-lol":
				return catalog.Page[catalog.Channel]{
					Items: []catalog.Channel{{
						ID: "ch-cae", Login: "caedrel", DisplayName: "Caedrel",
						Viewers: catalog.IntPtr(50000), CategoryID: catalog.StringPtr("cat-lol"),
						CategoryName: catalog.StringPtr("League of Legends"),
						Availability: catalog.AvailabilityLive,
					}},
				}, nil
			case "cat-fn":
				return catalog.Page[catalog.Channel]{
					Items: []catalog.Channel{{
						ID: "ch-fn", Login: "fnstreamer", DisplayName: "FNStreamer",
						Viewers: catalog.IntPtr(1000), CategoryID: catalog.StringPtr("cat-fn"),
						CategoryName: catalog.StringPtr("Fortnite"),
						Availability: catalog.AvailabilityLive,
					}},
				}, nil
			default:
				return catalog.Page[catalog.Channel]{}, nil
			}
		},
	}
	m := New(cat, WithSource(src), WithPlayback(&fakePlayback{}))
	m, cmd := pressKeyMsg(m, m.Init()())
	m = feedLoadedMsgs(t, m, cmd)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})

	// Open League of Legends (first category) and select caedrel.
	m, cmd = pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = feedLoadedMsgs(t, m, cmd)
	if m.Mode() != ModeChannels || m.list.selectedCategory == nil || m.list.selectedCategory.Name != "League of Legends" {
		t.Fatalf("expected League channels, mode=%q cat=%+v", m.Mode(), m.list.selectedCategory)
	}
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "caedrel", Volume: 100, Selection: 1})
	m.player.windowNotice = false

	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "League of Legends") {
		t.Fatalf("playing caedrel must show League of Legends:\n%q", bar)
	}
	if strings.Contains(bar, "Fortnite") {
		t.Fatalf("must not show Fortnite before visiting it:\n%q", bar)
	}

	// Leave to categories, open Fortnite list, keep caedrel playing.
	m.list.mode = ModeCategories
	m.list.channels = nil
	m.list.highlightedID = "cat-fn"
	m, cmd = pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = feedLoadedMsgs(t, m, cmd)
	if m.list.selectedCategory == nil || m.list.selectedCategory.Name != "Fortnite" {
		t.Fatalf("selectedCategory should be Fortnite (list), got %+v", m.list.selectedCategory)
	}

	bar = ansi.Strip(m.StatusBar())
	printScreen(t, "Barra com caedrel ainda em League após visitar Fortnite", m.View(), 120)
	if !strings.Contains(bar, "League of Legends") {
		t.Fatalf("after visiting Fortnite list, bar must keep League of Legends:\n%q", bar)
	}
	if strings.Contains(bar, "Fortnite") {
		t.Fatalf("bar must not show last visited list Fortnite while caedrel plays:\n%q", bar)
	}
}

func TestStatusBarCategoryFromFavoriteRecentSearchAndUnknown(t *testing.T) {
	cat := mustCatalog(t, i18n.LangEN)

	t.Run("favorite", func(t *testing.T) {
		m := New(cat,
			WithFavorites([]catalog.Channel{{
				ID: "f1", Login: "favchan", DisplayName: "FavChan",
				CategoryName: catalog.StringPtr("Valorant"),
				Availability: catalog.AvailabilityLive,
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.list.selectedCategory = &catalog.Category{ID: "cat-fn", Name: "Fortnite"}
		// Favoritos e recentes abrem da tela inicial, com Fortnite ainda selecionada.
		m.list.mode = ModeCategories
		m.list.highlightedID = "f1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "favchan", Volume: 80, Selection: 1})
		m.player.windowNotice = false

		bar := ansi.Strip(m.StatusBar())
		if !strings.Contains(bar, "Valorant") {
			t.Fatalf("favorite open must show channel category Valorant:\n%q", bar)
		}
		if strings.Contains(bar, "Fortnite") {
			t.Fatalf("favorite must not inherit open list Fortnite:\n%q", bar)
		}
	})

	t.Run("recent", func(t *testing.T) {
		m := New(cat,
			WithRecents([]catalog.Channel{{
				ID: "r1", Login: "recchan", DisplayName: "RecChan",
				CategoryName: catalog.StringPtr("Dota 2"),
				Availability: catalog.AvailabilityLive,
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.list.selectedCategory = &catalog.Category{ID: "cat-fn", Name: "Fortnite"}
		// Favoritos e recentes abrem da tela inicial, com Fortnite ainda selecionada.
		m.list.mode = ModeCategories
		m.list.highlightedID = "r1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "recchan", Volume: 80, Selection: 1})
		m.player.windowNotice = false

		bar := ansi.Strip(m.StatusBar())
		if !strings.Contains(bar, "Dota 2") {
			t.Fatalf("recent open must show channel category Dota 2:\n%q", bar)
		}
		if strings.Contains(bar, "Fortnite") {
			t.Fatalf("recent must not inherit open list Fortnite:\n%q", bar)
		}
	})

	t.Run("search", func(t *testing.T) {
		src := &fakeSource{
			searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
				return catalog.Page[catalog.Channel]{
					Items: []catalog.Channel{{
						ID: "s1", Login: "searchchan", DisplayName: "SearchChan",
						CategoryName: catalog.StringPtr("Minecraft"),
						Availability: catalog.AvailabilityLive,
					}},
				}, nil
			},
		}
		m := New(cat, WithSource(src), WithPlayback(&fakePlayback{}))
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.list.selectedCategory = &catalog.Category{ID: "cat-fn", Name: "Fortnite"}
		m.list.mode = ModeSearch
		m.list.search.query = "search"
		m.list.channels = []catalog.Channel{{
			ID: "s1", Login: "searchchan", DisplayName: "SearchChan",
			CategoryName: catalog.StringPtr("Minecraft"),
			Availability: catalog.AvailabilityLive,
		}}
		m.list.highlightedID = "s1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "searchchan", Volume: 80, Selection: 1})
		m.player.windowNotice = false

		bar := ansi.Strip(m.StatusBar())
		if !strings.Contains(bar, "Minecraft") {
			t.Fatalf("search open must show channel category Minecraft:\n%q", bar)
		}
		if strings.Contains(bar, "Fortnite") {
			t.Fatalf("search must not inherit open list Fortnite:\n%q", bar)
		}
	})

	t.Run("unknown_omitted", func(t *testing.T) {
		m := New(cat,
			WithFavorites([]catalog.Channel{{
				ID: "f2", Login: "mystery", DisplayName: "Mystery",
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.list.selectedCategory = &catalog.Category{ID: "cat-fn", Name: "Fortnite"}
		// Favoritos e recentes abrem da tela inicial, com Fortnite ainda selecionada.
		m.list.mode = ModeCategories
		m.list.highlightedID = "f2"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "mystery", Volume: 80, Selection: 1})
		m.player.windowNotice = false

		bar := ansi.Strip(m.StatusBar())
		if strings.Contains(bar, "Fortnite") {
			t.Fatalf("unknown category must omit list name Fortnite:\n%q", bar)
		}
		printScreen(t, "Barra sem categoria quando desconhecida", m.View(), 120)
	})
}

func TestStatusBarCategoryUpdatesOnChannelSwitch(t *testing.T) {
	cat := mustCatalog(t, i18n.LangEN)
	m := New(cat, WithPlayback(&fakePlayback{}),
		WithFavorites([]catalog.Channel{
			{ID: "a", Login: "alpha", DisplayName: "Alpha", CategoryName: catalog.StringPtr("Chess")},
			{ID: "b", Login: "bravo", DisplayName: "Bravo", CategoryName: catalog.StringPtr("Poker")},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	// Favoritos abrem da tela inicial.
	m.list.mode = ModeCategories
	m.list.highlightedID = "a"
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "alpha", Volume: 70, Selection: 1})
	m.player.windowNotice = false
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, "Chess") || strings.Contains(bar, "Poker") {
		t.Fatalf("first channel category:\n%q", bar)
	}

	m.list.highlightedID = "b"
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "bravo", Volume: 70, Selection: 2})
	m.player.windowNotice = false
	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "Poker") {
		t.Fatalf("switch must update category to Poker:\n%q", bar)
	}
	if strings.Contains(bar, "Chess") {
		t.Fatalf("old category Chess must leave the bar:\n%q", bar)
	}
}

func TestStatusBarCategoryFits80And120(t *testing.T) {
	cat := mustCatalog(t, i18n.LangPTBR)
	for _, width := range []int{80, 120} {
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			m, _, _ := setupTwoPanelModel(t)
			m.catalog = cat
			m.player.windowNotice = false
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
			if m.list.selectedChannel == nil || channelCategoryName(m.list.selectedChannel) != "Counter-Strike" {
				t.Fatalf("selected channel category = %q, want Counter-Strike (stamped from list)", channelCategoryName(m.list.selectedChannel))
			}
			bar := ansi.Strip(m.StatusBar())
			assertStatusWidth(t, bar, width)
			assertAllLinesFit(t, m.View(), width)
			if width == 120 && !strings.Contains(bar, "Counter-Strike") {
				t.Fatalf("120-col bar from category open must show Counter-Strike:\n%q", bar)
			}
		})
	}
}
