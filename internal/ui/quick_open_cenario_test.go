package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/i18n"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// Ctrl+K abre o seletor sobre a tela inicial; digitar filtra; Esc volta.
func TestCenarioSeletorRapidoAbreFiltraEEsc(t *testing.T) {
	eachSize(t, "seletor-rapido-abre-filtra-esc", func(t *testing.T, width, height int) Model {
		fav := scenarioChannels()[0]
		fav.Platform = platform.Twitch
		m, _ := newScenario(t, width, height, WithFavorites([]catalog.Channel{fav}))
		m = play(t, m, "ctrl+k")
		if !m.list.quickOpen.active {
			t.Fatal("Ctrl+K não abriu o seletor")
		}
		screen := ansi.Strip(m.View())
		if !strings.Contains(screen, m.t("quick_open.title")) {
			t.Fatalf("tela sem título do seletor:\n%s", screen)
		}
		m = play(t, m, "g a u")
		if m.list.quickOpen.input != "gau" {
			t.Fatalf("input = %q, quer gau", m.list.quickOpen.input)
		}
		items := m.quickOpenItems()
		if len(items) == 0 || items[0].Channel == nil || items[0].Channel.Login != "gaules" {
			t.Fatalf("filtro não destacou gaules: %+v", items)
		}
		m = play(t, m, "esc")
		if m.list.quickOpen.active {
			t.Fatal("Esc não fechou o seletor")
		}
		if m.Mode() != ModeCategories {
			t.Fatalf("modo = %s, quer categorias", m.Mode())
		}
		return m
	})
}

// Enter num favorito do seletor abre o canal e começa o vídeo.
func TestCenarioSeletorRapidoAbreCanal(t *testing.T) {
	eachSize(t, "seletor-rapido-abre-canal", func(t *testing.T, width, height int) Model {
		fav := scenarioChannels()[0]
		fav.Platform = platform.Twitch
		m, w := newScenario(t, width, height, WithFavorites([]catalog.Channel{fav}))
		m = play(t, m, "ctrl+k g a u enter")
		if m.list.quickOpen.active {
			t.Fatal("seletor ficou aberto depois do Enter")
		}
		if !slices.Equal(w.playback.started, []string{"gaules"}) {
			t.Fatalf("player abriu %q, quer gaules", w.playback.started)
		}
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		return m
	})
}

// Enter numa ação do seletor a executa (mudo).
func TestCenarioSeletorRapidoExecutaAcao(t *testing.T) {
	eachSize(t, "seletor-rapido-executa-acao", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "enter enter")
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100})
		m = play(t, m, "ctrl+k")
		// "mudo" casa a descrição em pt-BR; as setas levam até a ação se outras
		// linhas também casarem.
		m = play(t, m, "m u d o")
		items := m.quickOpenItems()
		idx := -1
		for i, it := range items {
			if it.Kind == quickAction && it.Action == actMute {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("ação mudo ausente em %+v", items)
		}
		for step := 0; m.list.quickOpen.cursor != idx && step < len(items); step++ {
			m = play(t, m, "down")
		}
		if m.list.quickOpen.cursor != idx {
			t.Fatalf("cursor %d, quer %d", m.list.quickOpen.cursor, idx)
		}
		m = play(t, m, "enter")
		if w.playback.muteToggles != 1 {
			t.Fatalf("ação mudo pediu %d trocas, quer 1", w.playback.muteToggles)
		}
		m = feed(t, m, PlayerStatusMsg{Generation: 1, State: PlayerStatePlaying, Login: "gaules", Volume: 100, Muted: true})
		return m
	})
}

// Sem resultado, Enter busca na plataforma com o texto, como o Ctrl+F.
func TestCenarioSeletorRapidoEnterSemResultadoBusca(t *testing.T) {
	eachSize(t, "seletor-rapido-enter-busca", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+k z e l d a enter")
		if m.list.quickOpen.active {
			t.Fatal("seletor ficou aberto")
		}
		if !slices.Equal(w.searched, []string{"zelda"}) {
			t.Fatalf("buscas = %q, quer zelda", w.searched)
		}
		if m.Mode() != ModeSearch {
			t.Fatalf("modo = %s, quer search", m.Mode())
		}
		return m
	})
}

// [keys] troca a tecla do seletor sem mudança extra no despacho.
func TestSeletorRapidoTeclaCustomizavel(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	reg, notices := EffectiveKeymap(false, map[string][]string{
		"quick_open": {"f2"},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	if got := reg.keys[actQuickOpen]; !slices.Equal(got, []string{"f2"}) {
		t.Fatalf("teclas = %q, quer [f2]", got)
	}
	msg, ok := parseKeyName("f2")
	if !ok {
		t.Fatal("f2 inválido")
	}
	if reg.actionFor(modeNormal, msg) != actQuickOpen {
		t.Fatalf("f2 no normal dispara %q", reg.actionFor(modeNormal, msg))
	}
	ctrlK, ok := parseKeyName("ctrl+k")
	if !ok {
		t.Fatal("ctrl+k inválido")
	}
	if reg.actionFor(modeNormal, ctrlK) != "" {
		t.Fatalf("ctrl+k ainda dispara %q depois do override", reg.actionFor(modeNormal, ctrlK))
	}
}

// Ações sem tecla (desligadas) não entram no seletor.
func TestSeletorRapidoOmiteAcaoDesligada(t *testing.T) {
	cat, err := i18n.New(i18n.LangPTBR)
	if err != nil {
		t.Fatal(err)
	}
	reg, notices := EffectiveKeymap(false, map[string][]string{
		"mute": {},
	}, cat)
	if len(notices) != 0 {
		t.Fatalf("avisos: %v", notices)
	}
	m, _ := newScenario(t, 80, 24)
	m.keys = reg
	m = play(t, m, "ctrl+k m u t e")
	for _, it := range m.quickOpenItems() {
		if it.Action == actMute {
			t.Fatal("ação mudo desligada apareceu no seletor")
		}
	}
}
