package app

import (
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
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

			deadline := time.Now().Add(10 * time.Second)
			for !strings.Contains(p.screenSince(0), "FilterLangCat") {
				if time.Now().After(deadline) {
					t.Fatalf("categories missing FilterLangCat:\n%s", p.screenSince(0))
				}
				time.Sleep(20 * time.Millisecond)
			}
			home := p.screenSince(0)
			for _, key := range []string{"categories.title", "status.help_hint", "status.quit_hint"} {
				if want := cat.T(key); !strings.Contains(home, want) {
					t.Errorf("home missing %s = %q:\n%s", key, want, home)
				}
			}
			t.Logf("PTY capture (%s) — home:\n%s", tc.lang, home)

			p.sendAndWait([]byte("\r"), "PtLive")

			// Ctrl+W: split → chat → list. Only chat, list and focus reach the bar.
			p.sendAndWait([]byte{0x17}, "layout: "+cat.T("layout.mode.chat"))
			listOff := p.mark()
			p.sendAndWait([]byte{0x17}, "layout: "+cat.T("layout.mode.list"))
			t.Logf("PTY capture (%s) — layout list:\n%s", tc.lang, p.screenSince(listOff))

			helpOff := p.mark()
			p.sendAndWait([]byte("?"), cat.T("help.title"))
			help := p.screenSince(helpOff)
			modes := strings.Join([]string{
				cat.T("layout.mode.split"), cat.T("layout.mode.chat"),
				cat.T("layout.mode.list"), cat.T("layout.mode.focus"),
			}, "/")
			if !strings.Contains(help, modes) {
				t.Errorf("help must name the layout modes as %q:\n%s", modes, help)
			}
			t.Logf("PTY capture (%s) — help:\n%s", tc.lang, help)
			p.send([]byte("?"))

			menuOff := p.mark()
			p.sendAndWait([]byte("l"), cat.T("channel_filter.title"))
			t.Logf("PTY capture (%s) — channel filter menu:\n%s", tc.lang, p.screenSince(menuOff))

			all := p.screenSince(0)
			for _, word := range tc.forbidden {
				if strings.Contains(all, word) {
					t.Errorf("%s screen contains English %q", tc.lang, word)
				}
			}
		})
	}
}
