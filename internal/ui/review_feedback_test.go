package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Com só o chat à vista, Tab explica como recuperar a lista.
func TestCenarioTabListaOculta(t *testing.T) {
	for _, keys := range []string{"ctrl+h", "ctrl+w"} {
		t.Run(keys, func(t *testing.T) {
			eachSize(t, "tab-lista-oculta-"+keys, func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				m = play(t, m, "ctrl+f gaules enter enter "+keys+" tab")
				if !m.chatColumnVisible() || m.listVisible() || m.StatusNotice() != m.t("layout.hidden_list_key") {
					t.Fatalf("lista visível=%v chat visível=%v aviso=%q", m.listVisible(), m.chatColumnVisible(), m.StatusNotice())
				}
				return m
			})
		})
	}
}

// Num terminal estreito, Ctrl+L abre só o chat e Tab oferece recuperar a lista.
func TestCenarioTabListaOcultaEstreito(t *testing.T) {
	m, _ := newScenario(t, 60, 24)
	m = play(t, m, "ctrl+f gaules enter enter ctrl+l tab")
	if !m.chatColumnVisible() || m.listVisible() || m.StatusNotice() != m.t("layout.hidden_list_key") {
		t.Fatal(m.StatusNotice())
	}
	assertGolden(t, "tab-lista-oculta-estreito-60x24", screen(m))
}

// Ao tentar escrever com o chat oculto, recebo a tecla para mostrá-lo.
func TestCenarioEscreverChatOculto(t *testing.T) {
	eachSize(t, "escrever-chat-oculto", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+l i")
		if m.StatusNotice() != m.t("layout.hidden_chat_key") || m.chat.composing {
			t.Fatal(m.StatusNotice())
		}
		return m
	})
}

// No modo foco, o rodapé me dá os controles que restauram os painéis.
func TestCenarioRodapeFoco(t *testing.T) {
	eachSize(t, "rodape-foco", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+w ctrl+w ctrl+w")
		for _, key := range []string{"Ctrl+W", "Ctrl+H", "Ctrl+L", "? ajuda"} {
			if !strings.Contains(m.Footer(), key) {
				t.Fatalf("rodapé sem %s: %q", key, m.Footer())
			}
		}
		if strings.Contains(m.Footer(), "abrir") {
			t.Fatal(m.Footer())
		}
		return m
	})
}

func TestComposeUnsupportedFeedback(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(fmt.Sprint(unsupported), func(t *testing.T) {
			m, _ := newScenario(t, 80, 24)
			m = play(t, m, "ctrl+f gaules enter enter")
			m.chat.panel.unsupported = unsupported
			if !unsupported {
				m.chat.session = nil
			}
			m = play(t, m, "i")
			if m.StatusNotice() != m.t("chat.unsupported") || m.chat.composing {
				t.Fatal(m.StatusNotice())
			}
		})
	}
}

// Fixar ou desafixar confirma a ação mesmo quando o player já tem um erro.
func TestCenarioCategoriaSobreErro(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(fmt.Sprint(persisted), func(t *testing.T) {
			eachSize(t, fmt.Sprintf("categoria-sobre-erro-%v", persisted), func(t *testing.T, w, h int) Model {
				m, _ := newScenario(t, w, h)
				if persisted {
					m.store = &feedbackStore{}
				}
				m.player.state, m.player.failure = PlayerStateError, PlayerFailureNetwork
				name := m.VisibleItems()[m.list.highlightIndex].Category.Name
				for _, key := range []string{"categories.pinned", "categories.unpinned"} {
					m = play(t, m, "f")
					if !strings.Contains(ansi.Strip(m.StatusBar()), fmt.Sprintf(m.t(key), name)) {
						t.Fatal(m.StatusBar())
					}
				}
				return m
			})
		})
	}
}

// Fora da janela única, os controles de janela explicam a limitação.
func TestCenarioControlesSemJanela(t *testing.T) {
	for _, key := range []string{"ctrl+o", "ctrl+p", "alt+up", "alt+down", "ctrl+left", "ctrl+right", "alt+left", "alt+right"} {
		t.Run(key, func(t *testing.T) {
			m, _ := newScenario(t, 80, 24)
			m = play(t, m, key)
			if m.StatusNotice() != m.t("feedback.window_only") {
				t.Fatal(m.StatusNotice())
			}
		})
	}
}
