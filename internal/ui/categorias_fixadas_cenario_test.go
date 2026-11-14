package ui

import (
	"strings"
	"testing"
)

// O usuário fixa VALORANT com f e vê o nome no aviso e a categoria no topo.
func TestCenarioFixaCategoria(t *testing.T) {
	eachSize(t, "fixa-categoria", func(t *testing.T, width, height int) Model {
		m, _ := newScenario(t, width, height)
		m = play(t, m, "down down f")
		if !strings.Contains(m.StatusNotice(), "★ VALORANT fixada") {
			t.Fatalf("aviso = %q, quer ★ VALORANT fixada", m.StatusNotice())
		}
		if it := m.VisibleItems()[0]; it.Category == nil || it.Category.Name != "VALORANT" || !it.Favorite {
			t.Fatalf("primeiro item = %+v, quer VALORANT fixada", it)
		}
		return m
	})
}
