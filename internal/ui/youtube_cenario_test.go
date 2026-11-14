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

// Com a lista na Twitch, o usuário abre a busca (Ctrl+F) e digita "@LofiGirl":
// o atalho abre a live do YouTube sem disparar busca na Twitch e mantém a lista
// na Twitch, com chat só leitura e barra sem intervalo.
func TestCenarioYouTubeBuscaArroba(t *testing.T) {
	eachSize(t, "youtube-busca-arroba", func(t *testing.T, width, height int) Model {
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

		if len(w.searched) != 0 {
			t.Fatalf("buscas na Twitch = %q, quer nenhuma ao abrir @canal do YouTube", w.searched)
		}
		if m.ListPlatform() != platform.Twitch {
			t.Fatalf("lista mudou para %q, quer continuar em twitch", m.ListPlatform())
		}
		if !slices.Equal(w.playback.started, []string{"youtube:@LofiGirl"}) {
			t.Fatalf("player abriu %q, quer youtube:@LofiGirl", w.playback.started)
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
