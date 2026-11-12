package app

import (
	"bufio"
	"context"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

func TestChatAuthFailureRemainsVisible(t *testing.T) {
	for _, tc := range []struct{ lang, text string }{
		{"en", "Chat login refused. Run ttv-tui login and restart the app."},
		{"pt-BR", "Login do chat recusado. Execute ttv-tui login e reinicie o app."},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			mgr := chat.New(chat.Options{Token: "fixture", Username: "tester", InitialBackoff: time.Millisecond, MaxBackoff: 4 * time.Millisecond,
				Dial: func(context.Context, string) (net.Conn, error) {
					client, server := net.Pipe()
					go func() {
						defer func() { _ = server.Close() }()
						scanner := bufio.NewScanner(server)
						for scanner.Scan() {
							if strings.HasPrefix(scanner.Text(), "JOIN ") {
								_, _ = io.WriteString(server, ":srv NOTICE * :Login authentication failed\r\n")
								return
							}
						}
					}()
					return client, nil
				},
			})
			defer mgr.Close()
			cat, err := i18n.New(tc.lang)
			if err != nil {
				t.Fatal(err)
			}
			m := ui.New(cat)
			update := func(msg tea.Msg) { next, _ := m.Update(msg); m = next.(ui.Model) }
			update(tea.WindowSizeMsg{Width: 160, Height: 30})
			update(ui.ChannelSelectedMsg{Channel: catalog.Channel{ID: "1", Login: "test"}})
			if _, err := mgr.JoinLogin("test"); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(time.Second)
		events:
			for {
				select {
				case ev := <-mgr.Events():
					update(mapChatEvent(platform.Twitch, ev))
					if ev.State == chat.StateError {
						break events
					}
				case <-deadline:
					t.Fatal("no terminal authentication error")
				}
			}
			for i, width := range []int{160, 80, 120} {
				update(tea.WindowSizeMsg{Width: width, Height: 30 + i})
				want := tc.text
				if width < 160 {
					want = "ttv-tui login"
				}
				if view := m.View(); !strings.Contains(strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(view), "│", " ")), " "), want) {
					t.Fatalf("missing localized error %q in %s", tc.text, view)
				}
			}
			update(ui.ChannelSelectedMsg{Channel: catalog.Channel{ID: "2", Login: "other"}})
			if strings.Contains(m.View(), tc.text) {
				t.Fatal("error survived channel change")
			}
		})
	}
}
