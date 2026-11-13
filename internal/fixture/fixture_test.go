package fixture

import (
	"bytes"
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/twitch"
)

// O GQL de fixture passa pelo cliente de verdade: um campo que o cliente passe a
// exigir reprova aqui, e não no meio de um cenário da janela.
func TestGQLDeFixtureAtendeOClienteDaTwitch(t *testing.T) {
	srv := NewGQLServer(DefaultCatalog())
	defer srv.Close()
	src := twitch.NewGQLSource(twitch.GQLConfig{Endpoint: srv.URL + "/gql"})
	ctx := context.Background()

	cats, err := src.Categories(ctx, catalog.PageRequest{Limit: 100})
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats.Items) != 60 || cats.Items[0].Name != "Categoria 01" {
		t.Fatalf("Categories = %d itens, primeiro %+v", len(cats.Items), cats.Items[0])
	}
	chans, err := src.Channels(ctx, cats.Items[0].ID, catalog.PageRequest{Limit: 50})
	if err != nil {
		t.Fatalf("Channels: %v", err)
	}
	logins := make([]string, 0, len(chans.Items))
	for _, ch := range chans.Items {
		logins = append(logins, ch.Login)
	}
	if !slices.Equal(logins, []string{"fixture_um", "fixture_dois", "fixture_tres"}) {
		t.Fatalf("Channels = %v", logins)
	}
	found, err := src.Search(ctx, "fixture_d")
	if err != nil || len(found.Items) != 1 || found.Items[0].Login != "fixture_dois" {
		t.Fatalf("Search = %+v, %v", found.Items, err)
	}
	games, err := src.SearchCategories(ctx, "categoria 0")
	if err != nil || len(games) != 9 {
		t.Fatalf("SearchCategories = %d, %v", len(games), err)
	}
	users, err := src.ChannelsByLogins(ctx, []string{"fixture_tres", "ninguem"})
	if err != nil || len(users) != 1 || users[0].Login != "fixture_tres" {
		t.Fatalf("ChannelsByLogins = %+v, %v", users, err)
	}
}

// O IRC de fixture passa pelo cliente de verdade, em TCP sem TLS, e entrega o
// volume pedido.
func TestIRCDeFixtureEntregaAsMensagens(t *testing.T) {
	srv, err := NewIRCServer(IRCConfig{Messages: 3, Every: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	mgr := chat.New(chat.Options{
		Addr: srv.Addr(),
		Dial: func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
	})
	defer mgr.Close()
	if _, err := mgr.JoinLogin("fixture_um"); err != nil {
		t.Fatal(err)
	}
	var got []string
	deadline := time.After(5 * time.Second)
	for len(got) < 5 {
		select {
		case ev := <-mgr.Events():
			for _, msg := range ev.Messages {
				got = append(got, msg.User+": "+msg.Body)
			}
		case <-deadline:
			t.Fatalf("só chegaram %v", got)
		}
	}
	for n := 1; n <= 5; n++ {
		if want := MessageUser(n) + ": " + MessageText(n); got[n-1] != want {
			t.Fatalf("mensagem %d = %q, quer %q", n, got[n-1], want)
		}
	}
}

func TestSplitArgsComoOShlex(t *testing.T) {
	cases := map[string][]string{
		"--no-config --hwdec=auto-safe":          {"--no-config", "--hwdec=auto-safe"},
		`'--input-ipc-server=/tmp/a b/mpv.sock'`: {"--input-ipc-server=/tmp/a b/mpv.sock"},
		`'it'"'"'s' x`:                           {"it's", "x"},
		`'\\.\pipe\ttv'`:                         {`\\.\pipe\ttv`},
		`a\ b "c \"d\""`:                         {"a b", `c "d"`},
		"":                                       nil,
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("SplitArgs(%q) = %q, %v; quer %q", in, got, err, want)
		}
	}
	if _, err := SplitArgs("'aberta"); err == nil {
		t.Error("aspas sem fim deveriam dar erro")
	}
}

func TestClipeTemCabecalhoEQuadrosInteiros(t *testing.T) {
	var b bytes.Buffer
	if err := WriteClip(&b, 3); err != nil {
		t.Fatal(err)
	}
	header, body, ok := strings.Cut(b.String(), "\n")
	if !ok || !strings.HasPrefix(header, "YUV4MPEG2 W160 H90 F30:1") {
		t.Fatalf("cabeçalho %q", header)
	}
	frame := len("FRAME\n") + ClipWidth*ClipHeight*3/2
	if len(body) != 3*frame || strings.Count(body, "FRAME\n") != 3 {
		t.Fatalf("corpo de %d bytes, quer 3 quadros de %d", len(body), frame)
	}
}
