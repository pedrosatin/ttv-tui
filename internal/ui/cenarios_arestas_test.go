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
