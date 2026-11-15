package ui

import "testing"

// O espectador esconde e recupera a lista com a sequência, sem esperar o popup.
func TestCenarioMapaLogicoLista(t *testing.T) {
	eachSize(t, "mapa-logico-lista", func(t *testing.T, w, h int) Model {
		m, _ := newScenario(t, w, h)
		m = play(t, m, "ctrl+f gaules enter enter ctrl+w h")
		if m.listVisible() {
			t.Fatal("Ctrl+W h não escondeu a lista")
		}
		m = play(t, m, "ctrl+w h")
		if m.LayoutMode() != LayoutSplit {
			t.Fatal("Ctrl+W h não recuperou a lista")
		}
		return m
	})
}
