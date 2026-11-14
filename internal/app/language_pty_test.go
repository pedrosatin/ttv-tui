//go:build unix

package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// TestLanguageConsistency_PTY drives the real TUI on a PTY in each language
// through home, a non-default layout, the help and the channel filter menu, and
// requires every word the app writes to come from that language's catalog.
func TestLanguageConsistency_PTY(t *testing.T) {
	cases := []struct {
		lang   string
		locale string
		// English words that must never show up in the pt-BR screen.
		forbidden []string
	}{
		{i18n.LangEN, "en_US.UTF-8", nil},
		{i18n.LangPTBR, "pt_BR.UTF-8", []string{"split/chat/list/focus", "layout: list ", "layout: focus", "viewers", "Space ", "Live categories", "Channel filters"}},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			cat, err := i18n.New(tc.lang)
			if err != nil {
				t.Fatalf("i18n.New: %v", err)
			}
			p := startTUIOnPTY(t, "TTV_APP_CHANNEL_FILTER=1", "LANG="+tc.locale, "LC_ALL=", "LC_MESSAGES=")

			// The bar closes the same frame that opens with the list: seeing the
			// category proves nothing about it, so the wait names both.
			home := p.waitScreen(0, "FilterLangCat", cat.T("categories.title"),
				ui.CatalogText(cat, "status.help_hint"), ui.CatalogText(cat, "status.quit_hint"))
			t.Logf("PTY capture (%s) — home:\n%s", tc.lang, home)

			// No channel yet: Ctrl+W has no layout to offer and says so.
			p.sendAndWait([]byte{0x17}, ui.CatalogText(cat, "layout.no_chat"))

			p.sendAndWait([]byte("\r"), "PtLive")

			// The layouts arrange the chat, so they start with an open channel.
			oldPid := p.playerPid
			p.sendAndWait([]byte("\r"), fmt.Sprintf(cat.T("chat.title"), ""))
			p.followPlayer(oldPid)

			// Ctrl+W: split → chat → list. Only chat, list and focus reach the bar.
			p.sendAndWait([]byte{0x17}, "layout: "+cat.T("layout.mode.chat"))
			listOff := p.mark()
			p.sendAndWait([]byte{0x17}, "layout: "+cat.T("layout.mode.list"))
			t.Logf("PTY capture (%s) — layout list:\n%s", tc.lang, p.screenSince(listOff))

			helpOff := p.mark()
			p.sendAndWait([]byte("?"), cat.T("help.title"))
			modes := strings.Join([]string{
				cat.T("layout.mode.split"), cat.T("layout.mode.chat"),
				cat.T("layout.mode.list"), cat.T("layout.mode.focus"),
			}, "/")
			help := p.waitScreen(helpOff, modes)
			t.Logf("PTY capture (%s) — help:\n%s", tc.lang, help)
			p.send([]byte("?"))

			menuOff := p.mark()
			p.sendAndWait([]byte("l"), ui.CatalogText(cat, "channel_filter.title"))
			// The scan below only proves absence over a whole frame, so wait for
			// the status line that closes the menu's.
			menu := p.waitScreen(menuOff, cat.T("channel_filter.status_open"), ui.CatalogText(cat, "status.quit_hint"))
			t.Logf("PTY capture (%s) — channel filter menu:\n%s", tc.lang, menu)

			all := p.screenSince(0)
			for _, word := range tc.forbidden {
				if strings.Contains(all, word) {
					t.Errorf("%s screen contains English %q", tc.lang, word)
				}
			}
		})
	}
}
