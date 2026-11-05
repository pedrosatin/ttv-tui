//go:build unix

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// channelFilterSource feeds bilingual channels so the filter menu can list PT/EN
// and a 500+ audience cut leaves only the Portuguese mid-size stream.
type channelFilterSource struct{}

func (channelFilterSource) Categories(context.Context, twitch.PageRequest) (twitch.Page[twitch.Category], error) {
	return twitch.Page[twitch.Category]{Items: []twitch.Category{{ID: "cat-cf", Name: "FilterLangCat"}}}, nil
}

func (channelFilterSource) Channels(context.Context, string, twitch.PageRequest) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{Items: []twitch.Channel{
		{ID: "ch-pt", Login: "ptlive", DisplayName: "PtLive", Language: twitch.LanguagePtr("PT"), Viewers: twitch.IntPtr(750), Availability: twitch.AvailabilityLive},
		{ID: "ch-en", Login: "enlive", DisplayName: "EnLive", Language: twitch.LanguagePtr("EN"), Viewers: twitch.IntPtr(2000), Availability: twitch.AvailabilityLive},
		{ID: "ch-small", Login: "smallpt", DisplayName: "SmallPt", Language: twitch.LanguagePtr("PT"), Viewers: twitch.IntPtr(20), Availability: twitch.AvailabilityLive},
	}}, nil
}

func (channelFilterSource) Search(context.Context, string) (twitch.Page[twitch.Channel], error) {
	return twitch.Page[twitch.Channel]{}, nil
}

func (channelFilterSource) SearchCategories(context.Context, string) ([]twitch.Category, error) {
	return nil, nil
}

func (channelFilterSource) ChannelsByLogins(context.Context, []string) ([]twitch.Channel, error) {
	return nil, nil
}

// TestChannelFilterMenu_PTY opens the filter menu, selects PT and 500+, and
// captures the PTY screen with the menu open and the active filters applied.
func TestChannelFilterMenu_PTY(t *testing.T) {
	p := startTUIOnPTY(t, "TTV_APP_CHANNEL_FILTER=1", "LANG=en_US.UTF-8", "LC_ALL=", "LC_MESSAGES=")

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "FilterLangCat") {
		if time.Now().After(deadline) {
			t.Fatalf("categories missing FilterLangCat:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.sendAndWait([]byte("\r"), "PtLive")
	deadline = time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screenSince(0), "EnLive") || !strings.Contains(p.screenSince(0), "SmallPt") {
		if time.Now().After(deadline) {
			t.Fatalf("channel list incomplete:\n%s", p.screenSince(0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	menuOff := p.mark()
	p.sendAndWait([]byte("l"), "Channel filters (l/Esc close)")
	screenMenu := p.waitScreen(menuOff, "PT (2)", "EN (1)", "500+")
	if !strings.Contains(screenMenu, "PT (2)") || !strings.Contains(screenMenu, "EN (1)") {
		t.Fatalf("menu must list languages with counts:\n%s", screenMenu)
	}
	if !strings.Contains(screenMenu, "500+") {
		t.Fatalf("menu must list audience bands:\n%s", screenMenu)
	}
	t.Logf("PTY capture — filter menu open:\n%s", screenMenu)

	// Languages are sorted: All → EN → PT. Land on PT, then toggle.
	p.sendAndWait([]byte("j"), "[ ] EN")
	p.sendAndWait([]byte("j"), "[ ] PT")
	p.sendAndWait([]byte("\r"), "[x] PT")

	// From PT: EN was above; down goes Any → 100 → 500.
	p.sendAndWait([]byte("j"), "Any audience")
	p.sendAndWait([]byte("j"), "100+")
	p.sendAndWait([]byte("j"), "500+")
	p.sendAndWait([]byte("\r"), "PT · 500+")

	closeOff := p.mark()
	p.send([]byte("\x1b"))
	deadline = time.Now().Add(10 * time.Second)
	for {
		full := p.screenSince(0)
		since := p.screenSince(closeOff)
		hasStatus := strings.Contains(full, "PT · 500+")
		// Esc redraw may not reprint PtLive if that cell was unchanged; the
		// cumulative screen still has it from the channel list load.
		hasPt := strings.Contains(full, "PtLive")
		menuClosed := !strings.Contains(since, "l/Esc close")
		if hasStatus && hasPt && menuClosed {
			t.Logf("PTY capture — PT · 500+ active:\n%s", full)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected closed menu with PT · 500+ and PtLive; since Esc:\n%s\nfull:\n%s", since, full)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
