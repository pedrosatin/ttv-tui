package ui

// Cenários do ticket ux-19, as arestas da janela que também aparecem na TUI.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
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

// listHeader é a primeira linha da tela, sem ANSI: o título da lista.
func listHeader(m Model) string {
	return strings.SplitN(screen(m), "\n", 2)[0]
}

// Na janela, com a lista em 25 colunas, o usuário abre gaules e volta às
// categorias com Ctrl+R: o título com foco perde o rótulo "[foco]" inteiro em
// vez de mostrar "[f…". Tab passa o foco ao chat, e a marca vai junto.
func TestCenarioFocoNaoSeCortaEmColunaEstreita(t *testing.T) {
	eachSize(t, "foco-coluna-estreita", func(t *testing.T, width, height int) Model {
		m, _ := hostedScenario(t, width, height, WithHostedColumns(25, 0, false, false, 0, nil))
		m = play(t, m, "ctrl+f gaules enter enter ctrl+r")
		head := listHeader(m)
		if !strings.HasPrefix(head, "▸ ") {
			t.Fatalf("título da lista com foco sem ▸: %q", head)
		}
		if i := strings.Index(head, "["); i >= 0 && !strings.Contains(head, "[foco]") {
			t.Fatalf("rótulo de foco cortado: %q", head)
		}
		m = play(t, m, "tab")
		if m.Focus() != FocusChat || strings.HasPrefix(listHeader(m), "▸") {
			t.Fatalf("foco %s, título da lista %q", m.Focus(), listHeader(m))
		}
		if !strings.Contains(screen(m), "▸ Chat — #gaules") {
			t.Fatalf("o chat com foco sem ▸ no título:\n%s", screen(m))
		}
		return m
	})
}

// A seleção da lista fica forte com a lista em foco e atenuada com o foco no
// chat: fundo cinza em vez do vídeo reverso. O título do painel com foco é uma
// barra ciano.
func TestSelecaoDaListaAtenuaForaDeFoco(t *testing.T) {
	forceOrientationColorsForTest(true)
	defer forceOrientationColorsForTest(false)
	m, _ := newScenario(t, 100, 24)
	m = play(t, m, "ctrl+f gaules enter enter")
	m = chatLines(t, m, [2]string{"Fulano", "oi"})
	row := func(m Model) string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, "Gaules") && strings.Contains(l, "98.5k") {
				return l
			}
		}
		t.Fatalf("linha de Gaules fora da tela:\n%s", m.View())
		return ""
	}
	focused := row(m)
	if !strings.Contains(focused, "\x1b[1;7m") || strings.Contains(focused, "100m") {
		t.Fatalf("seleção com a lista em foco: %q", focused)
	}
	if head := strings.SplitN(m.View(), "\n", 2)[0]; !strings.Contains(head, "7;36m") && !strings.Contains(head, "1;7;36m") {
		t.Fatalf("título da lista em foco sem a barra ciano: %q", head)
	}
	m = play(t, m, "tab")
	dim := row(m)
	if !strings.Contains(dim, "\x1b[100m") || strings.Contains(dim, "7m") {
		t.Fatalf("seleção com o foco no chat: %q", dim)
	}
}

// Com a Twitch tocando, o usuário troca a lista para a Kick com p: a barra
// mostra "lista: Kick" antes do contexto e mantém o foco e as dicas. Em 80
// colunas, onde os dois não cabem, o foco fica e o título da lista nomeia a Kick.
func TestCenarioBarraComListaDaKick(t *testing.T) {
	eachSize(t, "barra-lista-kick", func(t *testing.T, width, height int) Model {
		m := twoPlatforms(t, i18n.LangPTBR, twitchCatalog(), kickCatalog(), WithChat(&fakeChat{}), WithPlayback(&fakePlayback{}))
		m.timer = scenarioTimer
		m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
		m = openFirst(t, m)
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "twitchone", Volume: 100})
		m = play(t, m, "p")
		bar := statusLine(m)
		for _, want := range []string{"Reproduzindo twitchone", "Foco: lista", "Ctrl+Q sair"} {
			if !strings.Contains(bar, want) {
				t.Fatalf("a barra perdeu %q: %q", want, bar)
			}
		}
		if width >= 100 && !strings.Contains(bar, "lista: Kick") {
			t.Fatalf("a barra de %d colunas sem a plataforma da lista: %q", width, bar)
		}
		return m
	})
}

// Sem token, o usuário abre gaules e aperta i: o aviso e a linha de entrada
// dizem a mesma coisa, citando --import twt. Esc tira o aviso.
func TestCenarioEscreverSemToken(t *testing.T) {
	eachSize(t, "escrever-sem-token", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		w.chat.hasToken = false
		m = play(t, m, "ctrl+f gaules enter enter")
		m = chatLines(t, m, [2]string{"Fulano", "oi"})
		m = play(t, m, "i")
		text := m.t("chat.no_token")
		if m.Composing() || m.ChatSendWarning() != text || !strings.Contains(text, "--import twt") {
			t.Fatalf("i sem token: escrevendo=%v, aviso %q", m.Composing(), m.ChatSendWarning())
		}
		if n := strings.Count(screen(m), "Para enviar:"); n != 2 {
			t.Fatalf("o texto aparece %d vezes; quer no aviso e na linha de entrada:\n%s", n, screen(m))
		}
		after := play(t, m, "esc")
		if after.ChatSendWarning() != "" || strings.Count(screen(after), "Para enviar:") != 1 {
			t.Fatalf("o aviso ficou depois do Esc:\n%s", screen(after))
		}
		return m
	})
}
