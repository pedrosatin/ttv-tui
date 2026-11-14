package ui

// Cenários de UX de referência: o que o usuário faz (teclas) e o que ele vê (a
// tela inteira, num golden). Cada um é gravado em 80x24 e 120x40. Para escrever
// um novo, veja docs/cenarios-de-ux.md.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
)

// eachSize roda o cenário em cada tamanho de terminal e grava o golden
// <name>-<largura>x<altura>.
func eachSize(t *testing.T, name string, run func(t *testing.T, width, height int) Model) {
	t.Helper()
	for _, size := range scenarioSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			m := run(t, size.width, size.height)
			assertGolden(t, fmt.Sprintf("%s-%dx%d", name, size.width, size.height), screen(m))
		})
	}
}

// O usuário abre o app, busca "gaules" com Ctrl+F e abre o primeiro resultado:
// o vídeo começa, o chat da sala aparece ao lado e a lista guarda a busca.
func TestCenarioBuscaEAbreCanal(t *testing.T) {
	eachSize(t, "busca-e-abre-canal", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas = %q, quer só \"gaules\"", w.searched)
		}
		if !slices.Equal(w.playback.started, []string{"gaules"}) {
			t.Fatalf("player abriu %q, quer gaules", w.playback.started)
		}
		if !slices.Equal(w.chat.joined, []string{"gaules"}) {
			t.Fatalf("chat entrou em %q, quer gaules", w.chat.joined)
		}

		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		return chatLines(t, m,
			[2]string{"Fulano", "salve, chat"},
			[2]string{"Ciclana", "que jogada"},
		)
	})
}

// Com gaules nos favoritos, o usuário digita "/gau" na tela inicial: o filtro
// deixa só o favorito, que fica destacado.
func TestCenarioFiltroComFavorito(t *testing.T) {
	eachSize(t, "filtro-com-favorito", func(t *testing.T, width, height int) Model {
		fav := scenarioChannels()[0]
		m, w := newScenario(t, width, height, WithFavorites([]catalog.Channel{fav}))
		m = play(t, m, "/ g a u")

		if len(w.searched) != 0 {
			t.Fatalf("o filtro buscou %q na plataforma; ele só filtra o que está na tela", w.searched)
		}
		if it, ok := m.HighlightedItem(); !ok || it.Channel == nil || it.Channel.Login != "gaules" {
			t.Fatalf("destaque = %q, quer o favorito gaules", m.HighlightedID())
		}
		return m
	})
}

// O usuário aperta ? na tela inicial: a ajuda cobre a tela com o título fixo
// no topo e abre nas teclas da lista, onde está o foco. A última linha diz a
// seção, a posição dela entre as sete e a linha.
func TestCenarioAjudaAberta(t *testing.T) {
	for _, lang := range []string{i18n.LangPTBR, i18n.LangEN} {
		name := "ajuda-aberta"
		if lang != i18n.LangPTBR {
			name += "-" + lang
		}
		eachSize(t, name, func(t *testing.T, width, height int) Model {
			m, _ := newScenario(t, width, height)
			m.catalog = mustCatalog(t, lang)
			m = play(t, m, "?")
			assertHelpAt(t, m, sectionNav)
			return m
		})
	}
}

// assertHelpAt confere que a ajuda está aberta na seção: o título dela logo
// abaixo do título fixo, ou à vista na última página, e o nome dela na linha de
// posição.
func assertHelpAt(t *testing.T, m Model, s helpSection) {
	t.Helper()
	view := ansi.Strip(m.View())
	if !strings.Contains(view, m.t("help.title")) {
		t.Fatalf("a ajuda não mostra o título %q:\n%s", m.t("help.title"), view)
	}
	want := helpSectionHeader(m, s)
	cur, total, _ := helpPosition(m)
	if got := helpTopRow(m); got != want && (cur < total || !strings.Contains(view, want)) {
		t.Fatalf("a ajuda abriu em %q, quer %q:\n%s", got, want, view)
	}
	rows := helpBody(m)
	if last := rows[len(rows)-1]; !strings.HasPrefix(last, m.helpSectionName(s)+statusSep) {
		t.Fatalf("a linha de posição %q não diz a seção %q", last, m.helpSectionName(s))
	}
}

// Com um canal aberto, o usuário passa o foco ao chat com Tab e aperta ?: a
// ajuda abre na seção do chat, com as teclas de rolar e de escrever.
func TestCenarioAjudaNoChat(t *testing.T) {
	eachSize(t, "ajuda-no-chat", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "enter enter tab ?")
		assertHelpAt(t, m, sectionChat)
		return m
	})
}

// Com o filtro aberto e "ga" digitado, o usuário aperta F1: a ajuda abre em
// Busca e filtros, e fechá-la devolve o filtro como estava.
func TestCenarioAjudaNoFiltro(t *testing.T) {
	eachSize(t, "ajuda-no-filtro", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "/ g a f1")
		assertHelpAt(t, m, sectionSearch)
		if closed := play(t, m, "f1"); !closed.Filtering() || closed.list.filter != "ga" {
			t.Fatalf("fechar a ajuda mudou o filtro: filtrando=%v texto=%q", closed.Filtering(), closed.list.filter)
		}
		return m
	})
}

// Na ajuda, o usuário aperta Tab quatro vezes, da lista à janela e layout: lá
// estão as teclas da janela, com a marca de que só valem nela.
func TestCenarioAjudaPulaDeSecao(t *testing.T) {
	eachSize(t, "ajuda-pula-de-secao", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "? tab tab tab tab")
		assertHelpAt(t, m, sectionWindow)
		if view := ansi.Strip(m.View()); !strings.Contains(view, m.t("help.ctx.window_only")) || !strings.Contains(view, "Ctrl+O") {
			t.Fatalf("a seção da janela não mostra Ctrl+O sob %q:\n%s", m.t("help.ctx.window_only"), view)
		}
		return play(t, m, "shift+tab right")
	})
}

// O usuário digita "gaules ao vivo" na busca e aperta Ctrl+Backspace, que chega
// como Alt+Backspace: some a última palavra, e a lista continua à vista.
func TestCenarioCtrlBackspaceNaBusca(t *testing.T) {
	eachSize(t, "ctrl-backspace-na-busca", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules space ao space vivo alt+backspace")

		if got := m.list.search.input; got != "gaules ao " {
			t.Fatalf("campo da busca = %q, quer \"gaules ao \"", got)
		}
		return m
	})
}
