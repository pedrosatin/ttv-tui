package app

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/config"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// Sem as variáveis de teste, o app fala com a Twitch de sempre.
func TestGanchosDeTesteDesligadosSemVariavel(t *testing.T) {
	t.Setenv(testGQLEnv, "")
	t.Setenv(testIRCEnv, "")
	t.Setenv(testStateEnv, "")
	if got := testGQLEndpoint(); got != "" {
		t.Errorf("testGQLEndpoint() = %q sem a variável", got)
	}
	opts := testIRCOptions(chat.Options{Token: "x"})
	if opts.Addr != "" || opts.Dial != nil {
		t.Errorf("testIRCOptions mudou as opções sem a variável: %+v", opts)
	}
	if testStateReport() {
		t.Error("testStateReport() ligado sem a variável")
	}
}

// Uma variável de teste com endereço fora do loopback não desvia o app.
func TestGanchosDeTesteSoAceitamLoopback(t *testing.T) {
	for _, raw := range []string{"https://127.0.0.1:1/gql", "http://exemplo.com/gql", "http://10.0.0.1/gql", "::", "http://localhost:1/gql"} {
		t.Setenv(testGQLEnv, raw)
		if got := testGQLEndpoint(); got != "" {
			t.Errorf("testGQLEndpoint() aceitou %q", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:4567/gql", "http://127.8.9.10:1/gql", "http://[::1]:2/gql"} {
		t.Setenv(testGQLEnv, raw)
		if got := testGQLEndpoint(); got != raw {
			t.Errorf("testGQLEndpoint() = %q, quer %q", got, raw)
		}
	}
	for _, raw := range []string{"irc.chat.twitch.tv:6697", "10.0.0.1:6667", "sem-porta", "localhost:6667"} {
		t.Setenv(testIRCEnv, raw)
		if opts := testIRCOptions(chat.Options{}); opts.Addr != "" || opts.Dial != nil {
			t.Errorf("testIRCOptions aceitou %q", raw)
		}
	}
	t.Setenv(testIRCEnv, "127.0.0.1:6667")
	if opts := testIRCOptions(chat.Options{}); opts.Addr != "127.0.0.1:6667" || opts.Dial == nil {
		t.Errorf("testIRCOptions não usou o IRC de teste: %+v", opts)
	}
}

// Com token no config o gancho do IRC não vale: o handshake mandaria o token em
// texto puro para quem escuta no loopback. Sem token, vale.
func TestGanchoDoIRCIgnoradoComToken(t *testing.T) {
	t.Setenv(testIRCEnv, "127.0.0.1:6667")
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := os.WriteFile(path, []byte("access_token = \"tokendeteste123\"\nusername = \"espectador\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil || cfg.AccessToken == "" {
		t.Fatalf("config com token: %+v, %v", cfg.Username, err)
	}
	if opts := chatOptions(cfg); opts.Addr != "" || opts.Dial != nil {
		t.Fatalf("com token o chat foi para o IRC de teste %q", opts.Addr)
	}

	if err := os.WriteFile(path, []byte("username = \"espectador\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.LoadFrom(path)
	if err != nil || cfg.AccessToken != "" {
		t.Fatalf("config sem token: %v", err)
	}
	if opts := chatOptions(cfg); opts.Addr != "127.0.0.1:6667" || opts.Dial == nil {
		t.Fatalf("sem token o chat não usou o IRC de teste: %q", opts.Addr)
	}
}

// O resumo do estado vai pelo socket do painel numa linha própria, com a chave
// testState, e o painel continua indo como antes.
func TestVideoHostMandaEstadoEPainelSeparados(t *testing.T) {
	ln, err := net.Listen("unix", t.TempDir()+"/l.sock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	host, err := dialVideoHost(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)

	host.SetPanel(ui.HostPanel{Row: 1, Col: 2, Rows: 3, Cols: 4, Visible: true})
	line, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var panel map[string]any
	if err := json.Unmarshal(line, &panel); err != nil || panel["Cols"] != 4.0 || panel["testState"] != nil {
		t.Fatalf("painel = %s (%v)", line, err)
	}

	host.SetState(ui.StateReport{Mode: "channels", Keys: 3})
	line, err = r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var msg struct {
		TestState *ui.StateReport `json:"testState"`
	}
	if err := json.Unmarshal(line, &msg); err != nil || msg.TestState == nil || msg.TestState.Mode != "channels" || msg.TestState.Keys != 3 {
		t.Fatalf("estado = %s (%v)", line, err)
	}
}
