package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func BenchmarkCategoryNavigationFrame(b *testing.B) {
	m := batchCategoriesModel(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.moveHighlight(1)
		_ = m.View()
	}
}

// O usuário segura Down, volta com Up e vê a categoria em que parou.
func TestCenarioRepeticaoSetasCategorias(t *testing.T) {
	eachSize(t, "repeticao-setas-categorias", func(t *testing.T, width, height int) Model {
		m := batchCategoriesModel(t, 1000)
		updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = play(t, updated.(Model), strings.Repeat("down ", 80)+strings.Repeat("up ", 20))
		if m.HighlightedID() != "cat-61" {
			t.Fatalf("destaque = %s", m.HighlightedID())
		}
		return m
	})
}
