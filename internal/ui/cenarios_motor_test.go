package ui

// Motor dos cenários de UX: uma sequência de teclas aplicada ao modelo e a tela
// que o usuário vê no fim, comparada com um golden. O guia está em
// docs/cenarios-de-ux.md.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/chat"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

var updateGoldens = flag.Bool("update", false, "regrava os goldens dos cenários de UX em testdata/")

// scenarioSizes são os terminais em que todo cenário é gravado: o mínimo que o
// desenho suporta e um terminal comum de monitor grande.
var scenarioSizes = []struct{ width, height int }{{80, 24}, {120, 40}}

// cmdDeadline só existe para um comando que bloqueie não travar a bateria: o
// cenário falha com o nome do problema em vez de esperar o timeout do go test.
// Nenhum comportamento depende dele.
const cmdDeadline = 10 * time.Second

// maxScenarioMsgs limita as mensagens de uma tecla, contra um laço de comandos.
const maxScenarioMsgs = 1000

// timerFiredMsg é o que um temporizador do modelo devolve num cenário: o motor o
// guarda em vez de esperar o relógio. O cenário vê a tela antes de o aviso
// expirar, como o usuário logo depois da tecla.
type timerFiredMsg struct {
	after time.Duration
	fire  func(time.Time) tea.Msg
}

// scenarioTimer substitui tea.Tick: devolve na hora, sem esperar, a marca do
// temporizador.
func scenarioTimer(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	return func() tea.Msg { return timerFiredMsg{after: d, fire: fn} }
}

// parseKeys converte a notação de um cenário nas teclas que o terminal
// entregaria. Os itens se separam por espaço e cada um é:
//   - um nome de tecla do bubbletea (`enter`, `ctrl+f`, `alt+up`, `pgdown`,
//     `space`, `shift+tab`) ou um caractere só (`g`, `/`, `?`);
//   - texto solto, digitado letra a letra (`gaules`);
//   - texto entre aspas, digitado mesmo quando é nome de tecla (`"enter"`).
func parseKeys(seq string) ([]tea.KeyMsg, error) {
	var keys []tea.KeyMsg
	for _, tok := range strings.Fields(seq) {
		if len(tok) >= 2 && strings.HasPrefix(tok, `"`) && strings.HasSuffix(tok, `"`) {
			keys = append(keys, runes(tok[1:len(tok)-1])...)
			continue
		}
		if k, ok := parseKeyName(tok); ok {
			keys = append(keys, k)
			continue
		}
		if strings.Contains(tok, "+") {
			// "ctrl+ç" ou "alt+" não são texto: o autor errou o nome da tecla.
			return nil, fmt.Errorf("tecla desconhecida: %q", tok)
		}
		keys = append(keys, runes(tok)...)
	}
	return keys, nil
}

// play aplica a sequência de teclas ao modelo, uma de cada vez, e resolve os
// comandos que cada uma devolve antes da próxima, como o terminal faria com um
// usuário que espera a tela responder.
func play(t *testing.T, m Model, seq string) Model {
	t.Helper()
	keys, err := parseKeys(seq)
	if err != nil {
		t.Fatalf("play(%q): %v", seq, err)
	}
	m.timer = scenarioTimer
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = pressKey(m, k)
		m = settle(t, m, cmd)
	}
	return m
}

// feed entrega ao modelo uma mensagem de fora, como as do chat ou do player,
// e resolve os comandos dela.
func feed(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m.timer = scenarioTimer
	m, cmd := pressKeyMsg(m, msg)
	return settle(t, m, cmd)
}

// settle roda cmd e o que ele gerar até não sobrar comando. Lotes (tea.Batch e
// tea.Sequence) rodam em ordem, um item depois do outro, para que a tela final
// não dependa do agendador. tea.Quit encerra; um temporizador fica sem
// disparar.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > maxScenarioMsgs {
			t.Fatalf("mais de %d mensagens numa tecla: um comando gera outro sem parar", maxScenarioMsgs)
		}
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg := runCmd(t, next)
		if cmds, ok := batchedCmds(msg); ok {
			queue = append(queue, cmds...)
			continue
		}
		switch msg.(type) {
		case nil, timerFiredMsg:
			continue
		case tea.QuitMsg:
			return m
		}
		var more tea.Cmd
		m, more = pressKeyMsg(m, msg)
		queue = append(queue, more)
	}
	return m
}

// runCmd roda um comando e falha o teste se ele não voltar: num cenário, todo
// comando responde com fixture, sem rede nem relógio.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(cmdDeadline):
		t.Fatalf("um comando não voltou em %s: falta fixture ou temporizador fora de m.after", cmdDeadline)
		return nil
	}
}

// cmdSliceType é o tipo dos lotes do bubbletea: tea.BatchMsg e o sequenceMsg
// interno de tea.Sequence são ambos []tea.Cmd.
var cmdSliceType = reflect.TypeOf([]tea.Cmd(nil))

func batchedCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	if msg == nil {
		return nil, false
	}
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || !v.Type().ConvertibleTo(cmdSliceType) {
		return nil, false
	}
	return v.Convert(cmdSliceType).Interface().([]tea.Cmd), true
}

// screen é a tela que o usuário vê, sem ANSI e sem os espaços do fim das
// linhas, que o terminal não mostra.
func screen(m Model) string {
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}
	return strings.Join(rows, "\n") + "\n"
}

// assertGolden compara a tela com testdata/cenarios/<name>.golden. Com -update,
// regrava o arquivo em vez de comparar.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "cenarios", name+".golden")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("criar %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("gravar %s: %v", path, err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler %s: %v (rode `go test ./internal/ui -run %s -update` para gravar)", path, err, t.Name())
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if want == got {
		return
	}
	t.Errorf("a tela mudou em relação a %s\n%s\nSe a mudança é de propósito, rode `go test ./internal/ui -run '%s' -update` e confira o diff do golden.",
		path, screenDiff(want, got), t.Name())
}

// screenDiff mostra só as linhas que diferem, numeradas, com a tela esperada e a
// obtida lado a lado no texto.
func screenDiff(want, got string) string {
	w := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	g := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	var b strings.Builder
	for i := 0; i < max(len(w), len(g)); i++ {
		var wl, gl string
		hasW, hasG := i < len(w), i < len(g)
		if hasW {
			wl = w[i]
		}
		if hasG {
			gl = g[i]
		}
		if hasW && hasG && wl == gl {
			continue
		}
		fmt.Fprintf(&b, "linha %2d\n", i+1)
		if hasW {
			fmt.Fprintf(&b, "  golden: |%s|\n", wl)
		} else {
			b.WriteString("  golden: (sem a linha)\n")
		}
		if hasG {
			fmt.Fprintf(&b, "  tela:   |%s|\n", gl)
		} else {
			b.WriteString("  tela:   (sem a linha)\n")
		}
	}
	return b.String()
}

// scenarioWorld é o catálogo, o player e o chat de fixture dos cenários. Os
// números e títulos são fixos, para que a tela não mude de uma rodada a outra.
type scenarioWorld struct {
	playback *fakePlayback
	chat     *fakeChat
	searched []string
}

var (
	scenarioTitleGaules = "MAJOR DE CS2 — Final ao vivo"
	scenarioTitleAlan   = "Zerando o jogo novo"
	scenarioTitleTV     = "Reprise da semifinal"
	scenarioLangPT      = "pt"
)

func scenarioCategories() []catalog.Category {
	return []catalog.Category{
		{ID: "cat-jc", Name: "Just Chatting", Viewers: catalog.IntPtr(410000)},
		{ID: "cat-cs", Name: "Counter-Strike", Viewers: catalog.IntPtr(250000)},
		{ID: "cat-val", Name: "VALORANT", Viewers: catalog.IntPtr(120000)},
	}
}

func scenarioChannels() []catalog.Channel {
	return []catalog.Channel{
		{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules", Language: &scenarioLangPT, Viewers: catalog.IntPtr(98500), Title: &scenarioTitleGaules},
		{ID: "ch-alan", Login: "alanzoka", DisplayName: "alanzoka", Language: &scenarioLangPT, Viewers: catalog.IntPtr(42000), Title: &scenarioTitleAlan},
	}
}

func scenarioSearchHits() []catalog.Channel {
	return []catalog.Channel{
		{ID: "ch-gaules", Login: "gaules", DisplayName: "Gaules", Language: &scenarioLangPT, Viewers: catalog.IntPtr(98500), Title: &scenarioTitleGaules},
		{ID: "ch-gaulestv", Login: "gaulestv", DisplayName: "GaulesTV", Language: &scenarioLangPT, Viewers: catalog.IntPtr(1200), Title: &scenarioTitleTV},
	}
}

// newScenario monta o modelo do app com o catálogo de fixture, em pt-BR, no
// tamanho de terminal pedido, já com as categorias carregadas.
func newScenario(t *testing.T, width, height int, opts ...Option) (Model, *scenarioWorld) {
	t.Helper()
	w := &scenarioWorld{
		playback: &fakePlayback{},
		chat:     &fakeChat{hasToken: true, username: "espectador"},
	}
	src := &fakeSource{
		categoriesFn: func(ctx context.Context, page catalog.PageRequest) (catalog.Page[catalog.Category], error) {
			return catalog.Page[catalog.Category]{Items: scenarioCategories()}, nil
		},
		channelsFn: func(ctx context.Context, categoryID string, page catalog.PageRequest) (catalog.Page[catalog.Channel], error) {
			if categoryID == "cat-zelda" {
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
					{ID: "ch-link", Login: "hero_of_time", DisplayName: "HeroOfTime", Language: &scenarioLangPT, Viewers: catalog.IntPtr(1500), Availability: catalog.AvailabilityLive},
				}}, nil
			}
			return catalog.Page[catalog.Channel]{Items: scenarioChannels()}, nil
		},
		searchCategoriesFn: func(ctx context.Context, query string) ([]catalog.Category, error) {
			if strings.EqualFold(query, "zelda") {
				return []catalog.Category{
					{ID: "cat-zelda", Name: "The Legend of Zelda: Tears of the Kingdom", Viewers: catalog.IntPtr(85000)},
				}, nil
			}
			return nil, nil
		},
		searchFn: func(ctx context.Context, query string) (catalog.Page[catalog.Channel], error) {
			w.searched = append(w.searched, query)
			if strings.EqualFold(query, "zelda") {
				return catalog.Page[catalog.Channel]{Items: []catalog.Channel{
					{ID: "ch-zelda-fan", Login: "zeldafan", DisplayName: "ZeldaFan", Language: &scenarioLangPT, Viewers: catalog.IntPtr(300), Availability: catalog.AvailabilityLive},
				}}, nil
			}
			return catalog.Page[catalog.Channel]{Items: scenarioSearchHits()}, nil
		},
		channelsByIDsFn: func(ctx context.Context, ids []string) ([]catalog.Channel, error) {
			known := append(scenarioChannels(), scenarioSearchHits()...)
			var out []catalog.Channel
			for _, id := range ids {
				for _, ch := range known {
					if ch.ID == id || ch.Login == id {
						live := ch
						live.Availability = catalog.AvailabilityLive
						out = append(out, live)
						break
					}
				}
			}
			return out, nil
		},
	}
	all := append([]Option{WithSource(src), WithPlayback(w.playback), WithChat(w.chat)}, opts...)
	m := New(mustCatalog(t, i18n.LangPTBR), all...)
	m.timer = scenarioTimer
	m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	m = settle(t, m, m.Init())
	return m, w
}

// chatLines entrega mensagens de chat de fixture ao canal aberto, com a sala já
// conectada.
func chatLines(t *testing.T, m Model, lines ...[2]string) Model {
	t.Helper()
	gen := m.chat.panel.gen
	channel := m.chat.panel.channel
	m = feed(t, m, ChatEventMsg{Generation: gen, Channel: channel, State: ChatStateConnected})
	msgs := make([]ChatMessage, 0, len(lines))
	for i, l := range lines {
		msgs = append(msgs, ChatMessage{
			Generation: gen, Kind: chat.MessageChat, ID: fmt.Sprintf("m%d", i),
			User: strings.ToLower(l[0]), DisplayName: l[0], Body: l[1],
		})
	}
	return feed(t, m, ChatEventMsg{Generation: gen, Channel: channel, Messages: msgs})
}
