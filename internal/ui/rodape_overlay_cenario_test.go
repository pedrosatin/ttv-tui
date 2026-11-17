package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// overlayComChat abre gaules na janela com o chat sobreposto, o vídeo tocando e
// n mensagens no chat. O foco fica na lista.
func overlayComChat(t *testing.T, width, height, n int, classic bool) Model {
	t.Helper()
	var m Model
	if classic {
		m, _ = hostedScenarioClassic(t, width, height)
	} else {
		// hostedScenario monta o mapa clássico; o padrão precisa do newScenario.
		exactCellWidths = true
		t.Cleanup(func() { exactCellWidths = false })
		m, _ = newScenario(t, width, height, WithVideoHost(&recordingHost{}))
	}
	m = play(t, m, "ctrl+f gaules enter enter ctrl+o")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
	m = feed(t, m, ChatEventMsg{Generation: m.chat.panel.gen, Channel: m.chat.panel.channel, State: ChatStateConnected})
	m = chegam(t, m, 0, n)
	if m.footerMode() != footOverlay {
		t.Fatalf("rodapé no modo %d, quero o do chat sobreposto com foco na lista", m.footerMode())
	}
	return m
}

// overlayPausado põe o foco no chat sobreposto, sobe duas páginas e deixa
// chegar três mensagens.
func overlayPausado(t *testing.T, m Model) Model {
	t.Helper()
	m = play(t, m, "tab pgup pgup")
	m = chegam(t, m, 45, 3)
	if m.ChatFollowLive() || m.ChatPendingNew() != 3 {
		t.Fatalf("chat ao vivo = %v com %d novas, quero pausado com 3", m.ChatFollowLive(), m.ChatPendingNew())
	}
	return m
}

func rodapeTem(t *testing.T, m Model, want ...string) {
	t.Helper()
	footer := ansi.Strip(m.Footer())
	for _, w := range want {
		if !strings.Contains(footer, w) {
			t.Fatalf("rodapé %q sem %q", footer, w)
		}
	}
}

func rodapeSem(t *testing.T, m Model, unwanted string) {
	t.Helper()
	if footer := ansi.Strip(m.Footer()); strings.Contains(footer, unwanted) {
		t.Fatalf("rodapé %q com %q", footer, unwanted)
	}
}

// Com o chat sobre o vídeo e o foco na lista, o rodapé diz como levar o foco ao
// chat, além da altura e da opacidade.
func TestCenarioRodapeSobrepostoFocoNaLista(t *testing.T) {
	eachSize(t, "rodape-sobreposto-lista", func(t *testing.T, width, height int) Model {
		m := overlayComChat(t, width, height, 45, true)
		rodapeTem(t, m, "Tab chat", "Alt+↑/↓ altura", "Alt+PgUp/Dn opacidade", "? ajuda")
		rodapeSem(t, m, "End ao vivo")
		return m
	})
}

// Com o foco no chat ao vivo, o rodapé diz como voltar à lista; não há ao vivo
// a retomar.
func TestCenarioRodapeSobrepostoFocoNoChat(t *testing.T) {
	eachSize(t, "rodape-sobreposto-chat", func(t *testing.T, width, height int) Model {
		m := play(t, overlayComChat(t, width, height, 45, true), "tab")
		rodapeTem(t, m, "Tab lista", "Alt+↑/↓ altura", "Alt+PgUp/Dn opacidade", "? ajuda")
		rodapeSem(t, m, "End ao vivo")
		return m
	})
}

// O espectador sobe duas páginas no chat sobreposto e chegam três mensagens: a
// primeira dica do rodapé é a tecla que volta ao vivo. End volta e a dica sai.
func TestCenarioRodapeSobrepostoPausado(t *testing.T) {
	eachSize(t, "rodape-sobreposto-pausado", func(t *testing.T, width, height int) Model {
		m := overlayPausado(t, overlayComChat(t, width, height, 45, true))
		if footer := ansi.Strip(m.Footer()); !strings.HasPrefix(footer, "End ao vivo · Tab lista") {
			t.Fatalf("rodapé %q não começa pelo ao vivo e pela troca de foco", footer)
		}
		rodapeTem(t, m, "Alt+↑/↓ altura", "Alt+PgUp/Dn opacidade", "? ajuda")
		if aoVivo := play(t, m, "end"); !aoVivo.ChatFollowLive() {
			t.Fatal("End não voltou ao vivo")
		} else {
			rodapeSem(t, aoVivo, "End ao vivo")
		}
		return m
	})
}

// Com a lista escondida (Ctrl+W h no padrão, Ctrl+H no clássico), Tab não leva o
// foco a ela: o rodapé oferece a tecla que mostra a lista de novo.
func TestRodapeSobrepostoSemListaNaoOfereceTab(t *testing.T) {
	for _, classic := range []bool{true, false} {
		m := play(t, overlayComChat(t, 80, 24, 45, classic), "tab")
		if classic {
			m = play(t, m, "ctrl+h")
		} else {
			m = play(t, m, "ctrl+w h")
		}
		if m.listVisible() {
			t.Fatalf("classic=%v: a lista continua visível", classic)
		}
		rodapeSem(t, m, "Tab lista")
		want := "Ctrl+W h lista"
		if classic {
			want = "Ctrl+H lista"
		}
		rodapeTem(t, m, want, "Alt+↑/↓ altura", "? ajuda")
		if m = play(t, m, strings.ToLower(strings.TrimSuffix(want, " lista"))); !m.listVisible() {
			t.Fatalf("classic=%v: %s não mostrou a lista", classic, want)
		}
	}
}

// No mapa padrão com os lados trocados, a lista fica à direita e volta com
// Ctrl+W l: é essa a tecla que o rodapé oferece.
func TestRodapeSobrepostoSemListaLadosTrocados(t *testing.T) {
	m := play(t, overlayComChat(t, 80, 24, 45, false), "ctrl+w x tab ctrl+w l")
	if m.listVisible() || !m.hosted.panelsSwapped {
		t.Fatalf("lista visível = %v, lados trocados = %v; quero a lista escondida com os lados trocados", m.listVisible(), m.hosted.panelsSwapped)
	}
	rodapeSem(t, m, "Tab lista")
	rodapeTem(t, m, "Ctrl+W l lista")
	if m = play(t, m, "ctrl+w l"); !m.listVisible() {
		t.Fatal("Ctrl+W l não mostrou a lista")
	}
}

// O mapa padrão e o clássico dão as mesmas teclas a essas dicas; uma tecla
// trocada no config aparece no rodapé.
func TestRodapeSobrepostoSegueOMapa(t *testing.T) {
	for _, classic := range []bool{true, false} {
		m := overlayPausado(t, overlayComChat(t, 80, 24, 45, classic))
		const want = "End ao vivo · Tab lista · Alt+↑/↓ altura · Alt+PgUp/Dn opacidade · ? ajuda"
		if got := strings.TrimRight(ansi.Strip(m.Footer()), " "); got != want {
			t.Fatalf("classic=%v: rodapé %q, quero %q", classic, got, want)
		}
	}
	m := overlayPausado(t, overlayComChat(t, 80, 24, 45, false))
	m.keys = newKeyRegistry(defaultActions(), map[actionID][]string{
		actEnd: {"ctrl+e"}, actChatTaller: {"ctrl+w +"}, actChatShorter: {"ctrl+w -"}, actChatLessOpaque: {},
	})
	rodapeTem(t, m, "Ctrl+E ao vivo", "Ctrl+W +/- altura", "Alt+PgUp opacidade")
	m.keys = newKeyRegistry(defaultActions(), map[actionID][]string{actChatTaller: {}, actChatShorter: {}})
	rodapeSem(t, m, "altura")
}

// O chat sobreposto aparece a partir de 79 colunas, onde cabem todas as dicas.
// Com teclas compridas no config, não cabe tudo: a navegação fica, a opacidade
// sai primeiro e a ajuda fica sempre.
func TestRodapeSobrepostoPrioridadeNaLargura(t *testing.T) {
	m := overlayPausado(t, overlayComChat(t, 79, 24, 45, true))
	rodapeTem(t, m, "End ao vivo", "Tab lista", "altura", "opacidade", "? ajuda")
	m.keys = newKeyRegistry(classicActions(), map[actionID][]string{
		actEnd: {"ctrl+shift+end"}, actChatTaller: {"ctrl+shift+up"}, actChatShorter: {"ctrl+shift+down"},
		actChatMoreOpaque: {"ctrl+shift+pgup"}, actChatLessOpaque: {"ctrl+shift+pgdown"},
	})
	if got := ansi.StringWidth(m.Footer()); got > m.width {
		t.Fatalf("rodapé com %d colunas em %d", got, m.width)
	}
	rodapeTem(t, m, "Ctrl+Shift+End ao vivo · Tab lista · Ctrl+Shift+↑/↓ altura · ? ajuda")
	rodapeSem(t, m, "opacidade")
}

func TestRodapeSobrepostoEmIngles(t *testing.T) {
	m := overlayPausado(t, overlayComChat(t, 80, 24, 45, true))
	m.catalog = mustCatalog(t, i18n.LangEN)
	rodapeTem(t, m, "End live · Tab list · Alt+↑/↓ height · Alt+PgUp/Dn opacity · ? help")
	m = play(t, m, "tab")
	rodapeTem(t, m, "Tab chat · Alt+↑/↓ height")
}
