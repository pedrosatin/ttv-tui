package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func TestStatusBarShowsChannelCategoryNotLastVisitedList(t *testing.T) {
	cat := mustCatalog(t, i18n.LangPTBR)
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page twitch.PageRequest) (twitch.Page[twitch.Category], error) {
			return twitch.Page[twitch.Category]{
				Items: []twitch.Category{
					{ID: "cat-lol", Name: "League of Legends", Viewers: twitch.IntPtr(200000)},
					{ID: "cat-fn", Name: "Fortnite", Viewers: twitch.IntPtr(150000)},
				},
			}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
			switch categoryID {
			case "cat-lol":
				return twitch.Page[twitch.Channel]{
					Items: []twitch.Channel{{
						ID: "ch-cae", Login: "caedrel", DisplayName: "Caedrel",
						Viewers: twitch.IntPtr(50000), CategoryID: twitch.StringPtr("cat-lol"),
						CategoryName: twitch.StringPtr("League of Legends"),
						Availability: twitch.AvailabilityLive,
					}},
				}, nil
			case "cat-fn":
				return twitch.Page[twitch.Channel]{
					Items: []twitch.Channel{{
						ID: "ch-fn", Login: "fnstreamer", DisplayName: "FNStreamer",
						Viewers: twitch.IntPtr(1000), CategoryID: twitch.StringPtr("cat-fn"),
						CategoryName: twitch.StringPtr("Fortnite"),
						Availability: twitch.AvailabilityLive,
					}},
				}, nil
			default:
				return twitch.Page[twitch.Channel]{}, nil
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
	if m.Mode() != ModeChannels || m.selectedCategory == nil || m.selectedCategory.Name != "League of Legends" {
		t.Fatalf("expected League channels, mode=%q cat=%+v", m.Mode(), m.selectedCategory)
	}
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "caedrel", Volume: 100, FromStart: true})
	m.playerWindowNotice = false

	bar := ansi.Strip(m.StatusBar())
	if !strings.Contains(bar, "League of Legends") {
		t.Fatalf("playing caedrel must show League of Legends:\n%q", bar)
	}
	if strings.Contains(bar, "Fortnite") {
		t.Fatalf("must not show Fortnite before visiting it:\n%q", bar)
	}

	// Leave to categories, open Fortnite list, keep caedrel playing.
	m.mode = ModeCategories
	m.channels = nil
	m.highlightedID = "cat-fn"
	m, cmd = pressKeyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = feedLoadedMsgs(t, m, cmd)
	if m.selectedCategory == nil || m.selectedCategory.Name != "Fortnite" {
		t.Fatalf("selectedCategory should be Fortnite (list), got %+v", m.selectedCategory)
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
			WithFavorites([]twitch.Channel{{
				ID: "f1", Login: "favchan", DisplayName: "FavChan",
				CategoryName: twitch.StringPtr("Valorant"),
				Availability: twitch.AvailabilityLive,
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.selectedCategory = &twitch.Category{ID: "cat-fn", Name: "Fortnite"}
		m.mode = ModeChannels
		m.highlightedID = "f1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "favchan", Volume: 80, FromStart: true})
		m.playerWindowNotice = false

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
			WithRecents([]twitch.Channel{{
				ID: "r1", Login: "recchan", DisplayName: "RecChan",
				CategoryName: twitch.StringPtr("Dota 2"),
				Availability: twitch.AvailabilityLive,
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.selectedCategory = &twitch.Category{ID: "cat-fn", Name: "Fortnite"}
		m.mode = ModeChannels
		m.highlightedID = "r1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "recchan", Volume: 80, FromStart: true})
		m.playerWindowNotice = false

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
			searchFn: func(ctx context.Context, query string) (twitch.Page[twitch.Channel], error) {
				return twitch.Page[twitch.Channel]{
					Items: []twitch.Channel{{
						ID: "s1", Login: "searchchan", DisplayName: "SearchChan",
						CategoryName: twitch.StringPtr("Minecraft"),
						Availability: twitch.AvailabilityLive,
					}},
				}, nil
			},
		}
		m := New(cat, WithSource(src), WithPlayback(&fakePlayback{}))
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.selectedCategory = &twitch.Category{ID: "cat-fn", Name: "Fortnite"}
		m.mode = ModeSearch
		m.searchQuery = "search"
		m.channels = []twitch.Channel{{
			ID: "s1", Login: "searchchan", DisplayName: "SearchChan",
			CategoryName: twitch.StringPtr("Minecraft"),
			Availability: twitch.AvailabilityLive,
		}}
		m.highlightedID = "s1"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "searchchan", Volume: 80, FromStart: true})
		m.playerWindowNotice = false

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
			WithFavorites([]twitch.Channel{{
				ID: "f2", Login: "mystery", DisplayName: "Mystery",
			}}),
			WithPlayback(&fakePlayback{}),
		)
		m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
		m.selectedCategory = &twitch.Category{ID: "cat-fn", Name: "Fortnite"}
		m.mode = ModeChannels
		m.highlightedID = "f2"
		m = openChannel(t, m)
		m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "mystery", Volume: 80, FromStart: true})
		m.playerWindowNotice = false

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
		WithFavorites([]twitch.Channel{
			{ID: "a", Login: "alpha", DisplayName: "Alpha", CategoryName: twitch.StringPtr("Chess")},
			{ID: "b", Login: "bravo", DisplayName: "Bravo", CategoryName: twitch.StringPtr("Poker")},
		}),
	)
	m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 24})
	m.mode = ModeChannels
	m.highlightedID = "a"
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "alpha", Volume: 70, FromStart: true})
	m.playerWindowNotice = false
	if bar := ansi.Strip(m.StatusBar()); !strings.Contains(bar, "Chess") || strings.Contains(bar, "Poker") {
		t.Fatalf("first channel category:\n%q", bar)
	}

	m.highlightedID = "b"
	m = openChannel(t, m)
	m, _ = pressKeyMsg(m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "bravo", Volume: 70, FromStart: true})
	m.playerWindowNotice = false
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
			m.playerWindowNotice = false
			m, _ = pressKeyMsg(m, tea.WindowSizeMsg{Width: width, Height: 24})
			if m.selectedChannel == nil || channelCategoryName(m.selectedChannel) != "Counter-Strike" {
				t.Fatalf("selected channel category = %q, want Counter-Strike (stamped from list)", channelCategoryName(m.selectedChannel))
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
