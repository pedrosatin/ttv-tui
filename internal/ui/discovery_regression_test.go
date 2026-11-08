package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// Uses the pre-discovery API too, so it can run against the released source.
func TestDiscoveryFailureVisibleWithoutKeys(t *testing.T) {
	for _, sized := range []bool{false, true} {
		m, _, _ := setupTwoPanelModel(t)
		WithVideo(true, "video.no_terminal")(&m)
		if sized {
			next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = next.(Model)
		}
		text := strings.ReplaceAll(ansi.Strip(m.View()), "\n", "")
		if !strings.Contains(text, "sixel") || !strings.Contains(text, "foot ttv-tui") {
			t.Fatalf("sized=%v: opening lacks persistent failure and next step", sized)
		}
	}
}
