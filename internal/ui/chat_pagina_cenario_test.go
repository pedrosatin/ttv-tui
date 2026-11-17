package ui

import (
	"fmt"
	"strings"
	"testing"
)

// overlayPaginado abre gaules na janela com o chat sobreposto e o vídeo
// tocando, com n mensagens de uma e de várias linhas, baixa o overlay quatro
// passos com Alt+↓ e põe o foco no chat.
func overlayPaginado(t *testing.T, width, height, n int) Model {
	t.Helper()
	m, _ := hostedScenarioClassic(t, width, height)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateConnected})
	m = chegam(t, m, 0, n)
	return play(t, m, "alt+down alt+down alt+down alt+down tab")
}

// O chat reconecta com um motivo comprido e o cabeçalho quebra em duas linhas.
// No overlay reduzido, o espectador vai ao início com Home e desce uma página:
// a página começa na mensagem logo abaixo da última que Home mostrou.
func TestCenarioChatSobrepostoPaginaCabecalhoQuebrado(t *testing.T) {
	eachSize(t, "chat-sobreposto-pagina-cabecalho", func(t *testing.T, width, height int) Model {
		m := cabecalhoQuebrado(t, overlayPaginado(t, width, height, 45))
		_, chatW, _ := m.hostedWidths()
		if got := len(m.renderChatHeaderLines(chatW)); got != 2 {
			t.Fatalf("cabeçalho com %d linhas, quero 2", got)
		}
		m = play(t, m, "home")
		if tela := screen(m); !strings.Contains(tela, "a00") || !strings.Contains(tela, "z00") {
			t.Fatalf("Home não mostrou a mais antiga inteira:\n%s", tela)
		}
		ultima := m.chat.panel.bottomIdx
		m = play(t, m, "pgdown")
		if tela := screen(m); !strings.Contains(tela, fmt.Sprintf("a%02d", ultima+1)) {
			t.Fatalf("PgDn pulou a mensagem %d:\n%s", ultima+1, tela)
		}
		return m
	})
}

// O espectador sobe duas páginas no overlay reduzido e chegam três mensagens:
// o aviso de novas ocupa a linha acima da entrada e a mensagem de baixo da
// página continua inteira.
func TestCenarioChatSobrepostoPaginaComNovas(t *testing.T) {
	eachSize(t, "chat-sobreposto-pagina-novas", func(t *testing.T, width, height int) Model {
		const n = 45
		m := overlayPaginado(t, width, height, n)
		m = play(t, m, "pgup pgup")
		m = chegam(t, m, n, 3)
		tela := screen(m)
		if !strings.Contains(tela, fmt.Sprintf(m.t("chat.scroll.new"), 3)) {
			t.Fatalf("sem o aviso de mensagens novas:\n%s", tela)
		}
		if baixo := m.chat.panel.bottomIdx; !strings.Contains(tela, fmt.Sprintf("a%02d", baixo)) || !strings.Contains(tela, fmt.Sprintf("z%02d", baixo)) {
			t.Fatalf("a mensagem %d, de baixo da página, não está inteira:\n%s", baixo, tela)
		}
		return m
	})
}
