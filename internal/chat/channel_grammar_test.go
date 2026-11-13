package chat

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
)

// Canais que viram linha ou alvo extra no IRC se chegarem ao protocolo.
var canaisForaDaGramatica = []struct {
	name, login string
}{
	{"crlf", "a\r\nb"},
	{"lf", "a\nprivmsg #x :y"},
	{"cr no fim", "gaules\r"},
	{"vírgula e outro canal", "a,#b"},
	{"espaço", "a b"},
	{"espaço nas pontas", " gaules "},
	{"nul", "a\x00b"},
	{"cerquilha interna", "a#b"},
	{"duas cerquilhas", "##gaules"},
	{"vazio", ""},
	{"só cerquilha", "#"},
	{"hífen", "a-b"},
	{"fora do ascii", "gaulés"},
	{"sinal kelvin", "K"},
	{"26 caracteres", strings.Repeat("a", 26)},
	{"url colada", "dummychan\r\nprivmsg #victimchan :injected text\r\nping"},
}

func TestJoinLoginRecusaCanalForaDaGramaticaSemEscreverNoSocket(t *testing.T) {
	srv := newFakeIRCServer(t)
	var conexoes atomic.Int32
	handshake := make(chan []string, 1)
	extra := make(chan string, 16)
	srv.handlers <- func(conn net.Conn) {
		conexoes.Add(1)
		r := bufio.NewReader(conn)
		var linhas []string
		for len(linhas) < 3 {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			linhas = append(linhas, l)
		}
		handshake <- linhas
		_, _ = io.WriteString(conn, ":tmi.twitch.tv 001 justinfan1 :Welcome\r\n")
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			extra <- l
		}
	}

	mgr := New(Options{Addr: srv.Addr(), Dial: srv.Dial, CoalesceInterval: 10 * time.Millisecond})
	t.Cleanup(mgr.Close)
	gen, err := mgr.JoinLogin("gaules")
	if err != nil {
		t.Fatalf("JoinLogin válido: %v", err)
	}
	select {
	case linhas := <-handshake:
		if linhas[2] != "JOIN #gaules\r\n" {
			t.Fatalf("JOIN = %q", linhas[2])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handshake não chegou")
	}
	waitConnected(t, mgr, gen)

	for _, tc := range canaisForaDaGramatica {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mgr.JoinLogin(tc.login)
			if !errors.Is(err, ErrInvalidChannel) || got != 0 {
				t.Fatalf("JoinLogin(%q) = %d, %v; want 0, ErrInvalidChannel", tc.login, got, err)
			}
			if strings.ContainsAny(err.Error(), "\r\n\x00") {
				t.Fatalf("o erro repete bytes do canal: %q", err.Error())
			}
			_, err = mgr.Join(context.Background(), catalog.Channel{Platform: "twitch", Login: tc.login})
			if !errors.Is(err, ErrInvalidChannel) {
				t.Fatalf("Join(%q): err = %v, want ErrInvalidChannel", tc.login, err)
			}
		})
	}

	if ch := mgr.Channel(); ch != "gaules" {
		t.Fatalf("canal da sessão = %q, want gaules", ch)
	}
	select {
	case l := <-extra:
		t.Fatalf("o socket recebeu %q depois de um canal recusado", l)
	case <-time.After(200 * time.Millisecond):
	}
	if n := conexoes.Load(); n != 1 {
		t.Fatalf("conexões = %d, want 1", n)
	}
}

func TestHandshakeRecusaCanalForaDaGramatica(t *testing.T) {
	mgr := New(Options{Token: "dummy", Username: "tester"})
	t.Cleanup(mgr.Close)
	for _, tc := range canaisForaDaGramatica {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := mgr.writeHandshake(&buf, tc.login, false); !errors.Is(err, ErrInvalidChannel) {
				t.Fatalf("writeHandshake(%q): err = %v, want ErrInvalidChannel", tc.login, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("handshake escreveu %q", buf.String())
			}
		})
	}
}

func TestFormatPrivMsgRecusaCanalForaDaGramatica(t *testing.T) {
	for _, tc := range canaisForaDaGramatica {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := FormatPrivMsg(tc.login, "n1", "oi")
			if !errors.Is(err, ErrInvalidChannel) || wire != "" {
				t.Fatalf("FormatPrivMsg(%q) = %q, %v; want \"\", ErrInvalidChannel", tc.login, wire, err)
			}
		})
	}
	wire, err := FormatPrivMsg("#Gaules", "n1", "oi")
	if err != nil || wire != "@client-nonce=n1 PRIVMSG #gaules :oi\r\n" {
		t.Fatalf("FormatPrivMsg válido = %q, %v", wire, err)
	}
}

// Moderação e estado de outra sala não mexem na sessão: CLEARCHAT, CLEARMSG,
// ROOMSTATE e NOTICE de outro canal são descartados, como PRIVMSG já era.
func TestEventosDeOutroCanalNaoMudamASessao(t *testing.T) {
	srv := newFakeIRCServer(t)
	srv.handlers <- func(conn net.Conn) {
		if _, err := readClientLines(conn, 3, 2*time.Second); err != nil {
			return
		}
		for _, l := range []string{
			":tmi.twitch.tv 001 justinfan1 :Welcome",
			"@slow=30;subs-only=1 :tmi.twitch.tv ROOMSTATE #outro",
			"@room-id=1 :tmi.twitch.tv CLEARCHAT #outro",
			"@ban-duration=600 :tmi.twitch.tv CLEARCHAT #outro :fulano",
			"@login=fulano;target-msg-id=abc :tmi.twitch.tv CLEARMSG #outro :texto",
			"@msg-id=subs_on :tmi.twitch.tv NOTICE #outro :aviso de outra sala",
			"@msg-id=emote_only_on :tmi.twitch.tv NOTICE #gaules :aviso da sala",
			"@emote-only=1 :tmi.twitch.tv ROOMSTATE #gaules",
		} {
			_, _ = io.WriteString(conn, l+"\r\n")
		}
		<-time.After(2 * time.Second)
	}

	mgr := New(Options{Addr: srv.Addr(), Dial: srv.Dial, CoalesceInterval: 10 * time.Millisecond})
	t.Cleanup(mgr.Close)
	if _, err := mgr.JoinLogin("gaules"); err != nil {
		t.Fatalf("JoinLogin: %v", err)
	}

	var avisos []string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-mgr.Events():
			if ev.ClearChat || ev.ClearUser != "" || ev.ClearMsgID != "" {
				t.Fatalf("evento de outro canal limpou o chat: %+v", ev)
			}
			for _, m := range ev.Messages {
				avisos = append(avisos, m.Body)
			}
			if ev.RoomState == nil {
				continue
			}
			rs := *ev.RoomState
			if rs.Slow != 0 || rs.SubsOnly {
				t.Fatalf("ROOMSTATE de outro canal entrou na sessão: %+v", rs)
			}
			if !rs.EmoteOnly {
				t.Fatalf("ROOMSTATE da sala não chegou: %+v", rs)
			}
			if len(avisos) != 1 || avisos[0] != "aviso da sala" {
				t.Fatalf("avisos = %q, want só o da sala", avisos)
			}
			return
		case <-deadline:
			t.Fatal("ROOMSTATE da sala não chegou")
		}
	}
}
