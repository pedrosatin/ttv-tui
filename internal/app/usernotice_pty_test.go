//go:build unix

package app

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// userNoticeWire is what Twitch writes for each kind, tags included: the PTY
// below must go through the real parser, not through hand-built messages.
var userNoticeWire = []string{
	":tmi.twitch.tv 001 tester :Welcome, GLHF!",
	"@badges=;display-name=Raider;id=n-raid;login=raider;msg-id=raid;msg-param-viewerCount=1234;system-msg=1234\\sraiders\\sfrom\\sRaider\\shave\\sjoined! :tmi.twitch.tv USERNOTICE #ptychan",
	"@display-name=Nova;id=n-sub;login=nova;msg-id=sub;system-msg=Nova\\ssubscribed\\sat\\sTier\\s1. :tmi.twitch.tv USERNOTICE #ptychan",
	"@display-name=Santa;id=n-gift;login=santa;msg-id=subgift;system-msg=Santa\\sgifted\\sa\\ssub\\sto\\sKid! :tmi.twitch.tv USERNOTICE #ptychan",
	"@badges=moderator/1;display-name=ModOne;id=n-ann;login=modone;mod=1;msg-id=announcement;msg-param-color=PRIMARY;system-msg= :tmi.twitch.tv USERNOTICE #ptychan :Sorteio 20h",
	"@display-name=Bob;id=n-chat :bob!bob@bob.tmi.twitch.tv PRIVMSG #ptychan :oi gente",
}

// dialUserNoticeIRC answers the JOIN with userNoticeWire over an in-memory pipe
// and then keeps draining the client, so the connection stays up.
func dialUserNoticeIRC(context.Context, string) (net.Conn, error) {
	client, server := net.Pipe()
	go func() {
		lines := bufio.NewScanner(server)
		for lines.Scan() {
			if !strings.HasPrefix(lines.Text(), "JOIN ") {
				continue
			}
			for _, line := range userNoticeWire {
				if _, err := io.WriteString(server, line+"\r\n"); err != nil {
					return
				}
			}
		}
	}()
	return client, nil
}

func TestUserNotice_PTY(t *testing.T) {
	cases := []struct {
		lang         string
		locale       string
		announcement string
	}{
		{i18n.LangEN, "en_US.UTF-8", "Announcement from ModOne: Sorteio 20h"},
		{i18n.LangPTBR, "pt_BR.UTF-8", "Anúncio de ModOne: Sorteio 20h"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			cat, err := i18n.New(tc.lang)
			if err != nil {
				t.Fatalf("i18n.New: %v", err)
			}
			p := startTUIOnPTY(t, "TTV_APP_RICH=1", "TTV_APP_USERNOTICE=1", "LANG="+tc.locale, "LC_ALL=", "LC_MESSAGES=")
			p.waitScreen(0, "PtyCat")
			off := p.mark()
			p.openChannel()

			system := cat.T("chat.system_prefix") + " "
			screen := p.waitScreen(off,
				"★ [raid] 1234 raiders from Raider",
				system+"Nova subscribed at Tier 1.",
				system+"Santa gifted a sub to Kid!",
				system+tc.announcement,
				"Bob: oi gente",
			)
			t.Logf("PTY capture (%s):\n%s", tc.lang, screen)

			if strings.Contains(screen, cat.T("chat.author.mod")) {
				t.Errorf("announcement carries the author badge:\n%s", screen)
			}
			if got := strings.Count(screen, system+"[raid]") + strings.Count(screen, "★ "+system); got != 0 {
				t.Errorf("raid wears the plain system marker:\n%s", screen)
			}
		})
	}
}
