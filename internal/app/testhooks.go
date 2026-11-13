package app

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/config"
)

// Ganchos dos cenários da janela real (tools/window-scenarios). Fora deles as
// variáveis não existem e o app fala com a Twitch como sempre. Cada uma só vale
// com um endereço de loopback: uma variável esquecida no ambiente não desvia o
// app para outra máquina.
const (
	// testGQLEnv troca o endpoint do GQL da Twitch, por exemplo
	// http://127.0.0.1:4567/gql.
	testGQLEnv = "TTV_TUI_TEST_GQL"
	// testIRCEnv troca o IRC da Twitch por host:porta em TCP sem TLS.
	testIRCEnv = "TTV_TUI_TEST_IRC"
	// testStateEnv = "1" faz a TUI contar o próprio estado à janela que a hospeda.
	testStateEnv = "TTV_TUI_TEST_STATE"
)

// testGQLEndpoint devolve o endpoint de teste, ou "" sem a variável ou com um
// endereço que não seja http de loopback.
func testGQLEndpoint() string {
	raw := os.Getenv(testGQLEnv)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
		return ""
	}
	return raw
}

// testIRCOptions aponta o chat para o IRC de teste, sem TLS. Sem a variável,
// ou com um endereço fora do loopback, devolve as opções sem mudança.
//
// Com token no config o gancho também não vale, e o chat segue para o IRC da
// Twitch. O handshake manda o token em PASS oauth:, em texto puro neste dial
// sem TLS: quem controla o ambiente de um binário de produção poria um servidor
// próprio em 127.0.0.1 e leria a credencial. Os cenários rodam sem login.
// O GQL não precisa da mesma regra: o cliente manda só o Client-Id público.
func testIRCOptions(opts chat.Options) chat.Options {
	addr := os.Getenv(testIRCEnv)
	if addr == "" || config.NormalizeToken(opts.Token) != "" {
		return opts
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !isLoopbackHost(host) {
		return opts
	}
	opts.Addr = addr
	opts.Dial = func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("chat: dial de teste %s: %w", addr, err)
		}
		return conn, nil
	}
	return opts
}

// testStateReport diz se a TUI deve contar o estado à janela.
func testStateReport() bool {
	return os.Getenv(testStateEnv) == "1"
}

// isLoopbackHost aceita só IP literal de loopback (127.0.0.0/8 e ::1). Um nome,
// mesmo localhost, depende do resolvedor e do /etc/hosts, que o ambiente controla.
func isLoopbackHost(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
