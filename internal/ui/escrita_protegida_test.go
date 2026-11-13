package ui

// O que se digita num campo de texto (a mensagem do chat, o filtro / e o prompt
// de busca) vai ao campo antes de qualquer atalho de layout ou de volume.

import (
	"slices"
	"testing"
)

// scenarioPlaying abre o primeiro canal da primeira categoria, com o vídeo
// tocando e a sala do chat conectada.
func scenarioPlaying(t *testing.T, width, height int) (Model, *scenarioWorld) {
	t.Helper()
	m, w := newScenario(t, width, height)
	m = play(t, m, "enter enter")
	m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 50})
	m = chatLines(t, m, [2]string{"Fulano", "salve, chat"})
	if m.LayoutMode() != LayoutSplit || !m.chatColumnVisible() {
		t.Fatalf("preparação: layout %q, chat visível %v; quer split com chat", m.LayoutMode(), m.chatColumnVisible())
	}
	return m, w
}

// O espectador escreve "oi", aperta Ctrl+H sem querer e continua com "x": o
// rascunho fica "oix" e a lista continua à vista.
func TestCenarioCtrlHNaEscrita(t *testing.T) {
	eachSize(t, "ctrl-h-na-escrita", func(t *testing.T, width, height int) Model {
		m, _ := scenarioPlaying(t, width, height)
		m = play(t, m, "i oi ctrl+h x")

		if got := m.chat.composeDraft; got != "oix" {
			t.Fatalf("rascunho = %q, quer \"oix\"", got)
		}
		if !m.chat.composing || !m.layout.showList || m.LayoutMode() != LayoutSplit {
			t.Fatalf("escrevendo %v, lista %v, layout %q; quer escrita aberta com a lista à vista", m.chat.composing, m.layout.showList, m.LayoutMode())
		}
		return m
	})
}

// O espectador escreve "a=_": o rascunho guarda os três caracteres, sem trocar
// = por + nem _ por -, e o volume não muda.
func TestCenarioIgualESublinhadoNaEscrita(t *testing.T) {
	eachSize(t, "igual-e-sublinhado-na-escrita", func(t *testing.T, width, height int) Model {
		m, w := scenarioPlaying(t, width, height)
		m = play(t, m, "i a=_")

		if got := m.chat.composeDraft; got != "a=_" {
			t.Fatalf("rascunho = %q, quer \"a=_\"", got)
		}
		if len(w.playback.volDeltas) != 0 {
			t.Fatalf("o volume mudou na escrita: %v", w.playback.volDeltas)
		}
		return m
	})
}

// textField descreve um campo de texto: as teclas que o abrem, onde ler o que
// foi digitado nele e se ele continua aberto.
type textField struct {
	name   string
	open   string
	value  func(Model) string
	isOpen func(Model) bool
}

func textFields() []textField {
	return []textField{
		{"escrita", "i", func(m Model) string { return m.chat.composeDraft }, func(m Model) bool { return m.chat.composing }},
		{"filtro", "/", func(m Model) string { return m.list.filter }, func(m Model) bool { return m.list.filtering }},
		{"busca", "ctrl+f", func(m Model) string { return m.list.search.input }, func(m Model) bool { return m.list.search.active }},
	}
}

func TestCampoDeTextoRecebeATeclaAntesDoAtalho(t *testing.T) {
	cases := []struct {
		name string
		keys string
		want string
	}{
		{"Ctrl+H não esconde a lista nem apaga", "oi ctrl+h x", "oix"},
		{"= e _ entram como digitados", "a=_", "a=_"},
		{"Ctrl+W apaga a palavra anterior", "fix space ab ctrl+w", "fix "},
		{"Ctrl+W repetido apaga a palavra seguinte", "fix space ab ctrl+w ctrl+w", ""},
		{"Ctrl+W com espaço no fim apaga a palavra e o espaço", "fix space ab space ctrl+w", "fix "},
		{"Alt+Backspace continua apagando a palavra", "fix space ab alt+backspace", "fix "},
		{"Backspace apaga uma letra", "ab backspace", "a"},
	}
	for _, f := range textFields() {
		for _, tc := range cases {
			t.Run(f.name+"/"+tc.name, func(t *testing.T) {
				m, w := scenarioPlaying(t, 120, 40)
				m = play(t, m, f.open+" "+tc.keys)

				if got := f.value(m); got != tc.want {
					t.Fatalf("campo = %q, quer %q", got, tc.want)
				}
				if !f.isOpen(m) {
					t.Fatalf("o campo fechou")
				}
				if m.LayoutMode() != LayoutSplit || !m.layout.showList || !m.layout.showChat {
					t.Fatalf("layout %q, lista %v, chat %v; a escrita mexeu no layout", m.LayoutMode(), m.layout.showList, m.layout.showChat)
				}
				if len(w.playback.volDeltas) != 0 {
					t.Fatalf("o volume mudou na escrita: %v", w.playback.volDeltas)
				}
			})
		}
	}
}

// As teclas que valem na escrita continuam passando: Ctrl+Q sai, F1 abre a
// ajuda, Esc fecha o campo.
func TestCampoDeTextoDeixaPassarAsTeclasDaEscrita(t *testing.T) {
	for _, f := range textFields() {
		t.Run(f.name+"/Ctrl+Q sai", func(t *testing.T) {
			m, w := scenarioPlaying(t, 120, 40)
			m = play(t, m, f.open+" oi ctrl+q")
			if !m.quitting {
				t.Fatal("Ctrl+Q no campo não saiu do app")
			}
			if w.playback.stopped == 0 {
				t.Fatal("Ctrl+Q no campo não parou o player")
			}
		})
		t.Run(f.name+"/F1 abre a ajuda", func(t *testing.T) {
			m, _ := scenarioPlaying(t, 120, 40)
			m = play(t, m, f.open+" oi f1")
			if !m.helpOpen() {
				t.Fatal("F1 no campo não abriu a ajuda")
			}
			if got := f.value(m); got != "oi" {
				t.Fatalf("a ajuda mexeu no rascunho: %q", got)
			}
		})
		t.Run(f.name+"/Esc fecha o campo", func(t *testing.T) {
			m, _ := scenarioPlaying(t, 120, 40)
			m = play(t, m, f.open+" oi esc")
			if f.isOpen(m) {
				t.Fatal("Esc não fechou o campo")
			}
		})
	}
}

// Fora de um campo de texto os atalhos continuam como antes.
func TestForaDaEscritaOsAtalhosContinuam(t *testing.T) {
	cases := []struct {
		name  string
		keys  string
		check func(*testing.T, Model, *scenarioWorld)
	}{
		{"Ctrl+W troca o layout", "ctrl+w", func(t *testing.T, m Model, _ *scenarioWorld) {
			if m.LayoutMode() == LayoutSplit {
				t.Fatal("Ctrl+W fora da escrita não trocou o layout")
			}
		}},
		{"Ctrl+H esconde a lista", "ctrl+h", func(t *testing.T, m Model, _ *scenarioWorld) {
			if m.layout.showList {
				t.Fatal("Ctrl+H fora da escrita não escondeu a lista")
			}
		}},
		{"= sobe o volume", "=", func(t *testing.T, _ Model, w *scenarioWorld) {
			if !slices.Equal(w.playback.volDeltas, []int{5}) {
				t.Fatalf("volume = %v, quer [5]", w.playback.volDeltas)
			}
		}},
		{"_ desce o volume", "_", func(t *testing.T, _ Model, w *scenarioWorld) {
			if !slices.Equal(w.playback.volDeltas, []int{-5}) {
				t.Fatalf("volume = %v, quer [-5]", w.playback.volDeltas)
			}
		}},
		{"Esc depois da escrita devolve o Ctrl+W ao layout", "i oi esc ctrl+w", func(t *testing.T, m Model, _ *scenarioWorld) {
			if m.chat.composeDraft != "oi" || m.LayoutMode() == LayoutSplit {
				t.Fatalf("rascunho %q, layout %q; quer o rascunho guardado e o layout trocado", m.chat.composeDraft, m.LayoutMode())
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, w := scenarioPlaying(t, 120, 40)
			m = play(t, m, tc.keys)
			tc.check(t, m, w)
		})
	}
}
