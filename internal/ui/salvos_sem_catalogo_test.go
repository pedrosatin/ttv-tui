package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/pedrosatin/ttv-tui/internal/catalog"
	"github.com/pedrosatin/ttv-tui/internal/platform"
)

// O usuário encontra salvos sem catálogo na Twitch, alterna a lista e desfavorita o canal de facebook.com.
func TestCenarioSalvosSemCatalogo(t *testing.T) {
	eachSize(t, "salvos-sem-catalogo", func(t *testing.T, width, height int) Model {
		kick := kickCatalog()
		m, _ := newScenario(t, width, height,
			WithPlatformCatalog(platform.Kick, kick, kick),
			WithFavorites([]catalog.Channel{{Platform: "facebook.com", Login: "pagina", DisplayName: "pagina", URL: "https://facebook.com/pagina"}}),
			WithRecents([]catalog.Channel{{Platform: platform.YouTube, Login: "video", DisplayName: "video", URL: "https://youtube.com/watch?v=video"}}))
		check := func(m Model, visible bool) {
			t.Helper()
			view := ansi.Strip(m.View())
			for _, label := range []string{"[YouTube] video", "[facebook.com] pagina"} {
				if strings.Contains(view, label) != visible {
					t.Fatalf("marca %q, visível %v, tela\n%s", label, visible, view)
				}
			}
		}
		check(m, true)
		m = play(t, m, "p")
		check(m, false)
		m = play(t, m, "p")
		check(m, true)
		it, ok := m.HighlightedItem()
		if !ok || it.Channel == nil || it.Channel.CanonicalPlatform() != "facebook.com" {
			t.Fatal("favorito do Facebook não está destacado")
		}
		m = play(t, m, "f")
		if len(m.Favorites()) != 0 {
			t.Fatalf("favoritos após f = %+v", m.Favorites())
		}
		return m
	})
}

// Um host longo na marca não esconde o nome do canal em 80 colunas.
func TestMarcaDeHostLongoMantemONome(t *testing.T) {
	host := strings.Repeat("a", 121) + ".com"
	m, _ := newScenario(t, 80, 24,
		WithFavorites([]catalog.Channel{{Platform: host, Login: "canal", DisplayName: "canal", URL: "https://" + host + "/canal"}}))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "] canal") {
		t.Fatalf("nome do canal sumiu atrás da marca:\n%s", view)
	}
}
