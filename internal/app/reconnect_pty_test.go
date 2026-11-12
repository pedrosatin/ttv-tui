//go:build unix

package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

const reconnectHistory = 50

func reconnectWireMessage(i int) string {
	return fmt.Sprintf("@display-name=Bob;id=m-%d :bob!bob@bob.tmi.twitch.tv PRIVMSG #ptychan :msg %02d", i, i)
}

// newReconnectIRC is a server whose first session carries the history. Later
// sessions resend only its last two messages and add one new message, so a
// cleared buffer cannot be refilled by the replay and hide the bug.
func newReconnectIRC() func(context.Context, string) (net.Conn, error) {
	var sessions atomic.Int32
	return func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		session := sessions.Add(1)
		go func() {
			lines := bufio.NewScanner(server)
			for lines.Scan() {
				if !strings.HasPrefix(lines.Text(), "JOIN ") {
					continue
				}
				wire := []string{":tmi.twitch.tv 001 tester :Welcome, GLHF!"}
				if session == 1 {
					for i := 1; i <= reconnectHistory; i++ {
						wire = append(wire, reconnectWireMessage(i))
					}
				} else {
					wire = append(wire,
						reconnectWireMessage(reconnectHistory-1),
						reconnectWireMessage(reconnectHistory),
						"@display-name=Ana;id=m-new :ana!ana@ana.tmi.twitch.tv PRIVMSG #ptychan :depois da lacuna",
					)
				}
				for _, line := range wire {
					if _, err := io.WriteString(server, line+"\r\n"); err != nil {
						return
					}
				}
			}
		}()
		return client, nil
	}
}

func TestCtrlTKeepsHistory_PTY(t *testing.T) {
	cases := []struct {
		lang   string
		locale string
	}{
		{i18n.LangEN, "en_US.UTF-8"},
		{i18n.LangPTBR, "pt_BR.UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			cat, err := i18n.New(tc.lang)
			if err != nil {
				t.Fatalf("i18n.New: %v", err)
			}
			p := startTUIOnPTY(t, "TTV_APP_RICH=1", "TTV_APP_RECONNECT=1", "LANG="+tc.locale, "LC_ALL=", "LC_MESSAGES=")
			p.waitScreen(0, "PtyCat")
			off := p.mark()
			p.openChannel()
			before := p.waitScreen(off, fmt.Sprintf("msg %02d", reconnectHistory))
			t.Logf("PTY before Ctrl+T (%s):\n%s", tc.lang, before)

			off = p.mark()
			p.send([]byte{0x14}) // Ctrl+T
			// Only the first word: the line wraps in the chat column.
			gap := cat.T("chat.system_prefix") + " " + strings.Fields(cat.T("chat.reconnected_gap"))[0]
			after := p.waitScreen(off, "Ana: depois da lacuna", gap, "Bob: msg 35", "Bob: msg 50")
			t.Logf("PTY after Ctrl+T (%s):\n%s", tc.lang, after)

			if empty := fmt.Sprintf(cat.T("chat.empty"), "#ptychan"); strings.Contains(after, empty) {
				t.Errorf("chat went back to the empty text:\n%s", after)
			}
		})
	}
}
