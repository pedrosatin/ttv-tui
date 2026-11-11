package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

func listErrorModel(t *testing.T, lang string) Model {
	t.Helper()
	cat, err := i18n.New(lang)
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}
	m := New(cat, WithFavorites([]twitch.Channel{
		{ID: "fav-a", Login: "alpha", DisplayName: "Alpha"},
		{ID: "fav-b", Login: "beta", DisplayName: "Beta"},
		{ID: "fav-c", Login: "gamma", DisplayName: "Gamma"},
	}))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	return updated.(Model)
}

func loadFailure() error {
	return &twitch.Error{Kind: twitch.Rede, Op: "channels", Err: errors.New("dial tcp: timeout")}
}

// The load error replaces every row on screen. j, the arrows, f and Enter must
// not reach the favorites it hides.
func TestListKeysIgnoreRowsHiddenByLoadError(t *testing.T) {
	m := listErrorModel(t, i18n.LangEN)
	m.setHighlightToItem(m.VisibleItems(), 1)
	updated, _ := m.Update(CategoriesLoadedMsg{Generation: m.list.generation, Err: loadFailure()})
	m = updated.(Model)
	if m.list.err == nil {
		t.Fatal("fixture: list error not set")
	}
	before := m.HighlightedID()

	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
	} {
		moved, _ := m.Update(k)
		if got := moved.(Model).HighlightedID(); got != before {
			t.Errorf("%s moved the highlight under the error: %q -> %q", k, before, got)
		}
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if sel, ok := cmd().(ChannelSelectedMsg); ok {
			t.Errorf("Enter under the error opened hidden channel %q", sel.Channel.Login)
		}
	}

	toggled, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if n := len(toggled.(Model).Favorites()); n != 3 {
		t.Errorf("f under the error left %d favorites, want 3", n)
	}
}

// / clears the error without a new load. The empty list must say the data did
// not load, not that the category has no live channels.
func TestClearedLoadErrorSaysListDidNotLoad(t *testing.T) {
	for _, tc := range []struct {
		lang, notLoaded, empty string
	}{
		{i18n.LangEN, "The list did not load.", "No live channels in this category."},
		{i18n.LangPTBR, "A lista não carregou.", "Nenhum canal ao vivo nesta categoria."},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			m := listErrorModel(t, tc.lang)
			m.list.favorites = nil
			m.list.mode = ModeChannels
			m.list.selectedCategory = &twitch.Category{ID: "cat-1", Name: "Just Chatting"}
			m.list.loading = true
			updated, _ := m.Update(ChannelsLoadedMsg{Generation: m.list.generation, Err: loadFailure()})
			m = updated.(Model)

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
			m = updated.(Model)
			if m.list.err != nil {
				t.Fatal("fixture: / did not clear the error")
			}

			view := plainView(m)
			if strings.Contains(view, tc.empty) {
				t.Fatalf("view presents the failed load as an empty category:\n%s", view)
			}
			if !strings.Contains(view, tc.notLoaded) {
				t.Fatalf("view does not say the list did not load (%q):\n%s", tc.notLoaded, view)
			}
		})
	}
}

// A batch lookup that omits the stream field has no evidence either way. Inside
// a category listing the favorite must stay unverified, not turn live.
func TestHydrationWithoutStreamKeepsFavoriteUnverified(t *testing.T) {
	m := listErrorModel(t, i18n.LangEN)
	m.list.mode = ModeChannels
	m.list.selectedCategory = &twitch.Category{ID: "cat-1", Name: "Just Chatting"}

	updated, _ := m.Update(SavedAvailabilityMsg{
		Generation: m.list.hydrateGen,
		Channels:   []twitch.Channel{{ID: "1", Login: "alpha", DisplayName: "Alpha"}},
	})
	m = updated.(Model)

	for _, it := range m.VisibleItems() {
		if it.Login() != "alpha" {
			continue
		}
		if it.Availability != twitch.AvailabilityUnknown {
			t.Fatalf("alpha availability = %v, want unknown", it.Availability)
		}
		if view := plainView(m); !strings.Contains(view, "[unverified]") {
			t.Fatalf("alpha row lacks [unverified]:\n%s", view)
		}
		return
	}
	t.Fatalf("alpha not in the list: %v", visibleLogins(m.VisibleItems()))
}
