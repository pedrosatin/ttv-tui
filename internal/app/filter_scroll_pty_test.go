//go:build unix

package app

import (
	"context"
	"fmt"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"strings"
	"testing"
	"time"
)

// filterScrollSource feeds 30 stream languages for the filter menu and a search
// where the exact channel competes with ten categories, nine of them empty.
type filterScrollSource struct{}

func (filterScrollSource) Categories(context.Context, catalog.PageRequest) (catalog.Page[catalog.Category], error) {
	return catalog.Page[catalog.Category]{Items: []catalog.Category{{ID: "cat-many", Name: "ManyLanguages"}}}, nil
}

func (filterScrollSource) Channels(context.Context, string, catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
	items := make([]catalog.Channel, 0, 30)
	for i := range 30 {
		items = append(items, catalog.Channel{
			ID:           fmt.Sprintf("ch-%d", i),
			Login:        fmt.Sprintf("chan%d", i),
			DisplayName:  fmt.Sprintf("Chan%d", i),
			Language:     catalog.LanguagePtr(fmt.Sprintf("%c%c", 'a'+i/26, 'a'+i%26)),
			Viewers:      catalog.IntPtr(100 + i),
			Availability: catalog.AvailabilityLive,
		})
	}
	return catalog.Page[catalog.Channel]{Items: items}, nil
}

func (filterScrollSource) Search(context.Context, string) (catalog.Page[catalog.Channel], error) {
	return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
		{ID: "clone", Login: "gaules_fan", DisplayName: "gaules_fan", Viewers: catalog.IntPtr(9000), Availability: catalog.AvailabilityLive},
		{ID: "exact", Login: "gaules", DisplayName: "Gaules", Viewers: catalog.IntPtr(30000), Availability: catalog.AvailabilityLive},
	}}, nil
}

func (filterScrollSource) SearchCategories(context.Context, string) ([]catalog.Category, error) {
	cats := make([]catalog.Category, 0, 10)
	for i := range 9 {
		cats = append(cats, catalog.Category{ID: fmt.Sprintf("dead-%d", i), Name: fmt.Sprintf("Gale Game %d", i)})
	}
	return append(cats, catalog.Category{ID: "xd", Name: "Pokémon XD: Gale of Darkness", Viewers: catalog.IntPtr(14)}), nil
}

func (filterScrollSource) ChannelsByLogins(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func (filterScrollSource) ChannelsByIDs(context.Context, []string) ([]catalog.Channel, error) {
	return nil, nil
}

func startFilterScrollTUI(t *testing.T) *ptyTUI {
	t.Helper()
	p := startTUIOnPTY(t, "TTV_APP_FILTER_SCROLL=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")
	setSize(t, p.master, 24, 80)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "ManyLanguages") {
		if time.Now().After(deadline) {
			t.Fatalf("categories missing ManyLanguages:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}
	return p
}

// TestFilterMenuScrollsToAudience_PTY walks the real TUI through the whole menu
// at 80x24. Each j must redraw the caret, or it left the screen.
func TestFilterMenuScrollsToAudience_PTY(t *testing.T) {
	p := startFilterScrollTUI(t)
	p.sendAndWait([]byte("\r"), "Chan0")
	p.sendAndWait([]byte("l"), "1/36")
	for i := 2; i <= 36; i++ {
		off := p.mark()
		p.send([]byte("j"))
		p.waitScreen(off, "▸", fmt.Sprintf("%d/36", i))
	}
	p.sendAndWait([]byte("\r"), "(•) 5000+")
}

// TestSearchExactChannelFirst_PTY searches gaules and opens the first result.
func TestSearchExactChannelFirst_PTY(t *testing.T) {
	p := startFilterScrollTUI(t)
	p.send([]byte{0x06})
	p.waitScreen(0, "Search:")
	off := p.mark()
	p.send([]byte("gaules\r"))
	screen := p.waitScreen(off, "▸ ", "Gaules", "Exact channel", "Categories")
	if strings.Index(screen, "Exact channel") > strings.Index(screen, "── Categories ──") {
		t.Fatalf("exact section must come before categories:\n%s", screen)
	}
	p.sendAndWait([]byte("\r"), "gaules")
}
