package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/chat"
)

// mensagemPaginada é a mensagem i do percurso: a%02d abre o texto e z%02d o
// fecha, para o teste saber se ela apareceu inteira. Uma em três ocupa uma
// linha; as outras quebram em várias, com texto comprido ou com emoji.
func mensagemPaginada(gen uint64, i int) ChatMessage {
	var meio string
	switch i % 3 {
	case 1:
		meio = " " + strings.TrimSpace(strings.Repeat("palavra comprida ", 4))
	case 2:
		meio = " " + strings.TrimSpace(strings.Repeat("🎉 ", 10))
	}
	return ChatMessage{
		Generation: gen, Kind: chat.MessageChat, ID: fmt.Sprintf("p%d", i),
		User: "fulano", DisplayName: "Fulano", Body: fmt.Sprintf("a%02d%s z%02d", i, meio, i),
	}
}

// chegam entrega as mensagens de first até first+n-1 ao chat aberto.
func chegam(t *testing.T, m Model, first, n int) Model {
	t.Helper()
	msgs := make([]ChatMessage, 0, n)
	for i := first; i < first+n; i++ {
		msgs = append(msgs, mensagemPaginada(m.chat.panel.gen, i))
	}
	return feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, Messages: msgs})
}

// cabecalhoQuebrado põe o chat em reconexão com um motivo comprido: o
// cabeçalho passa a ocupar duas linhas.
func cabecalhoQuebrado(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateReconnecting, ErrText: "conexão perdida com o servidor"})
}

// mensagensVistas devolve os números das mensagens que aparecem na tela. Conta
// a mensagem inteira (a%02d e z%02d na tela) ou, se ela é mais alta que a
// página, o fim dela.
func mensagensVistas(m Model, alturas map[int]int) map[int]bool {
	tela := screen(m)
	vistas := map[int]bool{}
	for _, z := range regexp.MustCompile(`\bz(\d\d)\b`).FindAllStringSubmatch(tela, -1) {
		i, err := strconv.Atoi(z[1])
		if err != nil {
			continue
		}
		if strings.Contains(tela, fmt.Sprintf("a%02d", i)) || alturas[i] > m.chatPageSize() {
			vistas[i] = true
		}
	}
	return vistas
}

// alturasPaginadas mede quantas linhas cada mensagem ocupa no painel.
func alturasPaginadas(m Model, n int) map[int]int {
	_, chatW := m.panelWidths()
	alturas := map[int]int{}
	for i := range n {
		lead, content := m.formatChatMessageParts(mensagemPaginada(m.chat.panel.gen, i), false)
		alturas[i] = len(wrapChatRow(lead, content, chatW))
	}
	return alturas
}

// faltando lista as mensagens de 0 a n-1 que nunca apareceram.
func faltando(vistas map[int]bool, n int) []int {
	var out []int
	for i := range n {
		if !vistas[i] {
			out = append(out, i)
		}
	}
	return out
}

// percorre aperta tecla até a página parar de mudar e devolve as mensagens que
// passaram pela tela.
func percorre(t *testing.T, m Model, tecla string, n int) (Model, map[int]bool) {
	t.Helper()
	alturas := alturasPaginadas(m, n)
	vistas := mensagensVistas(m, alturas)
	for range 4 * n {
		antes := screen(m)
		m = play(t, m, tecla)
		for i := range mensagensVistas(m, alturas) {
			vistas[i] = true
		}
		if screen(m) == antes || (tecla == "pgdown" && m.ChatFollowLive()) {
			break
		}
	}
	return m, vistas
}

type variantePaginacao struct {
	nome  string
	monta func(t *testing.T, width, height int) Model
}

// variantesPaginacao abre o mesmo chat no overlay (inteiro, reduzido, mínimo,
// com cabeçalho quebrado e com dica de envio) e no painel lateral.
func variantesPaginacao(n int) []variantePaginacao {
	overlay := func(descidas int, extra func(t *testing.T, m Model) Model) func(t *testing.T, width, height int) Model {
		return func(t *testing.T, width, height int) Model {
			m, _ := hostedScenarioClassic(t, width, height)
			m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
			m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
			m = feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateConnected})
			m = chegam(t, m, 0, n)
			for range descidas {
				m = play(t, m, "alt+down")
			}
			if extra != nil {
				m = extra(t, m)
			}
			return play(t, m, "tab")
		}
	}
	return []variantePaginacao{
		{"overlay-inteiro", overlay(0, nil)},
		{"overlay-reduzido", overlay(4, nil)},
		{"overlay-minimo", overlay(40, nil)},
		{"overlay-cabecalho-quebrado", overlay(4, cabecalhoQuebrado)},
		{"overlay-minimo-cabecalho-quebrado", overlay(40, cabecalhoQuebrado)},
		{"lateral", func(t *testing.T, width, height int) Model {
			m := scrollFixture(t, "pt-BR", 0)
			m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
			return chegam(t, m, 0, n)
		}},
		{"lateral-cabecalho-quebrado", func(t *testing.T, width, height int) Model {
			m := scrollFixture(t, "pt-BR", 0)
			m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
			return cabecalhoQuebrado(t, chegam(t, m, 0, n))
		}},
	}
}

// A página do PgUp/PgDn tem o tamanho das linhas de mensagem que o painel
// desenha: o cabeçalho quebrado, a dica de envio, o aviso e o aviso de
// mensagens novas saem dela.
func TestChatPaginaTemAsLinhasDesenhadas(t *testing.T) {
	linhaDeMensagem := regexp.MustCompile(`\bmsg \d+\b`)
	desenhadas := func(m Model) int {
		return len(linhaDeMensagem.FindAllString(screen(m), -1))
	}
	estados := []struct {
		nome  string
		muda  func(t *testing.T, m Model) Model
		pausa bool
	}{
		{"ao-vivo", nil, false},
		{"cabecalho-quebrado", cabecalhoQuebrado, false},
		{"escrevendo", func(t *testing.T, m Model) Model { return play(t, m, "i") }, false},
		{"aviso-de-envio", func(t *testing.T, m Model) Model { return play(t, m, "i /ban enter") }, false},
		{"mensagens-novas", nil, true},
		{"tudo-junto", func(t *testing.T, m Model) Model { return play(t, cabecalhoQuebrado(t, m), "i /ban enter") }, true},
	}
	for _, size := range scenarioSizes {
		for _, overlay := range []int{-1, 0, 4, 40} { // -1: painel lateral
			for _, e := range estados {
				nome := fmt.Sprintf("%dx%d/overlay=%d/%s", size.width, size.height, overlay, e.nome)
				t.Run(nome, func(t *testing.T) {
					m, _ := hostedScenarioClassic(t, size.width, size.height)
					if overlay < 0 {
						m, _ = newScenarioClassic(t, size.width, size.height)
						m = play(t, m, "ctrl+f gaules enter enter")
					} else {
						m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
						for range overlay {
							m = play(t, m, "alt+down")
						}
					}
					m = feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateConnected})
					m = withMessages(t, m, 200)
					m = play(t, m, "tab")
					if e.pausa {
						m = play(t, m, "k")
						m = withMessagesAppend(t, m, 3)
					}
					if e.muda != nil {
						m = e.muda(t, m)
					}
					if got, want := m.chatPageSize(), desenhadas(m); got != want {
						t.Fatalf("página de %d linhas, o painel desenha %d:\n%s", got, want, screen(m))
					}
				})
			}
		}
	}
}

// withMessagesAppend acrescenta n mensagens "nova N" ao buffer pelo caminho
// normal de chegada.
func withMessagesAppend(t *testing.T, m Model, n int) Model {
	t.Helper()
	msgs := make([]ChatMessage, 0, n)
	for i := range n {
		msgs = append(msgs, ChatMessage{Generation: m.chat.panel.gen, Kind: chat.MessageChat, ID: fmt.Sprintf("n%d", i), User: "u", DisplayName: "User", Body: fmt.Sprintf("nova %d", i)})
	}
	return feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, Messages: msgs})
}

// PgUp do ao vivo até o topo e PgDn de Home até o ao vivo passam por todas as
// mensagens, de uma ou de várias linhas, no overlay e no painel lateral. Home
// mostra a mais antiga inteira.
func TestChatPaginasPercorremTodasAsMensagens(t *testing.T) {
	const n = 45
	for _, size := range scenarioSizes {
		for _, v := range variantesPaginacao(n) {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, v.nome), func(t *testing.T) {
				m := v.monta(t, size.width, size.height)
				if m.Focus() != FocusChat {
					t.Fatalf("foco em %q, quero o chat", m.Focus())
				}
				if tela := screen(play(t, m, "home")); !strings.Contains(tela, "a00") || !strings.Contains(tela, "z00") {
					t.Errorf("Home não mostrou a mensagem mais antiga inteira:\n%s", tela)
				}
				m, vistas := percorre(t, m, "pgup", n)
				if f := faltando(vistas, n); len(f) > 0 {
					t.Fatalf("PgUp pulou as mensagens %v; tela no fim:\n%s", f, screen(m))
				}
				m = play(t, m, "home")
				m, vistas = percorre(t, m, "pgdown", n)
				if f := faltando(vistas, n); len(f) > 0 {
					t.Fatalf("PgDn pulou as mensagens %v; tela no fim:\n%s", f, screen(m))
				}
				if !m.ChatFollowLive() {
					t.Fatalf("PgDn até o fim devia voltar ao vivo:\n%s", screen(m))
				}
			})
		}
	}
}

// Mensagens novas chegam durante a leitura do histórico: o aviso toma uma linha
// da página, e PgDn ainda passa por todas, as velhas e as novas.
func TestChatPaginasComMensagensNovasNaoPulam(t *testing.T) {
	const n, novas = 45, 5
	for _, size := range scenarioSizes {
		for _, v := range variantesPaginacao(n) {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, v.nome), func(t *testing.T) {
				m := v.monta(t, size.width, size.height)
				m = play(t, m, "home")
				// O que estava na tela antes da chegada conta como lido.
				lidas := mensagensVistas(m, alturasPaginadas(m, n))
				m = chegam(t, m, n, novas)
				if m.ChatPendingNew() != novas {
					t.Fatalf("pendentes = %d, quero %d", m.ChatPendingNew(), novas)
				}
				m, vistas := percorre(t, m, "pgdown", n+novas)
				for i := range lidas {
					vistas[i] = true
				}
				if f := faltando(vistas, n+novas); len(f) > 0 {
					t.Fatalf("PgDn pulou as mensagens %v; tela no fim:\n%s", f, screen(m))
				}
			})
		}
	}
}

// End e G voltam ao vivo com a altura e a largura de agora, depois de Alt+↑/↓ e
// de redimensionar a janela: zeram as pendentes e mostram a mais nova junto da
// entrada.
func TestChatEndVoltaAoVivoDepoisDeMudarOPainel(t *testing.T) {
	const n = 30
	for _, tecla := range []string{"end", "G"} {
		for _, size := range scenarioSizes {
			for _, v := range variantesPaginacao(n) {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", tecla, size.width, size.height, v.nome), func(t *testing.T) {
					m := v.monta(t, size.width, size.height)
					m = play(t, m, "pgup pgup")
					m = chegam(t, m, n, 3)
					if m.hosted.chatOverlay {
						m = play(t, m, "alt+up alt+down alt+down")
					}
					m = feed(t, m, tea.WindowSizeMsg{Width: size.width + 7, Height: size.height - 3})
					m = play(t, m, tecla)
					if !m.ChatFollowLive() || m.ChatPendingNew() != 0 {
						t.Fatalf("%s: ao vivo=%v pendentes=%d", tecla, m.ChatFollowLive(), m.ChatPendingNew())
					}
					tela := screen(m)
					if !strings.Contains(tela, fmt.Sprintf("z%02d", n+2)) {
						t.Fatalf("%s não mostrou a mensagem mais nova:\n%s", tecla, tela)
					}
					if strings.Contains(tela, fmt.Sprintf(m.t("chat.scroll.new"), 3)) {
						t.Fatalf("%s deixou o aviso de mensagens novas:\n%s", tecla, tela)
					}
				})
			}
		}
	}
}
