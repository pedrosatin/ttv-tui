package fixture

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// IRCConfig controla o volume do chat de fixture.
type IRCConfig struct {
	// Messages é quantas mensagens a sala recebe logo depois do JOIN.
	Messages int
	// Every, quando maior que zero, manda mais uma mensagem a cada intervalo
	// até a conexão fechar.
	Every time.Duration
}

// IRCServer fala o pedaço do IRC da Twitch que internal/chat usa, em TCP sem TLS.
type IRCServer struct {
	cfg IRCConfig
	ln  net.Listener

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// NewIRCServer escuta em 127.0.0.1, numa porta livre.
func NewIRCServer(cfg IRCConfig) (*IRCServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("escutar o IRC de fixture: %w", err)
	}
	s := &IRCServer{cfg: cfg, ln: ln, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Addr é o host:porta para TTV_TUI_TEST_IRC.
func (s *IRCServer) Addr() string { return s.ln.Addr().String() }

// Close fecha o servidor e as conexões abertas.
func (s *IRCServer) Close() {
	_ = s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *IRCServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(c)
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
			_ = c.Close()
		}()
	}
}

// MessageText é o texto da n-ésima mensagem de fixture (a partir de 1).
func MessageText(n int) string { return fmt.Sprintf("mensagem de fixture %d", n) }

// MessageUser é quem manda a n-ésima mensagem.
func MessageUser(n int) string { return fmt.Sprintf("espectador%d", (n-1)%5+1) }

func (s *IRCServer) serve(c net.Conn) {
	var wmu sync.Mutex
	write := func(line string) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := fmt.Fprintf(c, "%s\r\n", line)
		return err
	}
	done := make(chan struct{})
	defer close(done)
	nick := "justinfan"
	privmsg := func(channel string, n int) error {
		user := MessageUser(n)
		return write(fmt.Sprintf("@badges=;color=#1E90FF;display-name=%s;emotes=;id=fx-%d;room-id=1;tmi-sent-ts=1700000000000;user-id=%d :%s!%s@%s.tmi.twitch.tv PRIVMSG #%s :%s",
			user, n, 100+n, user, user, user, channel, MessageText(n)))
	}
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		cmd, rest, _ := strings.Cut(line, " ")
		switch cmd {
		case "NICK":
			nick = strings.TrimSpace(rest)
			if write(":tmi.twitch.tv 001 "+nick+" :Welcome, GLHF!") != nil {
				return
			}
		case "PING":
			if write("PONG :"+strings.TrimPrefix(rest, ":")) != nil {
				return
			}
		case "JOIN":
			channel := strings.TrimPrefix(strings.TrimSpace(rest), "#")
			if write(fmt.Sprintf(":%s!%s@%s.tmi.twitch.tv JOIN #%s", nick, nick, nick, channel)) != nil ||
				write(fmt.Sprintf("@emote-only=0;followers-only=-1;r9k=0;room-id=1;slow=0;subs-only=0 :tmi.twitch.tv ROOMSTATE #%s", channel)) != nil {
				return
			}
			for n := 1; n <= s.cfg.Messages; n++ {
				if privmsg(channel, n) != nil {
					return
				}
			}
			if s.cfg.Every > 0 {
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					t := time.NewTicker(s.cfg.Every)
					defer t.Stop()
					for n := s.cfg.Messages + 1; ; n++ {
						select {
						case <-done:
							return
						case <-t.C:
						}
						if privmsg(channel, n) != nil {
							return
						}
					}
				}()
			}
		}
	}
}
