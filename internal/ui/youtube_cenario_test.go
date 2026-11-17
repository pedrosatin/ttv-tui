package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// O usuário cola a URL de uma live do YouTube com Ctrl+F: abre o canal sem
// catálogo, com chat ao lado só leitura e a barra nunca mostra intervalo.
func TestCenarioYouTubeColaURL(t *testing.T) {
	eachSize(t, "youtube-cola-url", func(t *testing.T, width, height int) Model {
		title := "lofi hip hop radio - beats to relax/study to"
		yt := &platformResolver{answer: []catalog.Channel{{
			Platform:     platform.YouTube,
			ID:           "jfKfPfyJRdk",
			Login:        "jfKfPfyJRdk",
			DisplayName:  "Lofi Girl",
			URL:          "https://www.youtube.com/watch?v=jfKfPfyJRdk",
			Availability: catalog.AvailabilityLive,
			Viewers:      catalog.IntPtr(24500),
			Title:        &title,
		}}}
		m, w := newScenario(t, width, height, WithPlatformResolver(platform.YouTube, yt))
		w.chat.platforms = map[string]catalog.ChatCapabilities{
			platform.YouTube: catalog.ReadOnlyChatCapabilities,
		}

		m = play(t, m, "ctrl+f")
		m = feed(t, m, tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune("https://www.youtube.com/watch?v=jfKfPfyJRdk"),
			Paste: true,
		})
		m = play(t, m, "enter")

		if len(w.searched) != 0 {
			t.Fatalf("buscas na Twitch = %q, quer nenhuma ao colar URL do YouTube", w.searched)
		}
		if !slices.Equal(w.playback.started, []string{"youtube:jfKfPfyJRdk"}) {
			t.Fatalf("player abriu %q, quer youtube:jfKfPfyJRdk", w.playback.started)
		}
		if !slices.Equal(w.chat.joined, []string{"jfKfPfyJRdk"}) {
			t.Fatalf("chat entrou em %q, quer jfKfPfyJRdk", w.chat.joined)
		}
		if m.ChatCapabilities().CanSend {
			t.Fatal("chat do YouTube deve ser só leitura")
		}

		m = feed(t, m, PlayerStatusMsg{
			Generation: 1,
			State:      PlayerStatePlaying,
			Login:      "youtube:jfKfPfyJRdk",
			Volume:     100,
		})
		// Descarta o aviso único da janela do mpv (como na primeira tecla do
		// usuário) para a barra mostrar o estado de reprodução sem intervalo.
		m = play(t, m, "tab")
		m = chatLines(t, m,
			[2]string{"Ana", "bom som para estudar"},
			[2]string{"Bruno", "salve do Brasil"},
		)

		bar := ansi.Strip(m.StatusBar())
		if strings.Contains(strings.ToLower(bar), "intervalo") || strings.Contains(strings.ToLower(bar), "ad break") {
			t.Fatalf("barra mostrou intervalo no YouTube: %q", bar)
		}
		if !strings.Contains(bar, "Lofi Girl (YouTube)") {
			t.Fatalf("barra não identifica o canal do YouTube: %q", bar)
		}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "YouTube: só leitura") {
			t.Fatalf("painel não mostra aviso de chat só leitura:\n%s", view)
		}
		if !strings.Contains(view, "Chat — Lofi Girl") {
			t.Fatalf("cabeçalho do chat não mostra o nome do canal:\n%s", view)
		}
		if strings.Contains(view, "Chat — jfKfPfyJRdk") {
			t.Fatalf("cabeçalho do chat ainda mostra o ID do vídeo:\n%s", view)
		}
		return m
	})
}

// youtubeOfferText é a linha que oferece abrir o @canal no YouTube, em pt-BR.
func youtubeOfferText(handle string) string {
	return "Abrir " + handle + " no YouTube"
}

// assertYouTubeOfferLast confere que a oferta do YouTube está na tela, abaixo
// de todos os resultados da plataforma, e que nada abriu o player.
func assertYouTubeOfferLast(t *testing.T, m Model, w *scenarioWorld, handle string, results ...string) {
	t.Helper()
	offer := lineWith(m, youtubeOfferText(handle))
	if offer < 0 {
		t.Fatalf("a busca não oferece %q:\n%s", youtubeOfferText(handle), ansi.Strip(m.View()))
	}
	for _, r := range results {
		at := lineWith(m, r)
		if at < 0 {
			t.Fatalf("o resultado %q da plataforma não está na tela:\n%s", r, ansi.Strip(m.View()))
		}
		if at > offer {
			t.Fatalf("o resultado %q ficou abaixo da oferta do YouTube:\n%s", r, ansi.Strip(m.View()))
		}
	}
	if len(w.playback.started) != 0 {
		t.Fatalf("player abriu %q sem o usuário escolher a linha do YouTube", w.playback.started)
	}
}

// Com a lista na Twitch, o usuário busca "@gaules" com Ctrl+F: na Twitch, @nome
// é como se chama alguém, então a busca vai à Twitch com "gaules" e mostra os
// resultados de lá. A última linha oferece abrir @gaules no YouTube, e nada
// toca até o usuário escolher.
func TestCenarioYouTubeBuscaArroba(t *testing.T) {
	eachSize(t, "youtube-busca-arroba", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f @gaules enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas na Twitch = %q, quer [gaules]", w.searched)
		}
		if m.ListPlatform() != platform.Twitch {
			t.Fatalf("lista mudou para %q, quer continuar em twitch", m.ListPlatform())
		}
		assertYouTubeOfferLast(t, m, w, "@gaules", "Gaules", "GaulesTV")
		if it, ok := m.HighlightedItem(); !ok || it.Channel == nil || it.Channel.Login != "gaules" {
			t.Fatalf("destaque = %+v, quer o canal exato gaules da Twitch", it)
		}
		return m
	})
}

// Com a lista na Twitch, o usuário busca "@LofiGirl", desce até a última linha
// e aperta Enter na oferta: abre a live do YouTube, com chat só leitura, a
// barra sem intervalo e a lista ainda na Twitch.
func TestCenarioYouTubeBuscaArrobaAbre(t *testing.T) {
	eachSize(t, "youtube-busca-arroba-abre", func(t *testing.T, width, height int) Model {
		title := "lofi hip hop radio - beats to relax/study to"
		yt := &platformResolver{answer: []catalog.Channel{{
			Platform:     platform.YouTube,
			ID:           "@LofiGirl",
			Login:        "@LofiGirl",
			DisplayName:  "Lofi Girl",
			URL:          "https://www.youtube.com/@LofiGirl/live",
			Availability: catalog.AvailabilityLive,
			Viewers:      catalog.IntPtr(24500),
			Title:        &title,
		}}}
		m, w := newScenario(t, width, height, WithPlatformResolver(platform.YouTube, yt))
		w.chat.platforms = map[string]catalog.ChatCapabilities{
			platform.Twitch:  catalog.TwitchChatCapabilities,
			platform.YouTube: catalog.ReadOnlyChatCapabilities,
		}

		m = play(t, m, "ctrl+f @LofiGirl enter")
		assertYouTubeOfferLast(t, m, w, "@LofiGirl", "Gaules", "GaulesTV")
		m = play(t, m, "end enter")

		if !slices.Equal(w.searched, []string{"LofiGirl"}) {
			t.Fatalf("buscas na Twitch = %q, quer [LofiGirl]", w.searched)
		}
		if m.ListPlatform() != platform.Twitch {
			t.Fatalf("lista mudou para %q, quer continuar em twitch", m.ListPlatform())
		}
		if !slices.Equal(w.playback.started, []string{"youtube:@LofiGirl"}) {
			t.Fatalf("player abriu %q, quer youtube:@LofiGirl", w.playback.started)
		}
		if got := w.playback.targets[0].Channel.URL; got != "https://www.youtube.com/@LofiGirl/live" {
			t.Fatalf("URL do player = %q, quer a live do @LofiGirl", got)
		}
		if !slices.Equal(w.chat.joined, []string{"@LofiGirl"}) {
			t.Fatalf("chat entrou em %q, quer @LofiGirl", w.chat.joined)
		}
		if m.ChatCapabilities().CanSend {
			t.Fatal("chat do YouTube deve ser só leitura")
		}

		m = feed(t, m, PlayerStatusMsg{
			Generation: 1,
			State:      PlayerStatePlaying,
			Login:      "youtube:@LofiGirl",
			Volume:     100,
		})
		m = play(t, m, "tab")
		m = chatLines(t, m,
			[2]string{"Ana", "bom som para estudar"},
			[2]string{"Bruno", "salve do Brasil"},
		)

		bar := ansi.Strip(m.StatusBar())
		if strings.Contains(strings.ToLower(bar), "intervalo") || strings.Contains(strings.ToLower(bar), "ad break") {
			t.Fatalf("barra mostrou intervalo no YouTube: %q", bar)
		}
		if !strings.Contains(bar, "Lofi Girl (YouTube)") {
			t.Fatalf("barra não identifica o canal do YouTube: %q", bar)
		}
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "YouTube: só leitura") {
			t.Fatalf("painel não mostra aviso de chat só leitura:\n%s", view)
		}
		if !strings.Contains(view, "Chat — Lofi Girl") {
			t.Fatalf("cabeçalho do chat não mostra o nome do canal:\n%s", view)
		}
		return m
	})
}

// O usuário busca "gaules", sem @: os resultados da Twitch aparecem sozinhos,
// sem a linha do YouTube.
func TestCenarioYouTubeBuscaSemArroba(t *testing.T) {
	eachSize(t, "youtube-busca-sem-arroba", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "ctrl+f gaules enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas na Twitch = %q, quer [gaules]", w.searched)
		}
		if lineWith(m, "Gaules") < 0 {
			t.Fatalf("os resultados da Twitch não estão na tela:\n%s", ansi.Strip(m.View()))
		}
		if lineWith(m, "no YouTube") >= 0 {
			t.Fatalf("busca sem @ ofereceu o YouTube:\n%s", ansi.Strip(m.View()))
		}
		return m
	})
}

// Com a lista na Kick, o usuário busca "@gaules": a busca vai à Kick com
// "gaules", mostra o canal de lá e oferece o YouTube na última linha.
func TestCenarioYouTubeBuscaArrobaKick(t *testing.T) {
	eachSize(t, "youtube-busca-arroba-kick", func(t *testing.T, width, height int) Model {
		kick := kickCatalog()
		m, w := newScenario(t, width, height, WithPlatformCatalog(platform.Kick, kick, kick))
		m = play(t, m, "p")
		if m.ListPlatform() != platform.Kick {
			t.Fatalf("lista em %q, quer kick", m.ListPlatform())
		}
		m = play(t, m, "ctrl+f @gaules enter")

		calls := kick.calls()
		if !slices.Contains(calls, "search:gaules") || slices.Contains(calls, "search:@gaules") {
			t.Fatalf("buscas na Kick = %q, quer search:gaules", calls)
		}
		if len(w.searched) != 0 {
			t.Fatalf("buscas na Twitch = %q, quer nenhuma com a lista na Kick", w.searched)
		}
		assertYouTubeOfferLast(t, m, w, "@gaules", "Gaules", "Project Zomboid")
		return m
	})
}

// Com a lista na Twitch, a busca por "@gaules" não acha nada: a linha do
// YouTube fica sozinha na lista, com o destaque, e só abre com Enter.
func TestCenarioYouTubeBuscaArrobaVazia(t *testing.T) {
	eachSize(t, "youtube-busca-arroba-vazia", func(t *testing.T, width, height int) Model {
		empty := &countingSource{category: scenarioCategories()}
		m, w := newScenario(t, width, height, WithPlatformCatalog(platform.Twitch, empty, empty))
		m = play(t, m, "ctrl+f @gaules enter")

		if calls := empty.calls(); !slices.Contains(calls, "search:gaules") {
			t.Fatalf("buscas na Twitch = %q, quer search:gaules", calls)
		}
		assertYouTubeOfferLast(t, m, w, "@gaules")
		if got := len(m.VisibleItems()); got != 1 {
			t.Fatalf("a lista tem %d itens, quer só a oferta do YouTube", got)
		}
		if highlightedLine(m) != lineWith(m, youtubeOfferText("@gaules")) {
			t.Fatalf("o destaque não está na oferta do YouTube:\n%s", ansi.Strip(m.View()))
		}
		return m
	})
}

// O usuário filtra a tela inicial com "/" e "@gaules". Nada casa, e Enter leva o
// texto à busca da Twitch, como qualquer filtro sem resultado: o YouTube não
// abre sozinho, e a oferta aparece como a última linha dos resultados.
func TestCenarioYouTubeFiltroArroba(t *testing.T) {
	eachSize(t, "youtube-filtro-arroba", func(t *testing.T, width, height int) Model {
		m, w := newScenario(t, width, height)
		m = play(t, m, "/ @gaules enter")

		if !slices.Equal(w.searched, []string{"gaules"}) {
			t.Fatalf("buscas na Twitch = %q, quer [gaules]", w.searched)
		}
		if m.Filtering() {
			t.Fatal("o filtro continua aberto depois do Enter sem resultado")
		}
		assertYouTubeOfferLast(t, m, w, "@gaules", "Gaules", "GaulesTV")
		return m
	})
}
