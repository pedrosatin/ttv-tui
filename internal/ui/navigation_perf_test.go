package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func BenchmarkCategoryNavigationFrame(b *testing.B) {
	m := batchCategoriesModel(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		direction := 1
		if (i/999)%2 == 1 {
			direction = -1
		}
		m.moveHighlight(direction)
		_ = m.View()
	}
}

// A rolagem longa mantém o recorte e o destaque da lista nos goldens.
// A rajada de autorepeat é coberta pelo cenário SDL da janela.
func TestCenarioRolagemLongaDasCategorias(t *testing.T) {
	eachSize(t, "rolagem-longa-categorias", func(t *testing.T, width, height int) Model {
		m := batchCategoriesModel(t, 1000)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = play(t, updated.(Model), strings.Repeat("down ", 80)+strings.Repeat("up ", 20))
		if m.HighlightedID() != "cat-61" {
			t.Fatalf("destaque = %s", m.HighlightedID())
		}
		return m
	})
}
