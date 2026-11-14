package ui

// Cenários do ticket ux-19, as arestas da janela que também aparecem na TUI.

import (
	"strings"
	"testing"
)

// statusLine é a barra de status, a última linha da tela.
func statusLine(m Model) string {
	rows := strings.Split(strings.TrimSuffix(screen(m), "\n"), "\n")
	return rows[len(rows)-1]
}

// O usuário abre gaules e aperta m: o player recebe o pedido de mudo e, com o
// mudo confirmado, a barra diz "mudo" no lugar do volume.
func TestCenarioTeclaMMuda(t *testing.T) {
	eachSize(t, "tecla-m-muda", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter")
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		m = play(t, m, "m")
		if w.playback.muteToggles != 1 {
			t.Fatalf("m pediu %d trocas de mudo, quer 1", w.playback.muteToggles)
		}
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, Muted: true})
		if bar := statusLine(m); !strings.Contains(bar, "mudo") || strings.Contains(bar, "vol 100") {
			t.Fatalf("barra com o player mudo: %q", bar)
		}
		return m
	})
}

// Sem player, m não pede nada: não há o que silenciar.
func TestTeclaMSemPlayer(t *testing.T) {
	m, w := newScenario(t, 80, 24)
	m = play(t, m, "m")
	if w.playback.muteToggles != 0 {
		t.Fatalf("m sem player pediu %d trocas de mudo", w.playback.muteToggles)
	}
	if strings.Contains(statusLine(m), "mudo") {
		t.Fatalf("barra sem player: %q", statusLine(m))
	}
}

// Escrevendo no chat, m é texto.
func TestTeclaMNaEscritaEhTexto(t *testing.T) {
	m, w := newScenario(t, 80, 24)
	m = play(t, m, "ctrl+f gaules enter enter")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = chatLines(t, m, [2]string{"Fulano", "oi"})
	m = play(t, m, "i m")
	if w.playback.muteToggles != 0 || m.chat.composeDraft != "m" {
		t.Fatalf("na escrita: %d trocas de mudo, rascunho %q", w.playback.muteToggles, m.chat.composeDraft)
	}
}

// hostedScenario é o cenário dentro da janela única: o vídeo fica na coluna do
// meio, entre a lista e o chat.
func hostedScenario(t *testing.T, width, height int, opts ...Option) (Model, *scenarioWorld) {
	t.Helper()
	exactCellWidths = true
	t.Cleanup(func() { exactCellWidths = false })
	return newScenario(t, width, height, append([]Option{WithVideoHost(&recordingHost{})}, opts...)...)
}

// Na janela, com uma categoria destacada na tela inicial, a área do vídeo diz
// que Enter abre a categoria; com um canal destacado, que Enter assiste a ele.
func TestCenarioJanelaTelaInicialFalaDoDestacado(t *testing.T) {
	eachSize(t, "janela-tela-inicial", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenario(t, width, height)
		if !strings.Contains(screen(m), "Enter abre a categoria") {
			t.Fatalf("tela inicial sem a dica da categoria:\n%s", screen(m))
		}
		return m
	})
	eachSize(t, "janela-canal-destacado", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenario(t, width, height)
		m = play(t, m, "j enter")
		if !strings.Contains(screen(m), "Enter assiste a Gaules") {
			t.Fatalf("canais sem a dica do canal destacado:\n%s", screen(m))
		}
		return m
	})
}

// Na janela, enquanto o player abre gaules, a área do vídeo diz "Abrindo
// gaules…"; com o vídeo tocando, ela fica livre para o quadro.
func TestCenarioJanelaAbrindoCanal(t *testing.T) {
	eachSize(t, "janela-abrindo-canal", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter")
		if m.PlayerState() != PlayerStateStarting || !strings.Contains(screen(m), "Abrindo gaules…") {
			t.Fatalf("player %s, tela:\n%s", m.PlayerState(), screen(m))
		}
		playing := feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		if strings.Contains(screen(playing), "Abrindo") {
			t.Fatalf("o aviso de abertura ficou com o vídeo tocando:\n%s", screen(playing))
		}
		return m
	})
}
