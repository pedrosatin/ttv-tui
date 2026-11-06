//go:build unix

package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// filterScrollSource feeds 30 stream languages for the filter menu and a search
// where the exact channel competes with ten categories, nine of them empty.
type filterScrollSource struct{}

func (filterScrollSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-many", Name: "ManyLanguages"}}}, nil
}

func (filterScrollSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	items := make([]twitch.Channel, 0, 30)
	for i := range 30 {
		items = append(items, twitch.Channel{
			ID:           fmt.Sprintf("ch-%d", i),
			Login:        fmt.Sprintf("chan%d", i),
			DisplayName:  fmt.Sprintf("Chan%d", i),
			Language:     twitch.LanguagePtr(fmt.Sprintf("%c%c", 'a'+i/26, 'a'+i%26)),
			Viewers:      twitch.IntPtr(100 + i),
			Availability: twitch.AvailabilityLive,
		})
	}
	return twitch.Page[twitch.Channel]{Items: items}, nil
}

func (filterScrollSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "clone", Login: "gaules_fan", DisplayName: "gaules_fan", Viewers: twitch.IntPtr(9000), Availability: twitch.AvailabilityLive},
		{ID: "exact", Login: "gaules", DisplayName: "Gaules", Viewers: twitch.IntPtr(30000), Availability: twitch.AvailabilityLive},
	}}, nil
}

func (filterScrollSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	cats := make([]twitch.Category, 0, 10)
	for i := range 9 {
		cats = append(cats, twitch.Category{ID: fmt.Sprintf("dead-%d", i), Name: fmt.Sprintf("Gale Game %d", i)})
	}
	return append(cats, twitch.Category{ID: "xd", Name: "Pokémon XD: Gale of Darkness", Viewers: twitch.IntPtr(14)}), nil
}

func (filterScrollSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
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
	screen := p.waitScreen(off, "▸ ", "Gaules (gaules)", "Exact channel", "Categories")
	if strings.Index(screen, "Exact channel") > strings.Index(screen, "── Categories ──") {
		t.Fatalf("exact section must come before categories:\n%s", screen)
	}
	p.sendAndWait([]byte("\r"), "gaules")
}
