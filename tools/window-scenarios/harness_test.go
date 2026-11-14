//go:build cenarios && linux

package windowscenarios

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pedrosatin/ttv-tui/internal/fixture"
	"github.com/pedrosatin/ttv-tui/internal/ui"
)

// bins são os binários compilados uma vez por execução, em TestMain.
var bins struct {
	tui, window, fakeBin string
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp(os.Getenv("TTV_CENARIOS_BUILD"), "ttv-cenarios-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cenarios: diretório dos binários:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := build(dir); err != nil {
		fmt.Fprintln(os.Stderr, "cenarios:", err)
		return 1
	}
	return m.Run()
}

// build compila o checkout: a TUI e o streamlink falso pelo módulo da raiz, a
// janela pelo módulo dela.
func build(dir string) error {
	root, err := filepath.Abs("../..")
	if err != nil {
		return fmt.Errorf("raiz do repositório: %w", err)
	}
	bins.tui = filepath.Join(dir, "ttv-tui")
	bins.window = filepath.Join(dir, "ttv-tui-window")
	bins.fakeBin = filepath.Join(dir, "fakebin")
	steps := []struct{ dir, out, pkg string }{
		{root, bins.tui, "./cmd/ttv-tui"},
		{filepath.Join(root, "window"), bins.window, "."},
		{root, filepath.Join(bins.fakeBin, "streamlink"), "./internal/fixture/streamlink"},
	}
	for _, s := range steps {
		cmd := exec.Command("go", "build", "-buildvcs=false", "-o", s.out, s.pkg)
		cmd.Dir = s.dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("compilar %s em %s: %w\n%s", s.pkg, s.dir, err, out)
		}
	}
	return nil
}

// setup descreve a janela de um cenário.
type setup struct {
	// config é o config.toml de partida; vazio, o app começa sem arquivo.
	config string
	// state é o state.json de partida (favoritos e recentes); vazio, o app
	// começa sem nenhum.
	state string
	irc   fixture.IRCConfig
}

// window é uma janela de teste em execução, com os fixtures dela.
type window struct {
	t       *testing.T
	dir     string
	sock    string
	cfgPath string
	mark    string
	cmd     *exec.Cmd
	exited  chan struct{}
	// logPath guarda stdout e stderr da janela. Um arquivo, e não um pipe: o
	// Wait não fica preso a um neto que herdou a ponta.
	logPath string
}

// openWindow sobe GQL, IRC, config isolado e a janela, e espera o catálogo. A
// limpeza roda no fim do teste, passe ou falhe.
func openWindow(t *testing.T, s setup) *window {
	t.Helper()
	// O socket Unix tem limite de 108 bytes: o diretório fica curto, fora do t.TempDir.
	dir, err := os.MkdirTemp("", "ttvc-")
	if err != nil {
		t.Fatalf("diretório do cenário: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for _, d := range []string{"home", "cfg/ttv-tui", "state", "data", "cache", "run"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatalf("diretório do cenário: %v", err)
		}
	}
	w := &window{
		t: t, dir: dir, sock: filepath.Join(dir, "t.sock"),
		cfgPath: filepath.Join(dir, "cfg", "ttv-tui", "config.toml"),
		mark:    filepath.Base(dir), exited: make(chan struct{}), logPath: filepath.Join(dir, "window.out"),
	}
	if s.config != "" {
		if err := os.WriteFile(w.cfgPath, []byte(s.config), 0o600); err != nil {
			t.Fatalf("config de partida: %v", err)
		}
	}
	if s.state != "" {
		stateDir := filepath.Join(dir, "state", "ttv-tui")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatalf("diretório do estado de partida: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(s.state), 0o600); err != nil {
			t.Fatalf("estado de partida: %v", err)
		}
	}

	gql := httptest.NewServer(fixture.GQLHandler(fixture.DefaultCatalog()))
	t.Cleanup(gql.Close)
	irc, err := fixture.NewIRCServer(s.irc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(irc.Close)

	cmd := exec.Command(bins.window, "--lang", "pt-BR")
	cmd.Env = append(cleanEnv(),
		"PATH="+bins.fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+filepath.Join(dir, "home"),
		"XDG_CONFIG_HOME="+filepath.Join(dir, "cfg"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
		"XDG_DATA_HOME="+filepath.Join(dir, "data"),
		"XDG_CACHE_HOME="+filepath.Join(dir, "cache"),
		"XDG_RUNTIME_DIR="+filepath.Join(dir, "run"),
		"TMPDIR="+dir,
		"SDL_VIDEO_DRIVER=offscreen",
		"SDL_AUDIO_DRIVER=dummy",
		"TTV_WINDOW_MUTE=1",
		"TTV_WINDOW_APPID=ttv-cenarios",
		"TTV_WINDOW_TEST="+w.sock,
		"TTV_WINDOW_TUI="+bins.tui,
		"TTV_TUI_TEST_GQL="+gql.URL+"/gql",
		"TTV_TUI_TEST_IRC="+irc.Addr(),
		"TTV_TUI_TEST_STATE=1",
		"TTV_CENARIOS_RUN="+w.mark,
	)
	logf, err := os.Create(w.logPath)
	if err != nil {
		t.Fatalf("log da janela: %v", err)
	}
	defer func() { _ = logf.Close() }()
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		t.Fatalf("abrir a janela: %v", err)
	}
	w.cmd = cmd
	go func() {
		_ = cmd.Wait()
		close(w.exited)
	}()
	// LIFO: o relatório da falha roda antes de fechar a janela.
	t.Cleanup(w.close)
	t.Cleanup(w.reportOnFailure)

	// Com favoritos ou recentes guardados, o destaque começa no primeiro deles.
	saved := s.state != ""
	w.waitState("catálogo de fixture na lista", 20*time.Second, func(st ui.StateReport) bool {
		return st.Items >= 60 && (st.Highlighted == "Categoria 01" || saved && st.Highlighted != "")
	})
	return w
}

// cleanEnv é o ambiente do teste sem nada que leve a janela ao compositor, ao
// config do operador ou a outra execução.
func cleanEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "TTV_"), strings.HasPrefix(k, "XDG_"), strings.HasPrefix(k, "SDL_"),
			strings.HasPrefix(k, "WAYLAND_"), strings.HasPrefix(k, "HYPRLAND_"),
			k == "DISPLAY", k == "HOME", k == "PATH", k == "TMPDIR":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// close pede para sair pelo mesmo caminho de fechar a janela, espera, e mata o
// que sobrou com a marca deste cenário (TUI, streamlink, shim).
func (w *window) close() {
	_, _ = w.send("quit")
	select {
	case <-w.exited:
	case <-time.After(10 * time.Second):
		w.t.Errorf("a janela não saiu 10 s depois do quit")
		_ = syscall.Kill(-w.cmd.Process.Pid, syscall.SIGKILL)
		<-w.exited
	}
	left := marked(w.mark)
	for _, pid := range left {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if len(left) > 0 {
		w.t.Errorf("processos do cenário vivos depois da saída da janela: %v", left)
	}
}

// marked lista os processos que herdaram TTV_CENARIOS_RUN=mark.
func marked(mark string) []int {
	needle := []byte("TTV_CENARIOS_RUN=" + mark)
	entries, _ := os.ReadDir("/proc")
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, kv := range bytes.Split(env, []byte{0}) {
			if bytes.Equal(kv, needle) {
				out = append(out, pid)
				break
			}
		}
	}
	return out
}

func (w *window) reportOnFailure() {
	if !w.t.Failed() {
		return
	}
	if scr, err := w.send("screen"); err == nil {
		w.t.Logf("tela no fim:\n%s", scr)
	}
	if st, err := w.send("state"); err == nil {
		w.t.Logf("state no fim: %s", st)
	}
	if out, err := os.ReadFile(w.logPath); err == nil {
		w.t.Logf("saída da janela:\n%s", out)
	}
}

// send manda um comando ao socket de teste e devolve a resposta inteira.
func (w *window) send(args ...string) (string, error) {
	c, err := net.DialTimeout("unix", w.sock, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("socket de teste: %w", err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := io.WriteString(c, strings.Join(args, " ")+"\n"); err != nil {
		return "", fmt.Errorf("escrever no socket de teste: %w", err)
	}
	_ = c.(*net.UnixConn).CloseWrite()
	b, err := io.ReadAll(c)
	if err != nil {
		return "", fmt.Errorf("ler do socket de teste: %w", err)
	}
	return string(b), nil
}

func (w *window) must(args ...string) string {
	w.t.Helper()
	out, err := w.send(args...)
	if err != nil {
		w.t.Fatalf("%s: %v", strings.Join(args, " "), err)
	}
	return out
}

// press manda as teclas pelo comando key, uma a uma, na notação do socket
// (Return, Escape, PageDown, ctrl+o, alt+Up, ctrl+Backspace, f), e espera a TUI
// contar todas: o estado lido depois já é o de depois das teclas.
func (w *window) press(keys ...string) {
	w.t.Helper()
	before := w.waitState("o estado antes das teclas", 5*time.Second, func(ui.StateReport) bool { return true }).TUI.Keys
	for _, k := range keys {
		if out := strings.TrimSpace(w.must("key", k)); out != "ok" {
			w.t.Fatalf("key %s: resposta %q", k, out)
		}
	}
	want := before + len(keys)
	w.waitState(fmt.Sprintf("a TUI receber %s", strings.Join(keys, " ")), 5*time.Second, func(s ui.StateReport) bool {
		return s.Keys >= want
	})
}

// windowState é a resposta do comando state.
type windowState struct {
	Window struct {
		Cols, Rows, W, H, CellW, CellH int
		Hole                           []int
		HoleOn                         bool
		Overlay, OverlayOpacity        int
	} `json:"window"`
	TUI *ui.StateReport `json:"tui"`
}

func (w *window) state() (windowState, error) {
	var st windowState
	out, err := w.send("state")
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return st, fmt.Errorf("state ilegível %q: %w", out, err)
	}
	if st.TUI == nil {
		return st, errors.New("a TUI ainda não mandou o estado")
	}
	return st, nil
}

// waitState espera o estado da TUI satisfazer ok. Esgotado o tempo, reprova.
func (w *window) waitState(what string, timeout time.Duration, ok func(ui.StateReport) bool) windowState {
	w.t.Helper()
	var last windowState
	err := eventually(timeout, func() error {
		st, err := w.state()
		if err != nil {
			return err
		}
		last = st
		if !ok(*st.TUI) {
			return fmt.Errorf("estado %+v", *st.TUI)
		}
		return nil
	})
	if err != nil {
		w.t.Fatalf("esperava %s: %v", what, err)
	}
	return last
}

// waitScreen espera text aparecer na grade. Esgotado o tempo, reprova.
func (w *window) waitScreen(text string, timeout time.Duration) {
	w.t.Helper()
	err := eventually(timeout, func() error {
		if rowWith(w.screen(), text) < 0 {
			return fmt.Errorf("%q não está na tela", text)
		}
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
}

func (w *window) screen() []string {
	w.t.Helper()
	return strings.Split(strings.TrimRight(w.must("screen"), "\n"), "\n")
}

// fbpixel lê a cor que a GPU desenhou no pixel (x, y) da janela.
func (w *window) fbpixel(x, y int) (r, g, b int, err error) {
	out, err := w.send("fbpixel", strconv.Itoa(x), strconv.Itoa(y))
	if err != nil {
		return 0, 0, 0, err
	}
	if n, _ := fmt.Sscan(out, &r, &g, &b); n != 3 {
		return 0, 0, 0, fmt.Errorf("fbpixel respondeu %q", strings.TrimSpace(out))
	}
	return r, g, b, nil
}

// resizeCols muda a janela para a grade ter cols colunas, com a mesma altura.
func (w *window) resizeCols(cols int) {
	w.t.Helper()
	st, err := w.state()
	if err != nil {
		w.t.Fatal(err)
	}
	px := cols*st.Window.CellW + st.Window.CellW/2
	out := strings.TrimSpace(w.must("resize", strconv.Itoa(px), strconv.Itoa(st.Window.H)))
	var gotCols, gotRows int
	if n, _ := fmt.Sscan(out, &gotCols, &gotRows); n != 2 || gotCols != cols {
		w.t.Fatalf("resize para %d colunas respondeu %q", cols, out)
	}
	w.waitState(fmt.Sprintf("a TUI com %d colunas", cols), 5*time.Second, func(s ui.StateReport) bool { return s.Width == cols })
}

// savedConfig é o config.toml que o app gravou.
func (w *window) savedConfig() string {
	b, err := os.ReadFile(w.cfgPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// openFirstChannel abre o primeiro canal da primeira categoria e espera o vídeo.
func (w *window) openFirstChannel() windowState {
	w.t.Helper()
	w.press("Return")
	w.waitState("os canais da Categoria 01", 10*time.Second, func(s ui.StateReport) bool {
		return s.Mode == string(ui.ModeChannels) && s.Highlighted == "FixtureUm"
	})
	w.press("Return")
	return w.waitState("o player tocando FixtureUm", 15*time.Second, func(s ui.StateReport) bool {
		return s.Player == string(ui.PlayerStatePlaying) && s.ChatChannel == "fixture_um"
	})
}

// eventually repete check até ele passar ou o tempo acabar, e devolve o último erro.
func eventually(timeout time.Duration, check func() error) error {
	end := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil || time.Now().After(end) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Prazos da verificação final. Um bug conhecido que sumiu só é dado por
// corrigido no fim do prazo dele, que por isso é curto.
const (
	passTimeout     = 10 * time.Second
	knownBugTimeout = 3 * time.Second
)

// expect é a verificação final de um cenário que tem de passar: check repete
// até passar ou o prazo acabar.
func expect(t *testing.T, check func() error) {
	t.Helper()
	if err := eventually(passTimeout, check); err != nil {
		t.Fatal(err)
	}
}

// expectKnownBug é a verificação final de um cenário que descreve o
// comportamento certo de um defeito que a main ainda tem (bug é o ID do achado,
// como "W1"). check devolve se o defeito está na tela; um erro dele é falha de
// infraestrutura e reprova. Com o defeito presente o teste pula, com o que why
// descreve. Sem o defeito durante todo o prazo, o teste reprova pedindo que a
// marca saia: a correção chegou e o cenário vira guarda, com expect.
func expectKnownBug(t *testing.T, bug string, check func() (bugPresent bool, err error), why func() string) {
	t.Helper()
	switch v, err := judgeKnownBug(knownBugTimeout, 100*time.Millisecond, check); v {
	case bugBroken:
		t.Fatalf("o cenário do bug conhecido %s não chegou a verificar o defeito: %v", bug, err)
	case bugStill:
		t.Skipf("bug conhecido %s: %s", bug, why())
	default:
		t.Fatalf("o bug conhecido %s não aparece mais: troque expectKnownBug por expect para o cenário virar guarda", bug)
	}
}
