package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// O usuário abre na Kick um canal dos recentes. A Kick responde 403, o
// streamlink não acha Chrome nem Chromium para a checagem e a barra diz qual
// chave do config resolve, em vez do erro genérico de pedido recusado.
func TestCenarioKickSemNavegador(t *testing.T) {
	eachSize(t, "kick-sem-navegador", func(t *testing.T, width, height int) Model {
		kick := kickCatalog()
		m, w := newScenario(t, width, height,
			WithPlatformCatalog(platform.Kick, kick, kick),
			WithRecents([]catalog.Channel{{Platform: platform.Kick, Login: "xqc", DisplayName: "xQc", URL: "https://kick.com/xqc"}}))
		m = play(t, m, "p")
		if lineWith(m, "xQc") < 0 {
			t.Fatalf("o recente da Kick não está na tela:\n%s", ansi.Strip(m.View()))
		}
		m = play(t, m, "enter")
		if !slices.Equal(w.playback.started, []string{"kick:xqc"}) {
			t.Fatalf("player abriu %q, quer kick:xqc", w.playback.started)
		}
		m = feed(t, m, PlayerStatusMsg{
			Generation: 1,
			State:      PlayerStateError,
			Login:      "kick:xqc",
			LastLine:   "[plugins.kick][error] Could not find Chromium-based web browser executable: Please set the path to a supported web browser using --webbrowser-executable",
			Failure:    PlayerFailureBrowserMissing,
		})
		bar := ansi.Strip(m.StatusBar())
		if !strings.Contains(bar, "kick_browser") {
			t.Fatalf("a barra não cita a chave kick_browser: %q", bar)
		}
		return m
	})
}
